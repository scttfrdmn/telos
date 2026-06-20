// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import (
	"strings"
	"testing"
	"time"
)

// minimalValidSpec is a small, valid composition: a sequential root over two
// leaf reasoners, with a separately-enveloped acceptance node fed by a producer.
func minimalValidSpec() *Spec {
	return &Spec{
		Version:   SchemaVersion,
		Prompt:    "test question",
		Archetype: ArchetypeNone,
		RootID:    "root",
		Budget:    Budget{Amount: 100, Period: 30 * 24 * time.Hour, Currency: "USD"},
		Nodes: map[NodeID]*Node{
			"root": {
				ID: "root", Kind: KindReason, Pattern: PatternSequential,
				Trust:    TrustSameEnvelope,
				Budget:   BudgetRequest{Amount: 100, Period: 30 * 24 * time.Hour},
				Children: []NodeID{"worker", "judge"},
			},
			"worker": {
				ID: "worker", Kind: KindReason, Pattern: PatternLeaf,
				Trust:  TrustSameEnvelope,
				Budget: BudgetRequest{Amount: 50, Period: 30 * 24 * time.Hour},
			},
			"judge": {
				ID: "judge", Kind: KindAcceptance, Pattern: PatternLeaf,
				Trust:  TrustIsolated, // separate envelope (invariant 10)
				Budget: BudgetRequest{Amount: 10, Period: 30 * 24 * time.Hour},
			},
		},
		Edges: []Edge{
			{From: "worker", To: "judge", Kind: EdgeDataflow, Ref: "worker.out"},
		},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := minimalValidSpec().Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_BudgetMustBeGrant(t *testing.T) {
	s := minimalValidSpec()
	s.Budget.Period = 0 // a total, not a grant
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "period must be positive") {
		t.Fatalf("expected grant-shape rejection, got: %v", err)
	}
}

func TestValidate_AcceptanceSameEnvelopeRejected(t *testing.T) {
	s := minimalValidSpec()
	s.Nodes["judge"].Trust = TrustSameEnvelope
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "separate trust envelope") {
		t.Fatalf("expected acceptance same-envelope rejection (invariant 10), got: %v", err)
	}
}

func TestValidate_AcceptanceComposingProducerRejected(t *testing.T) {
	s := minimalValidSpec()
	// Make the judge produce the record it rules on — self-settling.
	s.Nodes["judge"].Pattern = PatternSequential
	s.Nodes["judge"].Children = []NodeID{"worker"}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "does not produce what it judges") {
		t.Fatalf("expected acceptance-composes-producer rejection (invariant 10), got: %v", err)
	}
}

// A neutral coordinator (the sequential root) wiring producers and a
// separately-enveloped acceptance node as siblings is the CORRECT shape — it
// must validate, not trip the seam check. (Guards against the over-strict rule
// that an earlier draft had.)
func TestValidate_CoordinatorWithJudgeSiblingOK(t *testing.T) {
	if err := minimalValidSpec().Validate(); err != nil {
		t.Fatalf("coordinator + separately-enveloped judge sibling must be valid, got: %v", err)
	}
}

func TestValidate_CycleRejected(t *testing.T) {
	s := minimalValidSpec()
	// worker -> root cycle
	s.Nodes["worker"].Pattern = PatternSequential
	s.Nodes["worker"].Children = []NodeID{"root"}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle rejection, got: %v", err)
	}
}

func TestValidate_DanglingChildRejected(t *testing.T) {
	s := minimalValidSpec()
	s.Nodes["root"].Children = []NodeID{"worker", "judge", "ghost"}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected dangling-child rejection, got: %v", err)
	}
}

func TestHash_StableAndSelfConsistent(t *testing.T) {
	s := minimalValidSpec()
	h1, err := s.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	// Recompute: hash must be stable and must not fold in the stored hash.
	h2, err := s.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	if err := s.VerifyHash(); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Fatalf("unexpected hash format: %s", h1)
	}
}

func TestHash_ChangesWithContent(t *testing.T) {
	s := minimalValidSpec()
	h1, _ := s.ComputeHash()
	s.Prompt = "a different question"
	h2, _ := s.ComputeHash()
	if h1 == h2 {
		t.Fatal("hash did not change with content")
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	orig := minimalValidSpec()
	data, err := orig.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Hash != orig.Hash {
		t.Fatalf("hash drift across round-trip: %s vs %s", got.Hash, orig.Hash)
	}
	if got.RootID != orig.RootID || len(got.Nodes) != len(orig.Nodes) {
		t.Fatal("structural drift across round-trip")
	}
	if got.Budget.Rate() != orig.Budget.Rate() {
		t.Fatal("budget rate drift across round-trip")
	}
}

func TestLoad_TamperedHashRejected(t *testing.T) {
	orig := minimalValidSpec()
	data, _ := orig.Marshal()
	tampered := strings.Replace(string(data), orig.Prompt, "tampered prompt", 1)
	if _, err := Load([]byte(tampered)); err == nil {
		t.Fatal("expected hash-mismatch rejection on tampered content")
	}
}

func TestLoad_UnknownFieldRejected(t *testing.T) {
	bad := `{"version":"v0.1","prompt":"x","archetype":"none","root_id":"r",
		"budget":{"amount":1,"period":1},"nodes":{},"surprise":true}`
	if _, err := Load([]byte(bad)); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

func TestBudget_RateRequiresClock(t *testing.T) {
	b := Budget{Amount: 100, Period: 0}
	if b.Rate() != 0 {
		t.Fatal("a period-less budget must not yield a spend rate")
	}
	if err := b.Validate(); err == nil {
		t.Fatal("a period-less budget is a total, must be invalid")
	}
}
