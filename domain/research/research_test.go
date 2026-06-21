// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package research

import (
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

const trem2 = "does microglial TREM2 signaling modulate tau propagation in the entorhinal cortex, and what's the current evidence?"

// §14 check #1: the TREM2 string is COMPOSITE — a planner that flattens it to one
// archetype fails here.
func TestClassify_TREM2IsComposite(t *testing.T) {
	c := Classify(trem2)
	if c.Archetype != ArchetypeComposite {
		t.Fatalf("TREM2 should be composite, got %s (%s)", c.Archetype, c.Rationale)
	}
	if !c.Mechanistic || !c.EvidenceAsk {
		t.Fatalf("composite must detect both the mechanistic act and the evidence ask: %+v", c)
	}
}

func TestClassify_PureMechanisticAndPureEvidence(t *testing.T) {
	if c := Classify("does X cause Y?"); c.Archetype != ArchetypeMechanistic {
		t.Fatalf("pure mechanistic misclassified: %s", c.Archetype)
	}
	if c := Classify("what is the current evidence on Z?"); c.Archetype != ArchetypeEvidenceSynthesis {
		t.Fatalf("pure evidence misclassified: %s", c.Archetype)
	}
}

// §14 check #2: scoping lands between flatten and explode and opens the called-out
// axes.
func TestScope_TREM2BoundedNotFlatNotExploded(t *testing.T) {
	s := Scope(trem2, Classify(trem2))
	if !s.Within() {
		t.Fatalf("scope must land between flatten(%d) and explode(%d); got %d entities",
			s.MinEntities, s.MaxEntities, len(s.Entities))
	}
	// The four axes §14 names must be present (by substring, direction-neutral).
	want := []string{"pathway", "direction", "propagation", "entorhinal"}
	joined := ""
	for _, e := range s.Entities {
		joined += e.Name + "|" + e.Reason + "\n"
	}
	for _, w := range want {
		if !contains(joined, w) {
			t.Fatalf("scope missing the %q axis; expansion was:\n%s", w, joined)
		}
	}
	// Explode-risk expansions are auditably dropped.
	if len(s.Dropped) == 0 {
		t.Fatal("scope should record what it dropped (explode guard auditable)")
	}
}

// The composite shape is the two-phase graph (substrate → head) and is valid,
// including the invariant-10 acceptance separation surviving EMISSION.
func TestPlan_CompositeShapeValid(t *testing.T) {
	b := acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"}
	spec := Plan(trem2, Classify(trem2), b, acs.StandardConcordant)
	if err := spec.Validate(); err != nil {
		t.Fatalf("composite shape invalid: %v", err)
	}
	// substrate feeds head (two-phase), and an isolated-envelope acceptance node
	// exists.
	if _, ok := spec.Nodes["substrate"]; !ok {
		t.Fatal("composite shape missing evidence-synthesis substrate")
	}
	if _, ok := spec.Nodes["head"]; !ok {
		t.Fatal("composite shape missing mechanistic head")
	}
	acc := spec.Nodes["accept"]
	if acc == nil || acc.Kind != acs.KindAcceptance || acc.Trust != acs.TrustIsolated {
		t.Fatalf("composite shape must place a separate-envelope acceptance node, got %+v", acc)
	}
	// The head must assemble BOTH directions (for/against) — the structure that
	// lets contested be earned.
	if _, ok := spec.Nodes["evidence_for"]; !ok {
		t.Fatal("head missing evidence_for")
	}
	if _, ok := spec.Nodes["evidence_against"]; !ok {
		t.Fatal("head missing evidence_against")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
