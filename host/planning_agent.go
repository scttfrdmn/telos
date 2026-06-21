// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/burnrate"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/governor"
)

// planningAgent closes the recursion (invariant 3). At Process time it reads the
// question, asks the planner to EMIT an acs.Spec, re-instantiates that spec as a
// live subgraph (one depth deeper, within the bound), and runs it under a child
// grant reserved from the parent — so budget flows through ctx down the live,
// dynamically-emitted tree and conservation recurses.
//
// This is "planning, re-planning, and sub-planning are one operation at different
// depths": the same builder instantiates the emitted graph, and a Planning node
// inside the emitted graph would plan again, one level deeper, until the depth
// bound (fails closed).
type planningAgent struct {
	node         *acs.Node
	deps         *Deps
	budget       acs.Budget          // the run grant the emitted graph conserves against
	seedStandard acs.StandardOfProof // the spec's seed fallback standard
	depth        int
	maxDepth     int
}

func (a *planningAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	question := a.node.Role
	if message != nil {
		if c := message.ContentString(); c != "" {
			question = c
		}
	}

	// Choose the run's default standard via burn-rate (M2): a visible function of
	// reservoir-over-clock. The seed value is the FALLBACK when burn-rate has no
	// signal (no governor / no live grant). §15 #4 (how stakes modulate it) stays
	// open — we surface burn-rate's default, not a hard-coded standard.
	standard := a.resolveStandard()

	// SCOPE then PLAN (estimate-first). The planner emits an unbound, validated
	// acs.Spec — the base case becomes a real graph.
	quote, err := a.deps.Planner.Scope(ctx, question, a.budget, standard)
	if err != nil {
		return nil, fmt.Errorf("host: scope: %w", err)
	}
	subSpec, err := a.deps.Planner.Plan(ctx, quote)
	if err != nil {
		return nil, fmt.Errorf("host: plan: %w", err)
	}

	// Re-instantiate the emitted spec one depth deeper (the recursion closes).
	sub := &builder{
		spec:     subSpec,
		deps:     a.deps,
		depth:    a.depth + 1,
		building: map[acs.NodeID]bool{},
		built:    map[acs.NodeID]agenkit.Agent{},
	}
	root, err := sub.node(subSpec.RootID)
	if err != nil {
		return nil, fmt.Errorf("host: re-instantiate emitted spec: %w", err)
	}

	// Run the emitted graph under a CHILD grant reserved from the parent, so the
	// emitted tree conserves against the run envelope (budget flows through ctx).
	runCtx, release, err := a.reserveChild(ctx, subSpec.Budget)
	if err != nil {
		return nil, err
	}
	defer release()

	out, err := root.Process(runCtx, agenkit.NewMessage("user", question))
	if err != nil {
		return nil, err
	}
	// Surface the scoping decision (auditable §14 #2) on the output metadata.
	if out != nil {
		out.WithMetadata("telos.archetype", string(quote.Classification.Archetype))
		out.WithMetadata("telos.scoping", quote.Scoping)
		out.WithMetadata("telos.standard", string(standard))
	}
	return out, nil
}

// resolveStandard returns the run default standard: burn-rate's reservoir-over-
// clock default when a governor signal is available, else the spec's seed
// fallback. (Burn-rate is the source of the default; the seed is the fallback —
// M2 wiring.)
func (a *planningAgent) resolveStandard() acs.StandardOfProof {
	if a.deps.Governor == nil {
		return orConcordant(a.seedStandard)
	}
	// Read the live root reservoir/clock for the burn-rate signal.
	mem, ok := a.deps.Governor.(*governor.Mem)
	if !ok {
		return orConcordant(a.seedStandard)
	}
	br := burnrate.New(nil)
	std := br.DefaultStandard(mem.ReservoirFor(governor.RootGrant), a.deps.runClock())
	if std == "" {
		return orConcordant(a.seedStandard)
	}
	return std
}

func orConcordant(s acs.StandardOfProof) acs.StandardOfProof {
	if s == "" {
		return acs.StandardConcordant
	}
	return s
}

// reserveChild reserves a child grant for the emitted graph and returns a context
// carrying it plus a release func. If no governor is wired, it is a no-op pass-
// through (offline/echo paths still run).
func (a *planningAgent) reserveChild(ctx context.Context, budget acs.Budget) (context.Context, func(), error) {
	if a.deps.Governor == nil {
		return ctx, func() {}, nil
	}
	grant, err := a.deps.Governor.Reserve(ctx, gateway.ParentGrant(ctx), acs.BudgetRequest{
		Amount: budget.Amount, Period: budget.Period,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("host: reserve child grant for emitted graph (fails closed): %w", err)
	}
	gid := governor.GrantID(grant.GrantID)
	runCtx := gateway.WithParentGrant(ctx, gid)
	release := func() {
		// The emitted graph's internal nodes settle their own spend through the
		// gateway; release the planning envelope's remaining escrow.
		_ = a.deps.Governor.Release(context.Background(), gid)
	}
	return runCtx, release, nil
}

func (a *planningAgent) Name() string           { return string(a.node.ID) }
func (a *planningAgent) Capabilities() []string { return []string{"planner", "planning", "root-agent"} }
func (a *planningAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}
