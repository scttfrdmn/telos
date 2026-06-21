// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package research is the research domain pack — the ONLY research-aware
// component (architecture §10). It maps a question to an inquiry SHAPE: an
// archetype and the ACS graph that archetype implies, each carrying a
// verification structure. Keeping research-awareness confined here keeps the
// core generic (composition, not codegen).
//
// M3 implements the two archetypes §14 needs plus the COMPOSITE of them:
//   - evidence-synthesis: retrieve → parallel extract → synthesize.
//   - mechanistic: assemble for/against → reconcile (may return *contested*).
//   - composite: an evidence-synthesis SUBSTRATE feeding a mechanistic HEAD —
//     the shape "does X modulate Y, and what's the evidence" requires (§14 #1).
package research

import "strings"

// Archetype is the inferred inquiry shape.
type Archetype string

const (
	ArchetypeEvidenceSynthesis Archetype = "evidence-synthesis"
	ArchetypeMechanistic       Archetype = "mechanistic"
	// ArchetypeComposite is evidence-synthesis substrate → mechanistic head. It
	// is not a third kind of inquiry but the CONJUNCTION of two acts in one
	// question; detecting it (vs. flattening to one archetype) is §14 check #1.
	ArchetypeComposite Archetype = "composite"
)

// Classification is the result of reading a question: its archetype plus the
// evidence that led there, so the inference is auditable rather than opaque.
type Classification struct {
	Archetype Archetype
	// Mechanistic reports a causal/mechanistic act ("does X modulate/cause Y").
	Mechanistic bool
	// EvidenceAsk reports an explicit ask for the evidence/literature.
	EvidenceAsk bool
	// Rationale is a short human-legible explanation of the classification.
	Rationale string
}

// mechanisticVerbs signal a causal/mechanistic act. "modulate" is the §14 verb;
// it deliberately HIDES direction (up/down/bidirectional), which the scoping
// pass must open rather than assume.
var mechanisticVerbs = []string{
	"modulate", "cause", "causes", "regulate", "drive", "drives", "affect",
	"affects", "influence", "induce", "inhibit", "promote", "mediate", "trigger",
}

// evidenceAskMarkers signal an explicit "and what's the evidence" conjunction.
var evidenceAskMarkers = []string{
	"what's the evidence", "what is the evidence", "and the evidence",
	"current evidence", "what evidence", "evidence for", "literature",
	"what's known", "what is known",
}

// Classify reads a question and infers its archetype. Composite is detected when
// the question conjoins a mechanistic act with an explicit evidence ask — two
// acts in one question. A planner that flattens this to a single archetype fails
// §14 check #1, so this is deliberately a structural detection, not a guess.
func Classify(prompt string) Classification {
	p := strings.ToLower(prompt)

	mech := containsAny(p, mechanisticVerbs)
	evid := containsAny(p, evidenceAskMarkers)
	// The conjunction "..., and ..." is the syntactic tell of two conjoined acts.
	conjoined := strings.Contains(p, ", and ") || strings.Contains(p, "; and ") ||
		(mech && evid)

	switch {
	case mech && evid && conjoined:
		return Classification{
			Archetype:   ArchetypeComposite,
			Mechanistic: true,
			EvidenceAsk: true,
			Rationale:   "conjoined acts: a mechanistic question ('" + firstVerb(p) + "') AND an explicit evidence ask → evidence-synthesis substrate feeding a mechanistic head",
		}
	case mech:
		return Classification{
			Archetype:   ArchetypeMechanistic,
			Mechanistic: true,
			Rationale:   "mechanistic act ('" + firstVerb(p) + "') without a distinct evidence ask",
		}
	default:
		return Classification{
			Archetype:   ArchetypeEvidenceSynthesis,
			EvidenceAsk: evid,
			Rationale:   "no mechanistic act detected → evidence synthesis",
		}
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func firstVerb(p string) string {
	for _, v := range mechanisticVerbs {
		if strings.Contains(p, v) {
			return v
		}
	}
	return ""
}
