// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/governor"
)

// overrunLauncher stays Running for the first N observes (accruing compute-seconds
// that exceed the reservation), then goes terminal — exercising the mid-run
// escrow-gap re-authorization (#63).
type overrunLauncher struct {
	mu       sync.Mutex
	observes int
	runFor   int   // stay running this many observes before terminal
	secsEach int64 // compute-seconds added per observe
}

func (l *overrunLauncher) Launch(ctx context.Context, s compute.Spec) (compute.Observation, error) {
	return compute.Observation{EntityID: s.EntityID, State: compute.StateRunning, Disposition: compute.DispositionRunning}, nil
}
func (l *overrunLauncher) Observe(ctx context.Context, ids []string) ([]compute.Observation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.observes++
	secs := int64(l.observes) * l.secsEach
	if l.observes >= l.runFor {
		return []compute.Observation{{EntityID: ids[0], State: compute.StateStopped, Disposition: compute.DispositionComplete, ComputeSeconds: secs}}, nil
	}
	return []compute.Observation{{EntityID: ids[0], State: compute.StateRunning, Disposition: compute.DispositionRunning, ComputeSeconds: secs}}, nil
}
func (l *overrunLauncher) Terminate(ctx context.Context, id string) error { return nil }
func (l *overrunLauncher) Reconcile(ctx context.Context, known []string) ([]compute.Observation, error) {
	return nil, nil
}

// #63 — the escrow gap fires a mid-run change-order: a job that overruns its
// estimate re-reserves the delta against the parent (when the grant can fund it).
func TestRunWork_MidRunReauthorizeWhenGrantFunds(t *testing.T) {
	// Estimate is small (1h × $1 = $1 reserved); the job accrues far more, but the
	// envelope is large enough to fund the re-authorizations.
	l := &overrunLauncher{runFor: 5, secsEach: 3600} // 1h per observe → overruns the $1 estimate
	gov := governor.New(acs.Budget{Amount: 1000, Period: 30 * 24 * time.Hour, Currency: "USD"})
	gw, err := New(Config{
		Backends: map[string]Backend{"echo": NewEchoBackend("echo")},
		Governor: gov, Costs: NewCostModel(CostModelConfig{}),
		Launcher: l, Pricer: fakePricer{hourly: 1.0}, ComputeCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = gw.RunWork(context.Background(), workspec("long-job", time.Hour, oneSource()))
	if err != nil {
		t.Fatalf("a funded overrun should re-authorize and complete, got: %v", err)
	}
	// The grant funded the overrun: reservoir went down (re-authorizations + final
	// settle) but stayed non-negative — no silent unbounded overspend.
	rem := gov.Remaining(governor.RootGrant).Amount
	if rem < 0 || rem >= 1000 {
		t.Fatalf("root remaining = %v; expected a bounded draw in (0,1000)", rem)
	}
}

// #63 — when the grant CANNOT fund the overrun, re-authorization fails closed; the
// run still completes (the on-node CostLimit is the backstop) and settles what was
// spent, without the gateway crashing or silently overspending past the envelope's
// hard floor. (We assert the loop tolerates a failed re-auth and still settles.)
func TestRunWork_MidRunReauthorizeFailsClosedGracefully(t *testing.T) {
	l := &overrunLauncher{runFor: 4, secsEach: 3600}
	// Tiny envelope: the first reservation ($1) fits, but re-authorizations won't.
	gov := governor.New(acs.Budget{Amount: 1.5, Period: 30 * 24 * time.Hour, Currency: "USD"})
	gw, _ := New(Config{
		Backends: map[string]Backend{"echo": NewEchoBackend("echo")},
		Governor: gov, Costs: NewCostModel(CostModelConfig{}),
		Launcher: l, Pricer: fakePricer{hourly: 1.0}, ComputeCurrency: "USD",
	})
	// Should NOT error out — the failed re-auth is tolerated (backstop is on-node);
	// the run reaches terminal and settles.
	res, _, err := gw.RunWork(context.Background(), workspec("starved-job", time.Hour, oneSource()))
	if err != nil {
		t.Fatalf("a failed re-auth must be tolerated (on-node backstop), got: %v", err)
	}
	if res.Disposition != compute.DispositionComplete {
		t.Fatalf("run should still reach terminal, got %v", res.Disposition)
	}
	if gov.Remaining(governor.RootGrant).Amount < 0 {
		t.Fatal("reservoir went negative — fail-closed breached")
	}
}

// #67 — FSx is its OWN ledger line: an ephemeral filesystem's cost is attributed
// separately from the compute meter, not folded into instance cost.
func TestRunWork_FSxIsSeparateLedgerLine(t *testing.T) {
	l := newFakeLauncher(compute.DispositionComplete, 3600) // 1 compute-hour
	gw, _ := computeGateway(t, 1000, l, fakePricer{hourly: 2.0})

	spec := workspec("job-fsx", time.Hour, oneSource())
	spec.Lifecycle.FSx = compute.FSxLifecycle{Mode: "ephemeral"}

	res, computeCost, err := gw.RunWork(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	// FSx is reported as its own line, NOT inside the compute cost.
	if res.FSx == nil {
		t.Fatal("FSx provisioned but no separate ledger line reported")
	}
	if res.FSx.Mode != "ephemeral" || res.FSx.Cost.Amount <= 0 {
		t.Fatalf("FSx line wrong: %+v", res.FSx)
	}
	if !res.FSx.Cost.FullySynthesized() {
		t.Fatal("FSx cost must be synthesized (modeled), distinguishable from billed")
	}
	// The compute meter is the instance cost only (1h × $2 = $2) — FSx is NOT folded in.
	if computeCost.Amount != 2.0 {
		t.Fatalf("compute cost = %v, want 2.0 (FSx must be a SEPARATE line, not folded in)", computeCost.Amount)
	}
}

// A durable filesystem bills for at least its TTL (the rectangle outlives the run).
func TestFSx_DurableBillsForTTL(t *testing.T) {
	line := fsxLineFor(compute.FSxLifecycle{Mode: "durable", TTL: 7 * 24 * time.Hour}, time.Hour, 1200, "USD")
	if line == nil || line.Mode != "durable" {
		t.Fatalf("durable FSx line: %+v", line)
	}
	// Billed for the TTL (much larger than the 1h run) — the perishable rectangle
	// that outlives the compute is visible.
	if line.Billed != 7*24*time.Hour {
		t.Fatalf("durable should bill for TTL, got %v", line.Billed)
	}
	ephemeral := fsxLineFor(compute.FSxLifecycle{Mode: "ephemeral"}, time.Hour, 1200, "USD")
	if ephemeral.Cost.Amount >= line.Cost.Amount {
		t.Fatal("durable (TTL-billed) should cost more than ephemeral (run-billed)")
	}
}
