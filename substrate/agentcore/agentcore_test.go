// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package agentcore

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/scttfrdmn/telos/transport"
	"github.com/spore-host/cohort"
)

// a minimal session handler standing in for a Telos host (avoids importing host
// here — the substrate takes an injected http.Handler, dependency points one way).
func okSession() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.HandleFunc("POST /invocations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":"ok"}`))
	})
	return mux
}

// The A2A-session substrate fills cohort's provider seam and reconciles a node to
// PhaseReady through the UNMODIFIED core — a session launched over the contract,
// observed ready via /ping, with a transport Placement (no EC2 vocabulary).
func TestAgentcore_ReconcilesThroughCohort(t *testing.T) {
	sub := New(func(ctx context.Context, id cohort.EntityID) (http.Handler, error) {
		return okSession(), nil
	})
	r := cohort.NewReconciler(sub, sub, sub, nil, nil, nil)

	intent, _ := cohort.NewEntityIntent("telos", "session-1", "g1", "c1",
		transport.NewPlacement(transport.RungA2ASession, transport.DefaultLadder), "")
	c, _ := cohort.NewSerialCohort("c1", intent, cohort.PhaseBudget{})

	outcome, err := r.Reconcile(context.Background(), c)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !outcome.Ready {
		t.Fatalf("a2a session not ready: %s", outcome.Records["session-1"].Summary())
	}
	defer sub.Terminate(context.Background(), "session-1")
}

// THE OBSERVER IS THE DESIGN: a DEAD session (factory fails) yields a legible
// cohort Record with a real disposition — never a bare error, never a vanished
// entity. This is cohort's legibility rule, inherited.
func TestAgentcore_DeadSessionIsLegible(t *testing.T) {
	sub := New(func(ctx context.Context, id cohort.EntityID) (http.Handler, error) {
		return nil, errors.New("session bootstrap failed: image pull error")
	})
	r := cohort.NewReconciler(sub, sub, sub, nil, nil, nil)

	intent, _ := cohort.NewEntityIntent("telos", "session-x", "g1", "c1",
		transport.NewPlacement(transport.RungA2ASession, transport.DefaultLadder), "")
	c, _ := cohort.NewSerialCohort("c1", intent, cohort.PhaseBudget{})

	outcome, err := r.Reconcile(context.Background(), c)
	if err != nil {
		t.Fatalf("reconcile returned a bare error instead of a legible outcome: %v", err)
	}
	if outcome.Ready {
		t.Fatal("a failed-launch session must not be Ready")
	}
	rec := outcome.Records["session-x"]
	// A real disposition, not "it didn't work": a Terminal fault with a code.
	if rec.Terminal == nil {
		t.Fatalf("dead session must carry a Terminal fault, got: %s", rec.Summary())
	}
	if rec.Summary() == "" {
		t.Fatal("dead session must have a legible Summary()")
	}
	t.Logf("legible dead-session record: %s", rec.Summary())
}

func TestAgentcore_Classify(t *testing.T) {
	sub := New(nil)
	if f := sub.Classify(context.Canceled); f.Code != "ContextCanceled" {
		t.Fatalf("cancel classify = %+v", f)
	}
	if f := sub.Classify(errors.New("boom")); f.Class != cohort.FaultTerminal || f.Code != "A2ASessionError" {
		t.Fatalf("generic classify = %+v", f)
	}
}
