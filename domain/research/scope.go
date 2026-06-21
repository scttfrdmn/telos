// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package research

import "strings"

// Scoping is the AUDITABLE result of the estimate-first scoping pass (§3, §14
// #2): the entity expansion and the bounds applied. It is emitted (not just
// asserted) so a human can verify scope landed between FLATTEN (under-fan →
// shallow) and EXPLODE (pathway dissertation → blown envelope). Mirrors how M2
// made burn-rate's curve inspectable.
type Scoping struct {
	// Entities is the bounded expansion: each axis the inquiry must cover, with
	// WHY it was kept (and, for axes deliberately dropped, why).
	Entities []ScopedEntity
	// MinEntities / MaxEntities are the flatten/explode bounds applied. The
	// expansion size must land within [MinEntities, MaxEntities] — outside is the
	// failure §14 #2 guards against.
	MinEntities int
	MaxEntities int
	// Dropped lists candidate expansions deliberately NOT taken (with reasons),
	// so over-pruning (flatten) is auditable too.
	Dropped []string
	// Note summarizes the bounding decision.
	Note string
}

// ScopedEntity is one axis of the bounded inquiry, with its rationale.
type ScopedEntity struct {
	Name   string // e.g. "TREM2 pathway"
	Reason string // why this axis is in scope and how it's framed
}

// Within reports whether the expansion landed between flatten and explode.
func (s Scoping) Within() bool {
	n := len(s.Entities)
	return n >= s.MinEntities && n <= s.MaxEntities
}

// scope bounds: an inquiry that fans to fewer than minEntities is FLAT (shallow);
// more than maxEntities is EXPLODED (a dissertation that blows the envelope).
// These are the calibration knobs §14 #2 is about; provisional, and the §15
// scoping-depth fork (#3) governs them — surfaced, not resolved here.
const (
	minEntities = 3
	maxEntities = 6
)

// Scope produces the bounded entity expansion for a classified question. It is
// deterministic and offline (the STRUCTURE of scoping is testable without a
// model; a real model fills in domain specifics at runtime). For the §14 TREM2
// class it opens exactly the axes the test calls out — TREM2 as a pathway,
// "modulate" as direction-bearing, tau propagation as contested, EC as Braak-I —
// and stops there, neither flattening to one node nor exploding into the whole
// pathway literature.
func Scope(prompt string, c Classification) Scoping {
	p := strings.ToLower(prompt)
	var ents []ScopedEntity
	var dropped []string

	// Axis 1: the agent/pathway — kept as a PATHWAY, not a single node, so its
	// components can be reasoned about without exploding into every member.
	if subj := detectSubject(p); subj != "" {
		ents = append(ents, ScopedEntity{
			Name:   subj + " pathway",
			Reason: "treated as a pathway (signaling axis), not a single node — bounded to the pathway, not its full membership",
		})
	}

	// Axis 2: the relation — a mechanistic verb HIDES direction; open it.
	if c.Mechanistic {
		if v := firstVerb(p); v != "" {
			ents = append(ents, ScopedEntity{
				Name:   "direction of \"" + v + "\"",
				Reason: "the verb hides direction (up / down / bidirectional); opened as an axis rather than assumed",
			})
		}
	}

	// Axis 3: the target process — flag it as itself contested when it is a
	// spread/propagation claim (a settled premise would flatten the inquiry).
	if obj := detectObject(p); obj != "" {
		reason := "the target process"
		if strings.Contains(p, "propagation") || strings.Contains(p, "spread") {
			reason = "the target process is ITSELF contested (propagation/spread is debated) — not taken as a settled premise"
		}
		ents = append(ents, ScopedEntity{Name: obj, Reason: reason})
	}

	// Axis 4: the locus — anatomical/temporal context that changes the answer.
	if loc := detectLocus(p); loc != "" {
		ents = append(ents, ScopedEntity{
			Name:   loc,
			Reason: "locus matters (e.g. an early-stage origin site); stage-dependence is in scope",
		})
	}

	// Bounding: if too few axes were found, the inquiry is flat — note it. If a
	// real model later over-expands, the MaxEntities bound is the explode guard;
	// here we deliberately STOP at the called-out axes rather than enumerating the
	// whole pathway literature.
	dropped = append(dropped,
		"full TREM2 pathway membership (would explode the envelope)",
		"every downstream tau species (kept to the propagation claim)",
	)

	note := "bounded between flatten and explode: opened the agent-as-pathway, the hidden direction, the contested target, and the locus; dropped exhaustive pathway/membership enumeration"
	if len(ents) < minEntities {
		note = "WARNING: expansion is flat (under-fanned) — fewer axes than the flatten bound"
	}
	if len(ents) > maxEntities {
		note = "WARNING: expansion is exploded — more axes than the explode bound"
	}

	return Scoping{
		Entities:    ents,
		MinEntities: minEntities,
		MaxEntities: maxEntities,
		Dropped:     dropped,
		Note:        note,
	}
}

// The detectors are deliberately simple keyword heuristics: M3's job is to make
// the scoping STRUCTURE correct and inspectable; a real model refines the domain
// specifics at runtime. They recognize the §14 TREM2 entities concretely.

func detectSubject(p string) string {
	for _, s := range []string{"trem2", "microglial trem2", "microglia"} {
		if strings.Contains(p, s) {
			if s == "trem2" || s == "microglial trem2" {
				return "TREM2"
			}
			return "microglia"
		}
	}
	return firstNoun(p)
}

func detectObject(p string) string {
	switch {
	case strings.Contains(p, "tau propagation"):
		return "tau propagation"
	case strings.Contains(p, "tau"):
		return "tau"
	}
	return ""
}

func detectLocus(p string) string {
	switch {
	case strings.Contains(p, "entorhinal cortex"), strings.Contains(p, "entorhinal"):
		return "entorhinal cortex (Braak-I origin)"
	}
	return ""
}

// firstNoun is a last-resort subject guess for non-TREM2 questions (keeps Scope
// total without a tokenizer); returns "" when nothing obvious is found.
func firstNoun(p string) string {
	fields := strings.Fields(p)
	for _, f := range fields {
		if len(f) > 3 && !stopword(f) {
			return strings.Trim(f, ",.?\"")
		}
	}
	return ""
}

func stopword(w string) bool {
	switch w {
	case "does", "what", "what's", "the", "and", "current", "evidence", "is", "are", "in", "of", "signaling":
		return true
	}
	return false
}
