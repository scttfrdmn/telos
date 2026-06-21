// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package planner is the root agent: question → ACS (architecture §3/§5,
// invariant 3). It is not a privileged outside controller — it runs ON the host
// as a Planning-pattern node, bootstrapped from the static seed. Planning,
// re-planning, and sub-planning are one operation at different depths.
//
// It is estimate-first: Scope turns a question into a costed plan (archetype,
// shape, bounded entity expansion) before any worker spends; Plan emits the
// unbound acs.Spec the host re-instantiates (closing the recursion). The research
// awareness lives in domain/research; the planner orchestrates it and makes the
// scoping decision auditable.
package planner

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/domain/research"
)

// Planner infers the shape of an inquiry and emits a composition.
type Planner interface {
	// Scope is the estimate-first pass: question → costed, audited plan.
	Scope(ctx context.Context, prompt string, budget acs.Budget, standard acs.StandardOfProof) (*Quote, error)
	// Plan emits the unbound ACS for a scoped quote (the host re-instantiates it).
	Plan(ctx context.Context, q *Quote) (*acs.Spec, error)
}

// Quote is the output of the estimate-first scoping pass — a costed plan plus the
// AUDITABLE scoping decision (the entity expansion and the bounds applied), so a
// human can verify scope landed between flatten and explode (§14 #2).
type Quote struct {
	Prompt         string
	Classification research.Classification
	Budget         acs.Budget
	Standard       acs.StandardOfProof
	// Scoping is the inspectable entity expansion and the bounds applied. It is
	// the artifact §14 check #2 audits.
	Scoping research.Scoping
}

// researchPlanner is the M3 planner over the research domain pack.
type researchPlanner struct{}

// New returns the research-domain planner.
func New() Planner { return &researchPlanner{} }

// Scope classifies the question and bounds its entity expansion. The scoping is
// returned on the Quote for inspection — the planner does not hide how it scoped.
func (p *researchPlanner) Scope(ctx context.Context, prompt string, budget acs.Budget, standard acs.StandardOfProof) (*Quote, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prompt == "" {
		return nil, fmt.Errorf("planner: empty prompt")
	}
	c := research.Classify(prompt)
	scoping := research.Scope(prompt, c)
	return &Quote{
		Prompt:         prompt,
		Classification: c,
		Budget:         budget,
		Standard:       standard,
		Scoping:        scoping,
	}, nil
}

// Plan emits the unbound ACS for the quote's archetype and budget.
func (p *researchPlanner) Plan(ctx context.Context, q *Quote) (*acs.Spec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q == nil {
		return nil, fmt.Errorf("planner: nil quote")
	}
	spec := research.Plan(q.Prompt, q.Classification, q.Budget, q.Standard)
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("planner: emitted an invalid spec: %w", err)
	}
	return spec, nil
}
