// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

// The enums are string-typed so an ACS file is human-authorable and round-trips
// through JSON without custom marshalers. Each has a set of valid values and a
// valid() predicate used by Validate.

// NodeKind is what a node does (architecture §4). Acceptance is first-class: a
// verdict is rendered by a disinterested node in a separate envelope, never by a
// producer (invariant 10).
type NodeKind string

const (
	KindReason           NodeKind = "reason"            // model reasoning
	KindRetrieve         NodeKind = "retrieve"          // fetch evidence / sources
	KindComputeSynthesis NodeKind = "compute_synthesis" // emit a WorkloadSpec (§8)
	KindPlan             NodeKind = "plan"              // emit a sub-ACS (the recursion)
	KindReconcile        NodeKind = "reconcile"         // consolidate / may return contested
	KindAcceptance       NodeKind = "acceptance"        // disinterested verdict (separate envelope)
)

func (k NodeKind) valid() bool {
	switch k {
	case KindReason, KindRetrieve, KindComputeSynthesis, KindPlan, KindReconcile, KindAcceptance:
		return true
	}
	return false
}

// IsProducer reports whether a node kind produces results that an acceptance
// node may later judge. Acceptance itself is never a producer — this is the
// seam that keeps production and acceptance separable (invariant 10 / §12).
func (k NodeKind) IsProducer() bool { return k != KindAcceptance }

// Pattern is the agenkit composition pattern a node instantiates. M0 supports
// the four base patterns plus Planning (the recursion seam, inert until M3).
type Pattern string

const (
	PatternSequential Pattern = "sequential"
	PatternParallel   Pattern = "parallel"
	PatternSupervisor Pattern = "supervisor"
	PatternReact      Pattern = "react"
	PatternPlanning   Pattern = "planning" // root agent re-plans (M3)
	// PatternLeaf marks a node with no composition — a single worker agent.
	PatternLeaf Pattern = "leaf"
)

func (p Pattern) valid() bool {
	switch p {
	case PatternSequential, PatternParallel, PatternSupervisor, PatternReact, PatternPlanning, PatternLeaf:
		return true
	}
	return false
}

// Composes reports whether a pattern wires CHILD NODES. Only Sequential,
// Parallel, and Supervisor compose children. React wraps a single agent plus
// tools (it has no child nodes — its agenkit constructor takes one agent and a
// tool set); Planning is the single root agent that re-emits a sub-ACS; Leaf is
// a lone worker. Those three carry no children.
func (p Pattern) Composes() bool {
	switch p {
	case PatternSequential, PatternParallel, PatternSupervisor:
		return true
	}
	return false
}

// UsesTools reports whether a pattern requires a tool set rather than child
// nodes. React is tool-driven (agenkit requires ≥1 tool to construct one).
func (p Pattern) UsesTools() bool { return p == PatternReact }

// EdgeKind distinguishes control dependency from dataflow. Dataflow edges carry
// a StateRef — state is passed BY REFERENCE, never inlined (architecture §4).
type EdgeKind string

const (
	EdgeDependency EdgeKind = "dependency" // B runs after A
	EdgeDataflow   EdgeKind = "dataflow"   // A's output (by ref) feeds B
)

func (e EdgeKind) valid() bool {
	return e == EdgeDependency || e == EdgeDataflow
}

// TrustBoundary decides placement, not cost (architecture §4/§6). It is also the
// axis on which acceptance must differ from its producer (invariant 10).
type TrustBoundary string

const (
	TrustSameEnvelope TrustBoundary = "same-envelope" // shares the parent's trust + budget tree
	TrustIsolated     TrustBoundary = "isolated"      // own envelope (own microVM, etc.)
	TrustUntrusted    TrustBoundary = "untrusted"     // hostile-input boundary
)

func (t TrustBoundary) valid() bool {
	switch t {
	case TrustSameEnvelope, TrustIsolated, TrustUntrusted:
		return true
	}
	return false
}

// Gravity expresses what pulls a node toward a substrate — data, model, or
// compute locality. Like Trust, it informs placement, not cost.
type Gravity string

