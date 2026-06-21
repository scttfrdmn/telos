// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/agenkit-go/patterns"
	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/router"
)

// Deps are the optional runtime services the host wires into leaf agents. When
// present, a Reason leaf invokes models THROUGH the gateway (invariant 5) instead
// of returning a deterministic stub. When nil (the M0 path), leaves are stubs and
// the graph is composition-only. Both keep acceptance separable (invariant 10).
type Deps struct {
	Gateway gateway.Gateway
	Router  router.Router
}

// BuildWithDeps instantiates a spec, wiring Deps into leaf agents that can use
// them. Build(spec) is BuildWithDeps(spec, nil).
func BuildWithDeps(spec *acs.Spec, deps *Deps) (agenkit.Agent, error) {
	if spec == nil {
		return nil, fmt.Errorf("host: nil spec")
	}
	b := &builder{spec: spec, deps: deps, building: map[acs.NodeID]bool{}, built: map[acs.NodeID]agenkit.Agent{}}
	return b.node(spec.RootID)
}

// Build instantiates an acs.Spec into a runnable agenkit agent graph rooted at
// the spec's RootID. This is "composition, not codegen" (invariant 2): the spec
// is data; this function maps each node's Pattern to an agenkit constructor.
//
// The four M0 patterns map as:
//
//	PatternSequential -> patterns.NewSequentialAgent (children in order)
//	PatternParallel   -> patterns.NewParallelAgent   (children + aggregator)
//	PatternSupervisor -> patterns.NewSupervisorAgent (stub planner + child specialists)
//	PatternReact      -> patterns.NewReActAgent       (stub reasoner + stub tools)
//	PatternLeaf       -> a deterministic stubAgent (or, for acceptance, the
//	                     separate-envelope acceptance node)
//	PatternPlanning   -> treated as its single child's agent (recursion seam, M3)
//
// THE SEAM (invariant 10): a node of kind Acceptance is ALWAYS built through
// acceptance.NewInertNode — never through a producer builder. This is the
// package-level half of the separation: production code in this package cannot
// construct an acceptance node from a stub, and acceptance code produces nothing.
func Build(spec *acs.Spec) (agenkit.Agent, error) {
	return BuildWithDeps(spec, nil)
}

type builder struct {
	spec     *acs.Spec
	deps     *Deps
	building map[acs.NodeID]bool          // cycle guard (defence in depth; Validate also checks)
	built    map[acs.NodeID]agenkit.Agent // memoize shared subgraphs
}

func (b *builder) node(id acs.NodeID) (agenkit.Agent, error) {
	if a, ok := b.built[id]; ok {
		return a, nil
	}
	if b.building[id] {
		return nil, fmt.Errorf("host: cycle through node %q", id)
	}
	n, ok := b.spec.Nodes[id]
	if !ok {
		return nil, fmt.Errorf("host: node %q not found", id)
	}
	b.building[id] = true
	defer delete(b.building, id)

	a, err := b.dispatch(n)
	if err != nil {
		return nil, fmt.Errorf("host: build node %q (%s/%s): %w", id, n.Kind, n.Pattern, err)
	}
	b.built[id] = a
	return a, nil
}

// dispatch routes a node to the correct constructor. Acceptance is intercepted
// FIRST, before any producer path, so an acceptance node can never be built as a
// producer regardless of the pattern it declares (invariant 10).
func (b *builder) dispatch(n *acs.Node) (agenkit.Agent, error) {
	if n.Kind == acs.KindAcceptance {
		return acceptance.NewInertNode(string(n.ID)), nil
	}

	switch n.Pattern {
	case acs.PatternLeaf:
		// A Reason leaf invokes a model THROUGH the gateway when deps are wired
		// (invariant 5). Other leaf kinds (Retrieve/Reconcile) and the no-deps M0
		// path remain deterministic stubs.
		if n.Kind == acs.KindReason && b.deps != nil && b.deps.Gateway != nil && b.deps.Router != nil {
			return newGatewayAgent(n, b.deps.Gateway, b.deps.Router), nil
		}
		return newStubAgent(n), nil

	case acs.PatternSequential:
		kids, err := b.children(n)
		if err != nil {
			return nil, err
		}
		return adapt(patterns.NewSequentialAgent(kids))

	case acs.PatternParallel:
		kids, err := b.children(n)
		if err != nil {
			return nil, err
		}
		return adapt(patterns.NewParallelAgent(kids, concatAggregator(n.ID)))

	case acs.PatternSupervisor:
		return b.supervisor(n)

	case acs.PatternReact:
		return b.react(n)

	case acs.PatternPlanning:
		// The recursion seam: a Planning node re-emits a sub-ACS (M3). In M0 it
		// has no children to re-instantiate, so it acts as a leaf worker. If it
		// were given children, treat it like a sequential spine.
		if len(n.Children) == 0 {
			return newStubAgent(n), nil
		}
		kids, err := b.children(n)
		if err != nil {
			return nil, err
		}
		return adapt(patterns.NewSequentialAgent(kids))

	default:
		return nil, fmt.Errorf("unsupported pattern %q", n.Pattern)
	}
}

// children builds each child node in declared order.
func (b *builder) children(n *acs.Node) ([]agenkit.Agent, error) {
	if len(n.Children) == 0 {
		return nil, fmt.Errorf("pattern %q requires children", n.Pattern)
	}
	kids := make([]agenkit.Agent, 0, len(n.Children))
	for _, cid := range n.Children {
		c, err := b.node(cid)
		if err != nil {
			return nil, err
		}
		kids = append(kids, c)
	}
	return kids, nil
}

// supervisor wires a stub planner over child specialists keyed by child node ID.
func (b *builder) supervisor(n *acs.Node) (agenkit.Agent, error) {
	if len(n.Children) == 0 {
		return nil, fmt.Errorf("supervisor requires child specialists")
	}
	specialists := make(map[string]agenkit.Agent, len(n.Children))
	types := make([]string, 0, len(n.Children))
	for _, cid := range n.Children {
		c, err := b.node(cid)
		if err != nil {
			return nil, err
		}
		specialists[string(cid)] = c
		types = append(types, string(cid))
	}
	planner := newStubPlanner(n.ID, types)
	return adapt(patterns.NewSupervisorAgent(planner, specialists))
}

// react wires a stub reasoner with the node's declared tools (as stub tools).
func (b *builder) react(n *acs.Node) (agenkit.Agent, error) {
	if len(n.Tools) == 0 {
		return nil, fmt.Errorf("react node requires at least one tool")
	}
	tools := make([]agenkit.Tool, 0, len(n.Tools))
	for _, ref := range n.Tools {
		tools = append(tools, newStubTool(ref))
	}
	return adapt(patterns.NewReActAgent(&patterns.ReActConfig{
		Agent:    &reactInnerAgent{nodeID: n.ID, role: n.Role},
		Tools:    tools,
		MaxSteps: 3,
	}))
}

// concatAggregator combines parallel results deterministically (stable order is
// guaranteed by ParallelAgent collecting in input order).
func concatAggregator(id acs.NodeID) patterns.AggregatorFunc {
	return func(msgs []*agenkit.Message) *agenkit.Message {
		out := fmt.Sprintf("[%s/parallel] aggregated %d result(s):", id, len(msgs))
		for _, m := range msgs {
			if m != nil {
				out += "\n  - " + m.ContentString()
			}
		}
		return agenkit.NewMessage("agent", out)
	}
}
