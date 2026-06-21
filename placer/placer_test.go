// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package placer

import (
	"context"
	"testing"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/transport"
)

func place(t *testing.T, n *acs.Node) Decision {
	t.Helper()
	d, err := New().Place(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Default: a same-envelope node with no gravity stays on the goroutine rung.
func TestPlace_DefaultGoroutine(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustSameEnvelope})
	if d.Rung != transport.RungGoroutine || d.Substrate != "inproc" {
		t.Fatalf("default should be goroutine/inproc, got %+v", d)
	}
	if d.Trigger != "default" {
		t.Fatalf("trigger = %q, want default", d.Trigger)
	}
}

// Trigger 1: an isolated node is promoted to the A2A session rung.
func TestPlace_IsolatedTrustForcesA2A(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustIsolated})
	if d.Rung != transport.RungA2ASession || d.Substrate != "agentcore" {
		t.Fatalf("isolated should force a2a-session/agentcore, got %+v", d)
	}
	if d.Trigger != "trust:isolated" {
		t.Fatalf("trigger = %q", d.Trigger)
	}
}

func TestPlace_UntrustedForcesA2A(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustUntrusted})
	if d.Rung != transport.RungA2ASession {
		t.Fatalf("untrusted should force a2a, got %+v", d)
	}
}

// Trigger 2: resource gravity promotes a same-envelope node to A2A.
func TestPlace_GravityForcesA2A(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustSameEnvelope, Gravity: acs.GravityData})
	if d.Rung != transport.RungA2ASession {
		t.Fatalf("data gravity should force a2a, got %+v", d)
	}
	if d.Trigger != "gravity:data" {
		t.Fatalf("trigger = %q", d.Trigger)
	}
}

// First-trigger-wins: trust isolation wins over gravity (evaluated first).
func TestPlace_FirstTriggerWins(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustIsolated, Gravity: acs.GravityCompute})
	if d.Trigger != "trust:isolated" {
		t.Fatalf("trust should win over gravity (first-trigger), got trigger %q", d.Trigger)
	}
}

// The placement renders into the acs annotation form, and the rung's fallback
// ladder is carried (a promoted rung can still advance to instance at M6/M7).
func TestPlace_RendersACSAndCarriesLadder(t *testing.T) {
	d := place(t, &acs.Node{ID: "n", Trust: acs.TrustIsolated})
	if d.AsACS().Transport != acs.TransportA2A {
		t.Fatalf("acs transport = %v", d.AsACS().Transport)
	}
	// From a2a-session, the ladder can still advance to instance.
	if _, ok := d.Placement.Advance(); !ok {
		t.Fatal("a2a-session placement should advance to the instance rung")
	}
}