const (
	GravityNone    Gravity = "none"
	GravityData    Gravity = "data"    // sovereign / large data is local
	GravityModel   Gravity = "model"   // a local model must run here
	GravityCompute Gravity = "compute" // heavy synthesized compute is local
)

func (g Gravity) valid() bool {
	switch g {
	case "", GravityNone, GravityData, GravityModel, GravityCompute:
		// "" defaults to GravityNone — gravity is optional on a node.
		return true
	}
	return false
}

// Archetype is the inferred inquiry shape (research domain pack, architecture
// §10). The seed uses ArchetypeNone — it is the base case, not a research run.
type Archetype string

const (
	ArchetypeNone            Archetype = "none"
	ArchetypeEvidenceSynth   Archetype = "evidence-synthesis"
	ArchetypeMechanistic     Archetype = "mechanistic"
	ArchetypeComparative     Archetype = "comparative"
	ArchetypeQuantitative    Archetype = "quantitative"
	ArchetypeExploratoryOpen Archetype = "exploratory"
)

func (a Archetype) valid() bool {
	switch a {
	case ArchetypeNone, ArchetypeEvidenceSynth, ArchetypeMechanistic,
		ArchetypeComparative, ArchetypeQuantitative, ArchetypeExploratoryOpen:
		return true
	}
	return false
}

// Tier is a capability constraint on model selection — NOT a model name. The
// router resolves a Tier (and capabilities) to a concrete ModelBinding; the ACS
// never names a model (architecture §4: capability-as-constraint).
type Tier string

const (
	TierCheap    Tier = "cheap" // cascade floor
	TierMid      Tier = "mid"
	TierFrontier Tier = "frontier" // escalate only under pressure
)

func (t Tier) valid() bool {
	switch t {
	case TierCheap, TierMid, TierFrontier, "":
		return true
	}
	return false
}

// StandardOfProof is the bar a result must clear to be accepted — "how sure we
// need to be." It is the user's PRIMARY input (architecture §3); envelope, bond
// curve, and court tier all derive from it. The user/archetype sets it and
// burnrate modulates the default up early in a grant and down late (architecture
// §12 committed seam). It is graded direction-neutrally: the verdict's Basis
// (oracle-verified / concordant-under-test / contested) is a separate axis — the
// label of HOW it was established, not WHICH way it went.
//
// PLACEHOLDER, not settled policy: bootstrap.acs currently fixes StandardConcordant
// as the seed default, but that is provisional pending burnrate (M2) and the
// resolution of §15 fork #4 (how the per-question standard is *determined* —
// prompt vs archetype vs adjudicated, and how question-stakes combine with
// grant-burn). It reads as "the one standard we have until burnrate exists," not
// a considered choice of default. Do not treat the value as decided.
type StandardOfProof string

const (
	// StandardScoping is the trivial bar for the estimate-first scoping pass:
	// enough to produce a costed plan, nothing more (architecture §3).
	StandardScoping StandardOfProof = "scoping"
	// StandardPlausible: internally coherent, lightly sourced.
	StandardPlausible StandardOfProof = "plausible"
	// StandardConcordant: grounded in direction-neutral verifiable facts — cited
	// sources exist and say what's claimed; computations reproduce. The system
	// default; preserves contested/negative as first-class results.
	StandardConcordant StandardOfProof = "concordant"
	// StandardOracle: oracle-verified where an oracle exists (clinical-grade).
	// Expensive; burnrate declines it late in a grant.
	StandardOracle StandardOfProof = "oracle"
)

func (s StandardOfProof) valid() bool {
	switch s {
	case "", StandardScoping, StandardPlausible, StandardConcordant, StandardOracle:
		return true
	}
	return false
}

// Transport is the rung of the placement ladder a node lands on (architecture
// §7). Set by the placer, not the author.
type Transport string

const (
	TransportGoroutine Transport = "goroutine"   // default; same trust + budget tree
	TransportA2A       Transport = "a2a-session" // AgentCore session (isolation / caps)
	TransportInstance  Transport = "instance"    // spore.host instance (GPU / sovereign / heavy)
)
