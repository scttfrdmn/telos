// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import (
	"errors"
	"fmt"
)

// Validate checks a Spec for structural integrity and for the two committed
// invariants this package is responsible for enforcing.
//
// Integrity:
//   - schema version is recognized
//   - RootID exists in Nodes
//   - every node's ID matches its map key, and its kind/pattern/trust are valid
//   - every edge endpoint and every child reference resolves to a known node
//   - the graph reachable from RootID is acyclic (a composition is a DAG)
//   - each node's BudgetRequest and the Spec Budget are well-formed (grant-shaped)
//
// Invariant 10 (the keystone seam): an Acceptance node may not share a
// producer's trust envelope. Concretely, an acceptance node must not be on a
// same-envelope edge with a producer it is positioned to judge, and must not be
// declared same-envelope while a producer parent is same-envelope. See
// checkAcceptanceSeparation.
//
// Invariant 4: every budget in the spec is a grant (amount over a positive
// period), enforced via Budget/BudgetRequest Validate.
func (s *Spec) Validate() error {
	var errs []error

	if s.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf("unsupported acs version %q (want %q)", s.Version, SchemaVersion))
	}
	if !s.Archetype.valid() {
		errs = append(errs, fmt.Errorf("invalid archetype %q", s.Archetype))
	}
	if !s.Standard.valid() {
		errs = append(errs, fmt.Errorf("invalid standard of proof %q", s.Standard))
	}
	if err := s.Budget.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("spec budget: %w", err))
	}
	if len(s.Nodes) == 0 {
		errs = append(errs, errors.New("spec has no nodes"))
	}
	if s.RootID == "" {
		errs = append(errs, errors.New("spec has no root_id"))
	} else if _, ok := s.Nodes[s.RootID]; !ok {
		errs = append(errs, fmt.Errorf("root_id %q is not a node", s.RootID))
	}

	for id, n := range s.Nodes {
		if n == nil {
			errs = append(errs, fmt.Errorf("node %q is nil", id))
			continue
		}
		if n.ID != id {
			errs = append(errs, fmt.Errorf("node key %q != node id %q", id, n.ID))
		}
		if !n.Kind.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid kind %q", id, n.Kind))
		}
		if !n.Pattern.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid pattern %q", id, n.Pattern))
		}
		if !n.Trust.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid trust %q", id, n.Trust))
		}
		if !n.Gravity.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid gravity %q", id, n.Gravity))
		}
		if !n.Model.Tier.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid model tier %q", id, n.Model.Tier))
		}
		if !n.Standard.valid() {
			errs = append(errs, fmt.Errorf("node %q: invalid standard of proof %q", id, n.Standard))
		}
		if err := n.Budget.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("node %q budget: %w", id, err))
		}
		// A composing pattern (Sequential/Parallel/Supervisor) needs children; a
		// non-composing pattern with children is an authoring mistake. React
		// needs a tool set, not children (its agenkit constructor requires ≥1
		// tool).
		if n.Pattern.Composes() && len(n.Children) == 0 {
			errs = append(errs, fmt.Errorf("node %q: pattern %q composes but has no children", id, n.Pattern))
		}
		if !n.Pattern.Composes() && len(n.Children) > 0 {
			errs = append(errs, fmt.Errorf("node %q: pattern %q does not compose but has %d children", id, n.Pattern, len(n.Children)))
		}
		if n.Pattern.UsesTools() && len(n.Tools) == 0 {
			errs = append(errs, fmt.Errorf("node %q: react pattern requires at least one tool", id))
		}
		for _, c := range n.Children {
			if _, ok := s.Nodes[c]; !ok {
				errs = append(errs, fmt.Errorf("node %q: child %q is not a node", id, c))
			}
		}
	}

	for i, e := range s.Edges {
		if !e.Kind.valid() {
			errs = append(errs, fmt.Errorf("edge %d: invalid kind %q", i, e.Kind))
		}
		if _, ok := s.Nodes[e.From]; !ok {
			errs = append(errs, fmt.Errorf("edge %d: from %q is not a node", i, e.From))
		}
		if _, ok := s.Nodes[e.To]; !ok {
			errs = append(errs, fmt.Errorf("edge %d: to %q is not a node", i, e.To))
		}
		if e.Kind == EdgeDataflow && e.Ref == "" {
			errs = append(errs, fmt.Errorf("edge %d: dataflow edge %s->%s has no state ref", i, e.From, e.To))
		}
	}

	if err := s.checkAcyclic(); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, s.checkAcceptanceSeparation()...)

	return errors.Join(errs...)
}

// checkAcyclic confirms the parent→child graph reachable from RootID is a DAG.
func (s *Spec) checkAcyclic() error {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[NodeID]int, len(s.Nodes))

	var visit func(id NodeID, path []NodeID) error
	visit = func(id NodeID, path []NodeID) error {
		n, ok := s.Nodes[id]
		if !ok {
			return nil // missing-node error already reported elsewhere
		}
		color[id] = grey
		path = append(path, id)
		for _, c := range n.Children {
			switch color[c] {
			case grey:
				return fmt.Errorf("composition has a cycle: %v -> %s", path, c)
			case white:
				if err := visit(c, path); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	if s.RootID != "" {
		if err := visit(s.RootID, nil); err != nil {
			return err
		}
	}
	return nil
}

// checkAcceptanceSeparation enforces invariant 10 at the schema level: an
// acceptance verdict is rendered by a disinterested party in a SEPARATE trust
// envelope from the production it judges, and acceptance never produces the
// thing it rules on.
//
// The two structural rules M0 can check without acceptance policy:
//
//  1. Separate envelope. An acceptance node declared TrustSameEnvelope is a
//     contradiction — the whole point is that it does not share the producers'
//     envelope. Require isolated/untrusted. (This alone guarantees an acceptance
//     node is never co-enveloped with a same-envelope producer; later milestones
//     add the budget-envelope half — neutral funding.)
//
//  2. Acceptance does not produce. An acceptance node may not compose producer
//     children — if it did, it would be generating the record it then rules on,
//     which is the self-settling the invariant forbids.
//
// Note on shape: a neutral COORDINATOR (e.g. a supervisor/plan root) that wires
// both producers and a separately-enveloped acceptance node as siblings is the
// CORRECT shape, not a violation — coordination is not production. The thing
// invariant 10 forbids is a node rendering a verdict on its OWN output; because
// acceptance is its own NodeKind in its own trust envelope, that cannot happen
// here. The complementary guard — production code never importing acceptance to
// self-render — is enforced at the package boundary (see issue #9), not the ACS.
func (s *Spec) checkAcceptanceSeparation() []error {
	var errs []error
	for id, n := range s.Nodes {
		if n == nil || n.Kind != KindAcceptance {
			continue
		}
		if n.Trust == TrustSameEnvelope {
			errs = append(errs, fmt.Errorf("acceptance node %q must not be same-envelope (invariant 10: separate trust envelope)", id))
		}
		for _, c := range n.Children {
			if child, ok := s.Nodes[c]; ok && child.Kind.IsProducer() {
				errs = append(errs, fmt.Errorf("acceptance node %q composes producer child %q (invariant 10: acceptance does not produce what it judges)", id, c))
			}
		}
	}
	return errs
}
