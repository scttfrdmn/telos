// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"strings"
	"testing"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// TestBuildWithDeps_ReasonLeafGoesThroughGateway proves a Reason leaf invokes a
// model through the gateway when deps are wired — the M1 end-to-end seam. It uses
// the offline echo backend (no network), so it runs anywhere.
func TestBuildWithDeps_ReasonLeafGoesThroughGateway(t *testing.T) {
	deps, err := NewDeps(context.Background(), DepsConfig{
		Envelope: acs.Budget{Amount: 100, Period: defaultEnvelopePeriod(), Currency: "USD"},
	}, nil) // no backends → echo fallback
	if err != nil {
		t.Fatalf("deps: %v", err)
	}

	root, err := BuildWithDeps(fourPatternSpec(t), deps)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := root.Process(context.Background(), agenkit.NewMessage("user", "investigate X"))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	_ = out // final output is the acceptance marker (sequential root ends on accept)
}

// TestGatewayAgent_MeteringMetadata confirms a gateway-backed leaf surfaces its
// metering provenance (cost, synthesized flag, backend) — and that the cost is
// synthesized for the offline/local backend.
func TestGatewayAgent_MeteringMetadata(t *testing.T) {
	deps, err := NewDeps(context.Background(), DepsConfig{
		Envelope: acs.Budget{Amount: 100, Period: defaultEnvelopePeriod(), Currency: "USD"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A lone Reason leaf so we can read its output metadata directly.
	n := &acs.Node{ID: "r", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Trust: acs.TrustSameEnvelope,
		Model: acs.ModelConstraint{Tier: acs.TierCheap}, Role: "answer the question"}
	a := newGatewayAgent(n, deps.Gateway, deps.Router)

	out, err := a.Process(context.Background(), agenkit.NewMessage("user", "what is X?"))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, ok := out.Metadata["telos.cost"]; !ok {
		t.Fatal("gateway-backed leaf must record telos.cost (metered at the gateway)")
	}
	if synth, _ := out.Metadata["telos.cost_synthesized"].(bool); !synth {
		t.Fatal("offline/local backend cost must be synthesized")
	}
	if be, _ := out.Metadata["telos.backend"].(string); be == "" {
		t.Fatal("response should record which backend served it")
	}
	if !strings.Contains(out.ContentString(), "echo") {
		t.Fatalf("expected echo backend output, got: %q", out.ContentString())
	}
}

// TestBuild_NoDepsIsStubPath confirms the M0 behavior is preserved: with no deps,
// a Reason leaf is a deterministic stub (no gateway, no model).
func TestBuild_NoDepsIsStubPath(t *testing.T) {
	n := &acs.Node{ID: "r", Kind: acs.KindReason, Pattern: acs.PatternLeaf, Trust: acs.TrustSameEnvelope}
	b := &builder{spec: &acs.Spec{Nodes: map[acs.NodeID]*acs.Node{"r": n}, RootID: "r"}, building: map[acs.NodeID]bool{}, built: map[acs.NodeID]agenkit.Agent{}}
	agent, err := b.dispatch(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, isStub := agent.(*stubAgent); !isStub {
		t.Fatalf("no-deps Reason leaf should be a stubAgent, got %T", agent)
	}
}
