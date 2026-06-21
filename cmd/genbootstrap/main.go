// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Command genbootstrap authors bootstrap.acs — the recursion's base case.
//
// The seed is generated from code (not hand-written JSON) so it is always valid
// and self-hashing: this program constructs the Spec, runs acs.Validate, and
// emits acs.Marshal output (which computes the content hash). Run from the repo
// root:
//
//	go run ./cmd/genbootstrap > bootstrap.acs
//
// WHY THIS SEED IS THE BASE CASE (the hand-off note's point, realized in M3).
// bootstrap.acs is the static seed the host instantiates before any planner
// exists (invariant 3: the planner is the root agent, bootstrapped from this
// seed). In M3 it actually drives a real recursion, so its shape determines
// whether composite detection and scoping land. It is therefore the MINIMAL base
// case: a single PLANNING-root node that, given a question, EMITS the real
// multi-node graph (the produce→judge→settle inquiry) which the host
// re-instantiates. The decomposition is NOT hand-wired here — it is the planner's
// output (domain/research). That is the point: the seed fixes as little as
// possible so it cannot bias the inquiry toward one archetype.
//
// What the seed DOES fix (and only this):
//
//	(1) The base case is a Planning node (KindPlan / PatternPlanning / replan):
//	    read the question, plan, re-instantiate. Nothing about WHICH archetype —
//	    that is inferred per question, so a composite question is not flattened.
//
//	(2) A fallback standard of proof (StandardConcordant), used ONLY when burn-rate
//	    has no reservoir-over-clock signal (M2: burn-rate is the source of the
//	    default; the seed is the fallback). §15 #4 (how the per-question standard
//	    is determined) stays open.
//
//	(3) A grant slice (amount + period — invariant 4, never a bare total) the
//	    emitted graph conserves against. Nominal placeholder; a real run's quote
//	    replaces it.
//
// Invariant 10 (acceptance in its own envelope) is NOT hand-placed in the seed —
// it is the planner's responsibility to EMIT an invariant-10-correct graph, and
// acs.Validate rejects any emitted graph that violates it. The seed proves the
// recursion closes; the emitted graph proves the inquiry is well-formed.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

func main() {
	spec := buildBootstrap()

	if err := spec.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "genbootstrap: seed is invalid: %v\n", err)
		os.Exit(1)
	}
	out, err := spec.Marshal() // computes and embeds the content hash
	if err != nil {
		fmt.Fprintf(os.Stderr, "genbootstrap: marshal: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(out, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "genbootstrap: write: %v\n", err)
		os.Exit(1)
	}
}

func buildBootstrap() *acs.Spec {
	// Nominal placeholder grant: a small reservoir over a short clock, shaped as a
	// grant (amount AND period — invariant 4). A real run replaces it via the
	// planner's scoping/quote pass.
	const period = 24 * time.Hour

	root := &acs.Node{
		ID:      "root",
		Kind:    acs.KindPlan,
		Pattern: acs.PatternPlanning,
		Role:    "Investigate the question, honestly, within the grant.",
		Trust:   acs.TrustSameEnvelope,
		// The planner infers the model need per emitted node; the root itself
		// reasons cheaply about shape.
		Model:  acs.ModelConstraint{Tier: acs.TierCheap},
		Budget: acs.BudgetRequest{Amount: 100, Period: period},
		// Replan: this node emits a sub-ACS the host re-instantiates. It is the
		// ONLY thing the base case does — the inquiry shape is the planner's output.
		Replan: true,
	}

	return &acs.Spec{
		Version:   acs.SchemaVersion,
		Prompt:    "(* seed *) Investigate the question, honestly, within the grant.",
		Archetype: acs.ArchetypeNone, // the base case has no archetype; the planner infers it
		// Fallback standard only (burn-rate is the source of the default — M2).
		Standard: acs.StandardConcordant,
		RootID:   "root",
		Nodes:    map[acs.NodeID]*acs.Node{"root": root},
		Budget:   acs.Budget{Amount: 100, Period: period, Currency: "USD"},
	}
}
