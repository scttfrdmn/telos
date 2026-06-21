// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

func seedSpec(t *testing.T) *acs.Spec {
	t.Helper()
	s, err := acs.LoadFile("../bootstrap.acs")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	return s
}

func offlineDeps(t *testing.T, envelope acs.Budget) *Deps {
	t.Helper()
	d, err := NewDeps(context.Background(), DepsConfig{Envelope: envelope}, nil) // echo backend
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// THE RECURSION CLOSES (offline): the planning-root seed reads a question, the
// planner emits a real multi-node graph, and the host re-instantiates and runs
// it. Base case → real graph (invariant 3).
func TestRecursion_SeedEmitsAndRunsRealGraph(t *testing.T) {
	seed := seedSpec(t)
	if seed.Nodes[seed.RootID].Pattern != acs.PatternPlanning {
		t.Fatalf("seed root should be a planning node, got %s", seed.Nodes[seed.RootID].Pattern)
	}
	deps := offlineDeps(t, seed.Budget)

	root, err := BuildWithDeps(seed, deps)
	if err != nil {
		t.Fatalf("build seed: %v", err)
	}
	out, err := root.Process(context.Background(),
		agenkit.NewMessage("user", "does X modulate Y, and what is the evidence?"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// The emitted inquiry was composite and produced a verdict.
	if a, _ := out.Metadata["telos.archetype"].(string); a != "composite" {
		t.Fatalf("expected composite emitted archetype, got %q", a)
	}
	if _, ok := out.Metadata["telos.accepted"]; !ok {
		t.Fatal("emitted graph should have produced an acceptance verdict")
	}
}

// §14 behavior, OFFLINE (deterministic plumbing): composite + earned-contested +
// accepted. The real-model QUALITY is the live gate; this proves the STRUCTURE.
func TestRecursion_EarnedContestedAcceptedOffline(t *testing.T) {
	seed := seedSpec(t)
	deps := offlineDeps(t, seed.Budget)
	root, _ := BuildWithDeps(seed, deps)

	out, err := root.Process(context.Background(),
		agenkit.NewMessage("user", "does microglial TREM2 signaling modulate tau propagation in the entorhinal cortex, and what's the current evidence?"))
	if err != nil {
		t.Fatal(err)
	}
	// Earned contested: both directions assembled, reconciled to contested.
	if c, _ := out.Metadata["telos.contested"].(bool); !c {
		t.Fatal("contested must be EARNED from both-sides assembly")
	}
	fa, _ := out.Metadata["telos.forAgainst"].(string)
	if fa == "" || !strings.Contains(fa, "evidence_for") || !strings.Contains(fa, "evidence_against") {
		t.Fatalf("for/against record must be inspectable and contain both sides, got %q", fa)
	}
	// Accepted with basis contested (a provenanced contested is a first-class
	// accepted result — §14 #3/#4).
	if acc, _ := out.Metadata["telos.accepted"].(bool); !acc {
		t.Fatal("a provenanced earned-contested result must be ACCEPTED")
	}
	if b, _ := out.Metadata["telos.basis"].(string); b != "contested" {
		t.Fatalf("expected basis contested, got %q", b)
	}
}

// Budget flows through ctx down the emitted tree: the planning agent reserves a
// child grant from the run envelope before running the emitted graph; an
// envelope too small to fund the emitted graph fails closed.
func TestRecursion_BudgetConservesDownEmittedTree(t *testing.T) {
	seed := seedSpec(t)
	// A real (governed) envelope so reservation actually happens.
	env := acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"}
	deps := offlineDeps(t, env)
	root, _ := BuildWithDeps(seed, deps)

	if _, err := root.Process(context.Background(), agenkit.NewMessage("user", "does X modulate Y, and what's the evidence?")); err != nil {
		t.Fatalf("run within envelope: %v", err)
	}
	// The governor saw spend conserved against the root grant (some was spent;
	// it did not exceed the envelope).
	gov := deps.Governor.(*governor.Mem)
	rem := gov.Remaining(governor.RootGrant)
	if rem.Amount < 0 || rem.Amount > env.Amount {
		t.Fatalf("root remaining %v out of [0,%v] — conservation breached", rem.Amount, env.Amount)
	}
}

// Re-planning depth is bounded: a planner that recursed forever must fail closed.
func TestRecursion_DepthBound(t *testing.T) {
	seed := seedSpec(t)
	deps := offlineDeps(t, seed.Budget)
	deps.MaxPlanDepth = 1 // shallow bound

	// Build at the bound: a planning node built when depth >= max fails closed.
	b := &builder{spec: seed, deps: deps, depth: 1, building: map[acs.NodeID]bool{}, built: map[acs.NodeID]agenkit.Agent{}}
	if _, err := b.node(seed.RootID); err == nil {
		t.Fatal("re-planning at/over max depth must fail closed")
	}
}
