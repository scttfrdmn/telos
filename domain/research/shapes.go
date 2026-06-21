// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package research

import (
	"time"

	"github.com/scttfrdmn/telos/acs"
)

// Shape builds the ACS graph an archetype implies (architecture §10). The graph
// is UNBOUND data (the binder/router resolve models later); shapes only set the
// composition and the verification structure. Each shape places a SEPARATE-
// envelope acceptance node (invariant 10) as a sibling of production under a
// neutral coordinator root — the planner emits invariant-10-correct graphs, the
// separation is not only hand-authored.
//
// The reconciliation node and producing nodes are tagged via Role and the
// standard of proof so the host knows to (a) thread provenance up from producers
// and (b) let the reconciliation emit an EARNED contested. The host reads these
// tags; the shape only declares them.

// roleReconcile / roleForAgainst are role markers the host recognizes to drive
// the earned-contested behavior (host keys on these, not on free text).
const (
	RoleScope           = "scope"
	RoleRetrieve        = "retrieve"
	RoleExtract         = "extract"
	RoleSynthesize      = "synthesize"
	RoleEvidenceFor     = "evidence-for"
	RoleEvidenceAgainst = "evidence-against"
	RoleReconcile       = "reconcile"
	RoleAccept          = "accept"
)

// Plan builds an unbound acs.Spec for a classified question. budget is the grant
// slice the emitted graph conserves against; standard is the run default (from
// burn-rate, M2). The returned spec is the planner's OUTPUT — the host
// re-instantiates it (closing the recursion).
func Plan(prompt string, c Classification, budget acs.Budget, standard acs.StandardOfProof) *acs.Spec {
	switch c.Archetype {
	case ArchetypeComposite:
		return compositeShape(prompt, budget, standard)
	case ArchetypeMechanistic:
		return mechanisticShape(prompt, budget, standard)
	default:
		return evidenceSynthShape(prompt, budget, standard)
	}
}

func req(amount float64, period time.Duration) acs.BudgetRequest {
	return acs.BudgetRequest{Amount: amount, Period: period}
}

// compositeShape is the §14 graph: an evidence-synthesis SUBSTRATE feeding a
// mechanistic-reconciliation HEAD, with a scoping pass in front and a separate-
// envelope acceptance node. This is the two-phase composite §14 #1 requires.
func compositeShape(prompt string, budget acs.Budget, standard acs.StandardOfProof) *acs.Spec {
	p := budget.Period
	nodes := map[acs.NodeID]*acs.Node{
		"root": {
			ID: "root", Kind: acs.KindReason, Pattern: acs.PatternSequential,
			Role:  "Composite inquiry: scope, gather evidence, reconcile mechanism, then accept.",
			Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount, p),
			// Production spine then the separately-enveloped judge.
			Children: []acs.NodeID{"scope", "substrate", "head", "accept"},
		},

		// Estimate-first scoping pass (cheap; bounds the entity expansion).
		"scope": {
			ID: "scope", Kind: acs.KindReason, Pattern: acs.PatternReact,
			Role: RoleScope, Trust: acs.TrustSameEnvelope, Standard: acs.StandardScoping,
			Model: acs.ModelConstraint{Tier: acs.TierCheap}, Budget: req(budget.Amount*0.05, p),
			Tools: []acs.ToolRef{{Name: "search"}},
		},

		// SUBSTRATE — evidence synthesis: retrieve → parallel extract → synthesize.
		"substrate": {
			ID: "substrate", Kind: acs.KindReason, Pattern: acs.PatternSupervisor,
			Role: "Evidence-synthesis substrate", Trust: acs.TrustSameEnvelope,
			Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.4, p),
			Children: []acs.NodeID{"retrieve", "extract", "synthesize"},
		},
		"retrieve": {
			ID: "retrieve", Kind: acs.KindRetrieve, Pattern: acs.PatternLeaf,
			Role: RoleRetrieve, Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount*0.1, p),
			Tools: []acs.ToolRef{{Name: "search"}, {Name: "fetch"}},
		},
		"extract": {
			ID: "extract", Kind: acs.KindReason, Pattern: acs.PatternParallel,
			Role: RoleExtract, Trust: acs.TrustSameEnvelope,
			Model: acs.ModelConstraint{Tier: acs.TierCheap}, Budget: req(budget.Amount*0.2, p),
			Children: []acs.NodeID{"extract_a", "extract_b"},
		},
		"extract_a": {ID: "extract_a", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleExtract,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierCheap}, Budget: req(budget.Amount*0.1, p)},
		"extract_b": {ID: "extract_b", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleExtract,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierCheap}, Budget: req(budget.Amount*0.1, p)},
		"synthesize": {ID: "synthesize", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleSynthesize,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.1, p)},

		// HEAD — mechanistic: assemble FOR and AGAINST, then reconcile (may
		// return contested). The for/against pair is the adversarial structure
		// (§10), NOT §12 court advocates.
		"head": {
			ID: "head", Kind: acs.KindReason, Pattern: acs.PatternSequential,
			Role: "Mechanistic head", Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount*0.45, p),
			Children: []acs.NodeID{"directions", "reconcile"},
		},
		"directions": {
			ID: "directions", Kind: acs.KindReason, Pattern: acs.PatternParallel,
			Role: "for/against assembly", Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount*0.3, p),
			Children: []acs.NodeID{"evidence_for", "evidence_against"},
		},
		"evidence_for": {ID: "evidence_for", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleEvidenceFor,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.15, p)},
		"evidence_against": {ID: "evidence_against", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleEvidenceAgainst,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.15, p)},
		"reconcile": {ID: "reconcile", Kind: acs.KindReconcile, Pattern: acs.PatternLeaf, Role: RoleReconcile,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.15, p)},

		// ACCEPT — separate-envelope judge (invariant 10).
		"accept": {ID: "accept", Kind: acs.KindAcceptance, Pattern: acs.PatternLeaf, Role: RoleAccept,
			Trust: acs.TrustIsolated, Standard: standard, Budget: req(budget.Amount*0.1, p)},
	}
	return &acs.Spec{
		Version: acs.SchemaVersion, Prompt: prompt, Archetype: acs.ArchetypeMechanistic,
		Standard: standard, RootID: "root", Nodes: nodes, Budget: budget,
		Edges: []acs.Edge{
			{From: "scope", To: "substrate", Kind: acs.EdgeDependency},
			{From: "substrate", To: "head", Kind: acs.EdgeDataflow, Ref: "substrate.evidence"},
			{From: "retrieve", To: "extract", Kind: acs.EdgeDataflow, Ref: "retrieve.sources"},
			{From: "extract", To: "synthesize", Kind: acs.EdgeDataflow, Ref: "extract.claims"},
			{From: "directions", To: "reconcile", Kind: acs.EdgeDataflow, Ref: "directions.forAgainst"},
			{From: "reconcile", To: "accept", Kind: acs.EdgeDataflow, Ref: "reconcile.record"},
		},
	}
}

