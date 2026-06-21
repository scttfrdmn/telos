// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"context"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/compute"
	"github.com/spore-host/cohort"
	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

func launcher(t *testing.T) (*Launcher, *fakeLaunch) {
	t.Helper()
	f := newFakeLaunch()
	return New(f, "us-east-1", spawnaws.LaunchConfig{AMI: "ami-test"}), f
}

func spec(id string) compute.Spec {
	return compute.Spec{
		EntityID:          id,
		IdempotencyToken:  "tok-" + id,
		Rung:              compute.Rung{Class: "cpu"},
		EstimatedDuration: time.Hour,
		Lifecycle:         compute.Lifecycle{TTL: 2 * time.Hour, IdleTimeout: 20 * time.Minute, CostLimit: 5},
	}
}

// The substrate satisfies the core compute.Launcher seam and launches+observes a
// unit — generalizing mpicohort, offline via the fake.
func TestLauncher_LaunchAndObserve(t *testing.T) {
	l, _ := launcher(t)
	ctx := context.Background()
	obs, err := l.Launch(ctx, spec("job-1"))
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != compute.StateRunning || obs.ProviderID == "" {
		t.Fatalf("launch obs = %+v", obs)
	}
	got, _ := l.Observe(ctx, []string{"job-1"})
	if len(got) != 1 || got[0].State != compute.StateRunning {
		t.Fatalf("observe = %+v", got)
	}
	// An unseen id is StateUnknown (lag), never absent.
	un, _ := l.Observe(ctx, []string{"ghost"})
	if un[0].State != compute.StateUnknown {
		t.Fatalf("unseen should be StateUnknown, got %v", un[0].State)
	}
}

// IDEMPOTENCY: a relaunch with the same token returns the existing instance — no
// duplicate. This is the orphan guard's foundation.
func TestLauncher_RelaunchIsIdempotent(t *testing.T) {
	l, f := launcher(t)
	ctx := context.Background()
	o1, _ := l.Launch(ctx, spec("job-1"))
	o2, _ := l.Launch(ctx, spec("job-1")) // same IdempotencyToken
	if o1.ProviderID != o2.ProviderID {
		t.Fatalf("relaunch created a duplicate: %s vs %s", o1.ProviderID, o2.ProviderID)
	}
	if f.launches != 1 {
		t.Fatalf("actual launches = %d, want 1 (idempotent relaunch)", f.launches)
	}
}

// THE TRAP (#70): a crash leaves an instance running with no ledger record.
// Reconcile lists Telos-tagged instances and returns the orphan (not in `known`)
// so the caller can terminate it. Prove nothing is left billing unaccounted.
func TestLauncher_OrphanDetection(t *testing.T) {
	l, f := launcher(t)
	ctx := context.Background()

	// Telos launches two jobs, then "crashes" — its ledger only ever recorded job-1
	// (job-2's record was lost in the crash, but the instance is real and billing).
	_, _ = l.Launch(ctx, spec("job-1"))
	_, _ = l.Launch(ctx, spec("job-2"))

	known := []string{"job-1"} // the ledger after the crash
	orphans, err := l.Reconcile(ctx, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].EntityID != "job-2" {
		t.Fatalf("expected exactly job-2 as orphan, got %+v", orphans)
	}
	// Terminate the orphan → nothing left billing unaccounted.
	if err := l.Terminate(ctx, orphans[0].EntityID); err != nil {
		t.Fatal(err)
	}
	// After termination, Reconcile (running-only) finds no orphans.
	again, _ := l.Reconcile(ctx, known)
	if len(again) != 0 {
		t.Fatalf("orphan still billing after terminate: %+v", again)
	}
	_ = f
}

// Untagged instances (not ours) are never claimed as orphans.
func TestLauncher_ReconcileIgnoresUntagged(t *testing.T) {
	l, f := launcher(t)
	ctx := context.Background()
	_, _ = l.Launch(ctx, spec("job-1"))
	// A foreign instance with no telos tag.
	f.mu.Lock()
	f.nextID++
	id := "i-foreign"
	f.instances[id] = &spawnaws.InstanceInfo{InstanceID: id, Name: "someone-elses", State: "running", Tags: map[string]string{}}
	f.mu.Unlock()

	orphans, _ := l.Reconcile(ctx, []string{"job-1"})
	if len(orphans) != 0 {
		t.Fatalf("must not claim untagged foreign instances: %+v", orphans)
	}
}

// FaultClass maps EC2 error codes to cohort fault classes (the Classifier half).
func TestFaultClass(t *testing.T) {
	cases := map[string]cohort.FaultClass{
		"InsufficientInstanceCapacity": cohort.FaultCapacityExhausted,
		"MaxSpotInstanceCountExceeded": cohort.FaultCapacityExhausted,
		"Throttling":                   cohort.FaultThrottle,
		"AuthFailure":                  cohort.FaultTerminal,
	}
	for code, want := range cases {
		got := FaultClass(&spawnaws.LaunchError{Code: code})
		if got != want {
			t.Errorf("FaultClass(%q) = %v, want %v", code, got, want)
		}
	}
}
