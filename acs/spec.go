// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package acs defines the Agent Composition Spec — Telos's central data
// structure (architecture §4).
//
// An ACS is data, not code: content-hashable and versionable. The planner emits
// an unbound Spec; the binder binds a model + budget + tools onto each node; the
// placer annotates a transport + substrate. A generic host instantiates agenkit
// patterns from it (composition, not codegen — invariant 2).
//
// Two invariants are enforced structurally by this package and must hold from
// the first commit:
//
//   - Acceptance is a separate-envelope node (invariant 10). Validate rejects an
//     acceptance node that shares a producer's trust envelope. See enums.go
//     (NodeKind.IsProducer) and validate.go.
//   - Budget is a grant — amount AND period (invariant 4). See budget.go.
package acs

// NodeID identifies a node within a Spec.
type NodeID string

// SchemaVersion is the ACS schema version this package reads and writes.
const SchemaVersion = "v0.1"

// Spec is a complete agent composition. It is the unit the host instantiates and
// the unit the planner re-emits when re-planning (the recursion bottoms out on a
// hand-authored Spec, bootstrap.acs).
type Spec struct {
	// Version is the ACS schema version (SchemaVersion).
	Version string `json:"version"`

	// Hash is the content hash of this Spec with Hash itself excluded from the
	// digest input (see hash.go). Empty until ComputeHash is called; serves as
	// the content-address for versioned re-planning.
	Hash string `json:"hash,omitempty"`

	// Prompt is the original question — provenance and telos. It is the fixed
	// purpose every node serves and nothing may capture (invariant 1).
	Prompt string `json:"prompt"`

	// Archetype is the inferred inquiry shape (research domain pack). The seed
	// uses ArchetypeNone.
	Archetype Archetype `json:"archetype"`

	// Standard is the run's DEFAULT standard of proof — the bar a result clears
	// to be accepted, before burnrate modulates it. bootstrap.acs fixes the
	// system default here. A node may override it (the scoping pass runs at
	// StandardScoping). Empty means StandardConcordant (the seeded default).
	Standard StandardOfProof `json:"standard,omitempty"`

	// RootID is the entry node — the agent the host invokes.
	RootID NodeID `json:"root_id"`

	// Nodes is the full node set, keyed by ID.
	Nodes map[NodeID]*Node `json:"nodes"`

	// Edges express control dependency and dataflow between nodes.
	Edges []Edge `json:"edges,omitempty"`

	// Budget is the grant slice (amount + period) drawn from the run's envelope.
	Budget Budget `json:"budget"`
}

// Node is a single agent in the composition.
type Node struct {
	ID      NodeID   `json:"id"`
	Kind    NodeKind `json:"kind"`
	Pattern Pattern  `json:"pattern"`

	// Role is a human-legible label / system-prompt seed for the node.
	Role string `json:"role,omitempty"`

	// Tools the node may use, by reference. The binder policy-gates these
	// (Cedar/LKI); the ACS only names them.
	Tools []ToolRef `json:"tools,omitempty"`

	// Model is a capability CONSTRAINT, not a model name. The router resolves it.
	Model ModelConstraint `json:"model"`

	// Budget is this node's ask against its parent's grant (a rate, not a total).
	Budget BudgetRequest `json:"budget"`

	// Trust and Gravity decide placement, not cost. Trust is also the axis on
	// which an acceptance node must differ from its producer (invariant 10).
	Trust   TrustBoundary `json:"trust"`
	Gravity Gravity       `json:"gravity,omitempty"`

	// Standard optionally overrides the Spec's default standard of proof for this
	// node (e.g. a scoping node runs at StandardScoping; an acceptance node may
	// be pinned to the run default). Empty inherits Spec.Standard.
	Standard StandardOfProof `json:"standard,omitempty"`

	// Replan marks a node permitted to emit a sub-ACS. Set by the archetype, not
	// generically — the generic host does not grant this.
	Replan bool `json:"replan,omitempty"`

	// Children are the nodes this node composes (for non-leaf patterns). They
	// are also expected to appear as edges; Children is the instantiation order.
	Children []NodeID `json:"children,omitempty"`

	// Annotations filled in downstream. Nil on an unbound spec.
	Binding    *ModelBinding `json:"binding,omitempty"`    // binder
	Grant      *BudgetGrant  `json:"grant,omitempty"`      // governor (via binder)
	Placement  *Placement    `json:"placement,omitempty"`  // placer
	Provenance *Provenance   `json:"provenance,omitempty"` // threaded up to claims
}

