// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// fourPatternSpec is a hand-built spec exercising all four M0 patterns plus a
// separately-enveloped acceptance node — the same shape as bootstrap.acs but
// kept local so the test does not depend on the generated file.
func fourPatternSpec(t *testing.T) *acs.Spec {
	t.Helper()
	const p = 24 * time.Hour
	req := func(a float64) acs.BudgetRequest { return acs.BudgetRequest{Amount: a, Period: p} }
	s := &acs.Spec{
		Version:   acs.SchemaVersion,
		Prompt:    "test",
		Archetype: acs.ArchetypeNone,
		Standard:  acs.StandardConcordant,
		RootID:    "root",
		Budget:    acs.Budget{Amount: 100, Period: p, Currency: "USD"},
		Nodes: map[acs.NodeID]*acs.Node{
			"root": {ID: "root", Kind: acs.KindPlan, Pattern: acs.PatternSequential, Trust: acs.TrustSameEnvelope,
				Budget: req(100), Children: []acs.NodeID{"scope", "sup", "accept"}},
			"scope": {ID: "scope", Kind: acs.KindReason, Pattern: acs.PatternReact, Trust: acs.TrustSameEnvelope,
				Budget: req(5), Tools: []acs.ToolRef{{Name: "search"}}},
			"sup": {ID: "sup", Kind: acs.KindReason, Pattern: acs.PatternSupervisor, Trust: acs.TrustSameEnvelope,
				Budget: req(50), Children: []acs.NodeID{"par", "leaf"}},
			"par": {ID: "par", Kind: acs.KindReason, Pattern: acs.PatternParallel, Trust: acs.TrustSameEnvelope,
				Budget: req(20), Children: []acs.NodeID{"w1", "w2"}},
			"w1":     {ID: "w1", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Trust: acs.TrustSameEnvelope, Budget: req(10)},
			"w2":     {ID: "w2", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Trust: acs.TrustSameEnvelope, Budget: req(10)},
			"leaf":   {ID: "leaf", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Trust: acs.TrustSameEnvelope, Budget: req(10)},
			"accept": {ID: "accept", Kind: acs.KindAcceptance, Pattern: acs.PatternLeaf, Trust: acs.TrustIsolated, Budget: req(10)},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}
	return s
}

func TestBuild_AllFourPatterns(t *testing.T) {
	root, err := Build(fourPatternSpec(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := root.Process(context.Background(), agenkit.NewMessage("user", "go"))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	got := out.ContentString()
	// The sequential root ends with the acceptance node, so its output is the
	// acceptance marker — proving acceptance was instantiated in the graph.
	if !strings.Contains(got, "not adjudicated") {
		t.Fatalf("expected acceptance marker in final output, got: %q", got)
	}
}

func TestBuild_BootstrapSeed(t *testing.T) {
	seed, err := acs.LoadFile("../bootstrap.acs")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	root, err := Build(seed)
	if err != nil {
		t.Fatalf("build seed: %v", err)
	}
	if _, err := root.Process(context.Background(), agenkit.NewMessage("user", "investigate")); err != nil {
		t.Fatalf("run seed graph: %v", err)
	}
}

// TestBuild_AcceptanceIsInertNode is the package-level half of the invariant-10
// seam: a KindAcceptance node is built via acceptance.NewInertNode, never as a
// producing stub. We assert it renders no judgement on content.
func TestBuild_AcceptanceIsInertNode(t *testing.T) {
	s := fourPatternSpec(t)
	// Build just the acceptance node by making it the root.
	s.RootID = "accept"
	s.Nodes["accept"].Trust = acs.TrustIsolated
	// Trim to a valid single-node spec.
	s.Nodes = map[acs.NodeID]*acs.Node{"accept": s.Nodes["accept"]}
	s.Edges = nil
	if err := s.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}
	root, err := Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := root.Process(context.Background(), agenkit.NewMessage("user", "is this true?"))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if k, _ := out.Metadata["telos.kind"].(string); k != "acceptance" {
		t.Fatalf("acceptance node did not tag itself as acceptance: %v", out.Metadata)
	}
	if strings.Contains(strings.ToLower(out.ContentString()), "true") {
		t.Fatalf("acceptance node asserted a truth value (forbidden): %q", out.ContentString())
	}
}

func TestBuild_ContextCancellation(t *testing.T) {
	root, err := Build(fourPatternSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // exhaustion / kill-switch is ctx cancel (invariant 11)
	if _, err := root.Process(ctx, agenkit.NewMessage("user", "go")); err == nil {
		t.Fatal("expected cancellation to propagate as an error")
	}
}
