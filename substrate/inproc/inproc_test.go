// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package inproc

import (
	"context"
	"testing"

	"github.com/scttfrdmn/telos/transport"
	"github.com/spore-host/cohort"
)

// The goroutine substrate fills cohort's provider seam and reconciles a node to
// PhaseReady through the UNMODIFIED cohort core — using a transport Placement
// with no EC2 vocabulary. This is the third-consumer proof for the inproc rung.
func TestInproc_ReconcilesThroughCohort(t *testing.T) {
	sub := New()
	r := cohort.NewReconciler(sub, sub, sub, nil, nil, nil)

	intent, err := cohort.NewEntityIntent("telos", "node-1", "g1", "c1",
		transport.NewPlacement(transport.RungGoroutine, transport.DefaultLadder), "")
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	c, err := cohort.NewSerialCohort("c1", intent, cohort.PhaseBudget{})
	if err != nil {
		t.Fatalf("cohort: %v", err)
	}

	outcome, err := r.Reconcile(context.Background(), c)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !outcome.Ready {
		t.Fatalf("goroutine node not ready: %s", outcome.Records["node-1"].Summary())
	}
	rec := outcome.Records["node-1"]
	if !rec.Succeeded() {
		t.Fatalf("expected success, got: %s", rec.Summary())
	}
	// Legibility: the Record renders the transport rung name, no EC2 vocabulary.
	if explain := rec.Explain(); explain == "" {
		t.Fatal("empty Explain()")
	}
}

// Classify never returns FaultAmbiguous (cohort contract) and maps cancel/error
// to legible classes.
func TestInproc_Classify(t *testing.T) {
	sub := New()
	if f := sub.Classify(context.Canceled); f.Class != cohort.FaultTerminal || f.Code != "ContextCanceled" {
		t.Fatalf("cancel classify = %+v", f)
	}
	if f := sub.Classify(context.DeadlineExceeded); f.Code != "DeadlineExceeded" {
		t.Fatalf("deadline classify = %+v", f)
	}
}

// An un-launched entity observes StateUnknown (lag), never StateAbsent.
func TestInproc_UnlaunchedIsUnknownNotAbsent(t *testing.T) {
	sub := New()
	obs, err := sub.Observe(context.Background(), []cohort.EntityID{"ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if obs[0].State != cohort.StateUnknown {
		t.Fatalf("un-launched should be StateUnknown (lag), got %v", obs[0].State)
	}
}
