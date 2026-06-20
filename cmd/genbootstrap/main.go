// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Command genbootstrap authors bootstrap.acs — the recursion's base case.
//
// The seed is generated from code (not hand-written JSON) so it is always valid
// and self-hashing: this program constructs the Spec, runs acs.Validate, and
// emits acs.Marshal output (which computes the content hash). Run it from the
// repo root:
//
//	go run ./cmd/genbootstrap > bootstrap.acs
//
// WHY THIS SEED IS OPINIONATED (the hand-off note's point). bootstrap.acs is the
// static seed the host instantiates before any planner exists (invariant 3: the
// planner is the root agent, bootstrapped from this seed). It silently fixes two
// system defaults that every later run inherits unless overridden:
//
//	(1) DEFAULT DECOMPOSITION — how Telos thinks when nothing tells it how. The
//	    seed encodes the evidence-synthesis shape (architecture §10/§11) fronted
//	    by an estimate-first scoping pass (§3): scope -> inquiry -> reconcile,
//	    where inquiry is a supervisor over (retrieve -> parallel-extract ->
//	    synthesize). This is the shape the planner reaches for by default.
//
//	(2) DEFAULT STANDARD OF PROOF — how sure we need to be by default. The seed
//	    fixes StandardConcordant: grounded in direction-neutral verifiable facts
//	    (cited sources exist and say what's claimed; computations reproduce),
//	    preserving contested/negative as first-class. Not oracle (too costly as a
//	    standing default; burnrate would decline it late in a grant) and not bare
//	    assertion. How the per-question standard is DETERMINED remains an open
//	    fork (§15 #4); this only sets the floor the recursion bottoms out on.
//
// The seed also pins the keystone seam (invariant 10): acceptance is a node in a
// SEPARATE trust envelope (TrustIsolated), wired as a sibling of production
// under the neutral root — never folded into a producer. In M0 the acceptance
// node is inert (no verdict logic until M2), but its placement is correct from
// commit one, which is the part that is unrecoverable later.
//
// The seed exercises all four M0 patterns, each chosen for a real reason rather
// than to tick a box:
//
//	Sequential -> root spine (estimate-first ordering: scope, then inquire, then reconcile)
//	React      -> scope node (reason + cheap probes to produce a costed plan, bounded)
//	Supervisor -> inquiry head (plan/delegate/synthesize: the evidence-synthesis default)
//	Parallel   -> extraction fan-out (independent per-source extraction)
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
	// The seed's grant is a nominal placeholder: a small reservoir over a short
	// clock. It is shaped as a grant (amount AND period — invariant 4), never a
	// total. A real run replaces this via the planner's scoping/quote pass; the
	// seed only needs to be a well-formed grant so the recursion has a base case.
	const period = 24 * time.Hour
	grant := func(amount float64) acs.BudgetRequest {
		return acs.BudgetRequest{Amount: amount, Period: period}
	}

	nodes := map[acs.NodeID]*acs.Node{
		// ROOT — the planner-as-root spine. Sequential: estimate-first ordering.
		// Replan-capable: this is the node that, post-M3, emits a sub-ACS. In M0
		// it is inert structure that the host instantiates as a SequentialAgent.
		"root": {
			ID:      "root",
			Kind:    acs.KindPlan,
			Pattern: acs.PatternSequential,
			Role:    "Planner-as-root: scope the question, conduct the inquiry, reconcile a result. Serves the telos; captures nothing.",
			Trust:   acs.TrustSameEnvelope,
			Budget:  grant(100),
			Replan:  true,
			// Production spine, then acceptance. The root is a NEUTRAL coordinator
			// wiring production (scope/inquiry/reconcile) and a separately-
			// enveloped acceptance node (accept) — the correct invariant-10 shape.
			// Acceptance runs last and in its own trust envelope; it judges, it
			// does not produce.
			Children: []acs.NodeID{"scope", "inquiry", "reconcile", "accept"},
		},

		// SCOPE — estimate-first (architecture §3). React: reason about the
		// question and issue cheap probes to produce a costed plan. Held to the
		// trivial StandardScoping bar so it cannot itself become a real spend.
		"scope": {
			ID:       "scope",
			Kind:     acs.KindReason,
			Pattern:  acs.PatternReact,
			Role:     "Scoping pass: turn the question into a costed plan (archetype, shape, estimated envelope, implied standard of proof) before any worker spends.",
			Trust:    acs.TrustSameEnvelope,
			Standard: acs.StandardScoping,
			Model:    acs.ModelConstraint{Tier: acs.TierCheap},
			Budget:   acs.BudgetRequest{Amount: 5, Period: period, MaxFraction: 0.1},
			Tools:    []acs.ToolRef{{Name: "search"}},
		},

		// INQUIRY — the default decomposition. Supervisor over the
		// evidence-synthesis shape: plan, delegate to retrieve/extract, synthesize.
		"inquiry": {
			ID:       "inquiry",
			Kind:     acs.KindReason,
			Pattern:  acs.PatternSupervisor,
			Role:     "Evidence-synthesis head: decompose, delegate retrieval and extraction, synthesize a consolidated finding.",
			Trust:    acs.TrustSameEnvelope,
			Model:    acs.ModelConstraint{Tier: acs.TierMid},
			Budget:   grant(70),
			Children: []acs.NodeID{"retrieve", "extract", "synthesize"},
		},

		// RETRIEVE — source fan-out (a leaf worker in M0).
		"retrieve": {
			ID:      "retrieve",
			Kind:    acs.KindRetrieve,
			Pattern: acs.PatternLeaf,
			Role:    "Retrieve candidate sources for the question.",
			Trust:   acs.TrustSameEnvelope,
			Budget:  grant(15),
			Tools:   []acs.ToolRef{{Name: "search"}, {Name: "fetch"}},
		},

		// EXTRACT — parallel per-source extraction. Parallel: independent workers,
		// results aggregated. The fan-out the goroutine rung is built for (§7).
		"extract": {
			ID:       "extract",
			Kind:     acs.KindReason,
			Pattern:  acs.PatternParallel,
			Role:     "Extract claims and evidence from each retrieved source, independently.",
			Trust:    acs.TrustSameEnvelope,
			Model:    acs.ModelConstraint{Tier: acs.TierCheap},
			Budget:   grant(30),
			Children: []acs.NodeID{"extract_a", "extract_b"},
		},
		"extract_a": {
			ID:      "extract_a",
			Kind:    acs.KindReason,
			Pattern: acs.PatternLeaf,
			Role:    "Extraction worker (source partition A).",
			Trust:   acs.TrustSameEnvelope,
			Model:   acs.ModelConstraint{Tier: acs.TierCheap},
			Budget:  grant(15),
		},
		"extract_b": {
			ID:      "extract_b",
			Kind:    acs.KindReason,
			Pattern: acs.PatternLeaf,
			Role:    "Extraction worker (source partition B).",
			Trust:   acs.TrustSameEnvelope,
			Model:   acs.ModelConstraint{Tier: acs.TierCheap},
			Budget:  grant(15),
		},

		// SYNTHESIZE — the supervisor's synthesis specialist.
		"synthesize": {
			ID:      "synthesize",
			Kind:    acs.KindReason,
			Pattern: acs.PatternLeaf,
			Role:    "Synthesize extracted evidence into a consolidated finding with provenance.",
			Trust:   acs.TrustSameEnvelope,
			Model:   acs.ModelConstraint{Tier: acs.TierMid},
			Budget:  grant(20),
		},

		// RECONCILE — consolidate the inquiry into a result that may be contested.
		// Returns a SHAPE, not a manufactured yes/no (architecture §14).
		"reconcile": {
			ID:      "reconcile",
			Kind:    acs.KindReconcile,
			Pattern: acs.PatternLeaf,
			Role:    "Reconcile the consolidated finding into a result; preserve contested / stage-dependent rather than forcing a verdict.",
			Trust:   acs.TrustSameEnvelope,
			Model:   acs.ModelConstraint{Tier: acs.TierMid},
			Budget:  grant(15),
		},

		// ACCEPT — the keystone seam (invariant 10). A disinterested verdict node
		// in a SEPARATE trust envelope (TrustIsolated), wired as a sibling of
		// production under the neutral root — never owned by a producer. Inert in
		// M0 (no verdict logic until M2); the SEPARATION is what must be right now.
		"accept": {
			ID:      "accept",
			Kind:    acs.KindAcceptance,
			Pattern: acs.PatternLeaf,
			Role:    "Disinterested acceptance: render a labeled verdict (oracle-verified / concordant-under-test / contested) on the run record, in a separate envelope. Renders no verdict in M0.",
			Trust:   acs.TrustIsolated,
			Budget:  grant(10),
		},
	}

	spec := &acs.Spec{
		Version:   acs.SchemaVersion,
		Prompt:    "(* seed *) Investigate the question, honestly, within the grant.",
		Archetype: acs.ArchetypeNone,      // the seed is the base case, not a research run
		Standard:  acs.StandardConcordant, // <-- the system DEFAULT standard of proof
		RootID:    "root",
		Nodes:     nodes,
		Budget:    acs.Budget{Amount: 100, Period: period, Currency: "USD"},
		Edges: []acs.Edge{
			// Spine dependencies (estimate-first ordering).
			{From: "scope", To: "inquiry", Kind: acs.EdgeDependency},
			{From: "inquiry", To: "reconcile", Kind: acs.EdgeDependency},
			// Inquiry-internal dataflow (state passed BY REFERENCE — §4).
			{From: "retrieve", To: "extract", Kind: acs.EdgeDataflow, Ref: "retrieve.sources"},
			{From: "extract", To: "synthesize", Kind: acs.EdgeDataflow, Ref: "extract.claims"},
			// The acceptance node judges the reconciled record — a dataflow edge
			// from production INTO acceptance. Acceptance never feeds production.
			{From: "reconcile", To: "accept", Kind: acs.EdgeDataflow, Ref: "reconcile.record"},
		},
	}
	return spec
}