// DefaultStandard returns the run's effective default standard of proof,
// substituting the system default (StandardConcordant) when unset. This is the
// value bootstrap.acs fixes and burnrate later modulates.
func (s *Spec) DefaultStandard() StandardOfProof {
	if s.Standard == "" {
		return StandardConcordant
	}
	return s.Standard
}

// EffectiveStandard returns the standard of proof a node is held to: its own
// override if set, otherwise the run default.
func (s *Spec) EffectiveStandard(n *Node) StandardOfProof {
	if n != nil && n.Standard != "" {
		return n.Standard
	}
	return s.DefaultStandard()
}

// Edge connects two nodes. Dataflow edges pass state BY REFERENCE so the
// working footprint stays the working slice (architecture §4).
type Edge struct {
	From NodeID   `json:"from"`
	To   NodeID   `json:"to"`
	Kind EdgeKind `json:"kind"`

	// Ref is the state reference a dataflow edge carries. Empty for dependency
	// edges.
	Ref StateRef `json:"ref,omitempty"`
}

// ToolRef names a tool the binder will resolve and policy-gate.
type ToolRef struct {
	Name string `json:"name"`
}

// StateRef is an opaque handle to state passed between nodes by reference. In
// M0 it is a logical key; M3+ may make it content-addressed.
type StateRef string

// ModelConstraint is a capability requirement, never a model name (architecture
// §4). The router (M1) resolves it to a ModelBinding.
type ModelConstraint struct {
	// Tier is the cascade tier the node needs (cheap/mid/frontier). Empty lets
	// the router pick from the cascade floor.
	Tier Tier `json:"tier,omitempty"`

	// Capabilities are required model capabilities (e.g. "tools", "vision",
	// "long-context"). The router never asks a model whether it can; it selects
	// one that declares these.
	Capabilities []string `json:"capabilities,omitempty"`
}

// ModelBinding is the resolved model for a node (binder/router output). Held as
// an annotation; nil on an unbound spec. Concrete fields land with the router
// (M1) — kept minimal here so the schema compiles and round-trips.
type ModelBinding struct {
	// Model is the concrete model identifier the router chose.
	Model string `json:"model"`
	// Provider is the serving backend (e.g. "bedrock", "local").
	Provider string `json:"provider,omitempty"`
}

// BudgetGrant is the conserved allocation the governor issues on Reserve. Held
// as an annotation; nil until reserved. Distinct from BudgetRequest (the ask).
type BudgetGrant struct {
	// GrantID identifies this grant in the ledger.
	GrantID string `json:"grant_id"`
	// Budget is the granted amount over period (still a rate, never a total).
	Budget Budget `json:"budget"`
}

// Placement is the placer's decision for a node (architecture §6/§7). Held as an
// annotation; nil until placed. M0 places everything on Goroutine.
type Placement struct {
	Transport Transport `json:"transport"`
	// Substrate names the adapter that will host the node (e.g. "inproc"). The
	// concrete cohort.Actuator/Observer binding lives in the substrate package.
	Substrate string `json:"substrate,omitempty"`
}

// Provenance is the citation/attestation graph threaded up to a node's claims.
// It is first-class: unprovenanced claims fail acceptance (architecture §4).
// Concrete shape lands with the attestation layer (M6); kept minimal here.
type Provenance struct {
	// Sources are references (e.g. citations, dataset IDs) backing the claim.
	Sources []string `json:"sources,omitempty"`
	// Attestations are provenance attestation IDs (provabl/attest/Copland).
	Attestations []string `json:"attestations,omitempty"`
}
