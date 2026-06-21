// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"fmt"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/agenkit-go/patterns"
	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/governor"
	"github.com/scttfrdmn/telos/planner"
	"github.com/scttfrdmn/telos/router"
)

// defaultMaxPlanDepth bounds re-planning recursion when Deps.MaxPlanDepth is
// unset. Planning/re-planning/sub-planning are one operation at different depths
// (invariant 3); this is the backstop so the recursion cannot run away.
const defaultMaxPlanDepth = 4

// Deps are the optional runtime services the host wires into leaf agents. When
// present, a Reason leaf invokes models THROUGH the gateway (invariant 5) instead
// of returning a deterministic stub. When nil (the M0 path), leaves are stubs and
// the graph is composition-only. Both keep acceptance separable (invariant 10).
type Deps struct {
	Gateway gateway.Gateway
	Router  router.Router
	// Governor conserves the run grant and settles spend; surplus banks through it
	// iff the acceptance verdict accepts (lexicographic — §9).
	Governor governor.Governor
	// Planner turns a question into an ACS the host re-instantiates (closing the
	// recursion). When set, a Planning node plans live; when nil it is inert.
	Planner planner.Planner
	// LiveAcceptance builds acceptance nodes as live summary-judgment renderers
	// (M2) rather than the inert M0 node. The node still runs in its own envelope
	// and is built only via the acceptance package (invariant 10).
	LiveAcceptance bool
	// MaxPlanDepth bounds re-planning recursion (fails closed beyond it). Zero
	// uses defaultMaxPlanDepth.
	MaxPlanDepth int
	// envelopePeriod is the run grant's period, used to build the burn-rate clock.
	envelopePeriod time.Duration
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
	depth    int                          // current re-planning depth (recursion backstop)
	building map[acs.NodeID]bool          // cycle guard (defence in depth; Validate also checks)
	built    map[acs.NodeID]agenkit.Agent // memoize shared subgraphs
}

func (b *builder) maxDepth() int {
	if b.deps != nil && b.deps.MaxPlanDepth > 0 {
		return b.deps.MaxPlanDepth
	}
	return defaultMaxPlanDepth
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
// producer regardless of the pattern it declares (invariant 10). With deps wired,
// the acceptance node renders LIVE verdicts (summary judgment); otherwise it is
// the inert M0 node. Either way it is built ONLY via the acceptance package —
// never a producer builder — and runs in its own envelope.
func (b *builder) dispatch(n *acs.Node) (agenkit.Agent, error) {
	if n.Kind == acs.KindAcceptance {
		if b.deps != nil && b.deps.LiveAcceptance {
			return acceptance.NewSummaryJudge(string(n.ID)), nil
		}
		return acceptance.NewInertNode(string(n.ID)), nil
	}

	switch n.Pattern {
	case acs.PatternLeaf:
		// Research-shape producing nodes (evidence-for/against, reconcile,
		// synthesize, ...) get provenance-aware agents so their records carry the
		// cited sources the verdict grades (M3 critical path). They wrap a
		// gateway-backed producer when deps are wired.
		var gw gateway.Gateway
		var rtr router.Router
		if b.deps != nil {
			gw, rtr = b.deps.Gateway, b.deps.Router
		}
		if a, ok := newResearchLeaf(n, gw, rtr); ok {
			return a, nil
		}
		// A plain Reason leaf invokes a model THROUGH the gateway when deps are
		// wired (invariant 5); otherwise the deterministic M0 stub.
		if n.Kind == acs.KindReason && gw != nil && rtr != nil {
			return newGatewayAgent(n, gw, rtr), nil
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
		// The recursion seam (M3): a Planning node reads the question, asks the
		// planner to EMIT a sub-ACS, and the host re-instantiates it as a live
		// subgraph (base case → real graph, invariant 3). With no planner wired
		// it degrades to a stub (M0 behavior) so composition-only paths still run.
		if b.deps != nil && b.deps.Planner != nil {
			return b.planningAgent(n)
		}
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

// planningAgent constructs the recursion-closing agent for a Planning node. It
// captures the current depth + deps so that, at Process time, it can plan and
// re-instantiate within the depth bound.
func (b *builder) planningAgent(n *acs.Node) (agenkit.Agent, error) {
	if b.depth >= b.maxDepth() {
		return nil, fmt.Errorf("host: re-planning exceeded max depth %d at node %q (fails closed)", b.maxDepth(), n.ID)
	}
	return &planningAgent{
		node:         n,
		deps:         b.deps,
		budget:       b.spec.Budget,
		seedStandard: b.spec.SeedDefaultStandard(),
		depth:        b.depth,
		maxDepth:     b.maxDepth(),
	}, nil
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
// guaranteed by ParallelAgent collecting in input order). Crucially it PRESERVES
// each child's provenance record: the default agenkit aggregation builds a fresh
// message and would drop child metadata, severing the for/against records before
// the reconciliation node can assemble them. We collect every child record into
// the output so provenance survives the parallel fan-in (M3 critical path).
func concatAggregator(id acs.NodeID) patterns.AggregatorFunc {
	return func(msgs []*agenkit.Message) *agenkit.Message {
		out := fmt.Sprintf("[%s/parallel] aggregated %d result(s):", id, len(msgs))
		var records []acceptance.Record
		for _, m := range msgs {
			if m == nil {
				continue
			}
			out += "\n  - " + m.ContentString()
			if r, ok := readRecord(m); ok {
				records = append(records, r)
			}
		}
		msg := agenkit.NewMessage("agent", out)
		// Carry the collected child records forward so the next stage (e.g. the
		// reconciliation node) can assemble the for/against record.
		if len(records) > 0 {
			msg.WithMetadata(recordsKey, records)
		}
		return msg
	}
}