// mechanisticShape is the head alone (for/against → reconcile + accept), for a
// purely mechanistic question with no distinct evidence ask.
func mechanisticShape(prompt string, budget acs.Budget, standard acs.StandardOfProof) *acs.Spec {
	p := budget.Period
	nodes := map[acs.NodeID]*acs.Node{
		"root": {ID: "root", Kind: acs.KindReason, Pattern: acs.PatternSequential,
			Role: "Mechanistic inquiry", Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount, p),
			Children: []acs.NodeID{"directions", "reconcile", "accept"}},
		"directions": {ID: "directions", Kind: acs.KindReason, Pattern: acs.PatternParallel,
			Role: "for/against assembly", Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount*0.6, p),
			Children: []acs.NodeID{"evidence_for", "evidence_against"}},
		"evidence_for": {ID: "evidence_for", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleEvidenceFor,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.3, p)},
		"evidence_against": {ID: "evidence_against", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleEvidenceAgainst,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.3, p)},
		"reconcile": {ID: "reconcile", Kind: acs.KindReconcile, Pattern: acs.PatternLeaf, Role: RoleReconcile,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.25, p)},
		"accept": {ID: "accept", Kind: acs.KindAcceptance, Pattern: acs.PatternLeaf, Role: RoleAccept,
			Trust: acs.TrustIsolated, Standard: standard, Budget: req(budget.Amount*0.15, p)},
	}
	return &acs.Spec{Version: acs.SchemaVersion, Prompt: prompt, Archetype: acs.ArchetypeMechanistic,
		Standard: standard, RootID: "root", Nodes: nodes, Budget: budget,
		Edges: []acs.Edge{
			{From: "directions", To: "reconcile", Kind: acs.EdgeDataflow, Ref: "directions.forAgainst"},
			{From: "reconcile", To: "accept", Kind: acs.EdgeDataflow, Ref: "reconcile.record"},
		}}
}

// evidenceSynthShape is retrieve → extract → synthesize + accept, for a question
// that only asks what the evidence says (no mechanistic act).
func evidenceSynthShape(prompt string, budget acs.Budget, standard acs.StandardOfProof) *acs.Spec {
	p := budget.Period
	nodes := map[acs.NodeID]*acs.Node{
		"root": {ID: "root", Kind: acs.KindReason, Pattern: acs.PatternSequential,
			Role: "Evidence synthesis", Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount, p),
			Children: []acs.NodeID{"retrieve", "synthesize", "accept"}},
		"retrieve": {ID: "retrieve", Kind: acs.KindRetrieve, Pattern: acs.PatternLeaf, Role: RoleRetrieve,
			Trust: acs.TrustSameEnvelope, Budget: req(budget.Amount*0.4, p),
			Tools: []acs.ToolRef{{Name: "search"}, {Name: "fetch"}}},
		"synthesize": {ID: "synthesize", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Role: RoleSynthesize,
			Trust: acs.TrustSameEnvelope, Model: acs.ModelConstraint{Tier: acs.TierMid}, Budget: req(budget.Amount*0.45, p)},
		"accept": {ID: "accept", Kind: acs.KindAcceptance, Pattern: acs.PatternLeaf, Role: RoleAccept,
			Trust: acs.TrustIsolated, Standard: standard, Budget: req(budget.Amount*0.15, p)},
	}
	return &acs.Spec{Version: acs.SchemaVersion, Prompt: prompt, Archetype: acs.ArchetypeEvidenceSynth,
		Standard: standard, RootID: "root", Nodes: nodes, Budget: budget,
		Edges: []acs.Edge{
			{From: "retrieve", To: "synthesize", Kind: acs.EdgeDataflow, Ref: "retrieve.sources"},
			{From: "synthesize", To: "accept", Kind: acs.EdgeDataflow, Ref: "synthesize.record"},
		}}
}
