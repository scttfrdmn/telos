// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

func env(amount float64) acs.Budget {
	return acs.Budget{Amount: amount, Period: 30 * 24 * time.Hour, Currency: "USD"}
}

func req(amount float64, period time.Duration) acs.BudgetRequest {
	return acs.BudgetRequest{Amount: amount, Period: period}
}

func cost(amount float64) acs.Cost { return acs.Cost{Amount: amount, Currency: "USD"} }

func TestReserve_WithinEnvelope(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	grant, err := g.Reserve(ctx, RootGrant, req(40, 24*time.Hour))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if grant.Budget.Amount != 40 {
		t.Fatalf("granted %.2f, want 40", grant.Budget.Amount)
	}
	if rem := g.Remaining(RootGrant); rem.Amount != 60 {
		t.Fatalf("root remaining %.2f, want 60", rem.Amount)
	}
	// Remaining is reported as a rate (carries the period), never a bare total.
	if rem := g.Remaining(RootGrant); rem.Period <= 0 {
		t.Fatal("remaining must carry a period (invariant 4)")
	}
}

func TestReserve_FailsClosedOnBreach(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	if _, err := g.Reserve(ctx, RootGrant, req(70, 24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Second reservation would push Σ children to 140 > 100.
	_, err := g.Reserve(ctx, RootGrant, req(70, 24*time.Hour))
	if err == nil {
		t.Fatal("expected conservation breach to fail closed")
	}
	if !errors.Is(err, ErrConservation) {
		t.Fatalf("expected ErrConservation, got: %v", err)
	}
	// Fails closed: nothing reserved, so 30 still available.
	if rem := g.Remaining(RootGrant); rem.Amount != 30 {
		t.Fatalf("after failed reserve, remaining %.2f, want 30 (nothing reserved)", rem.Amount)
	}
}

func TestReserve_PeriodConservation(t *testing.T) {
	g := New(env(100)) // 30-day envelope
	ctx := context.Background()
	// A child cannot spend over a longer horizon than its parent.
	_, err := g.Reserve(ctx, RootGrant, req(10, 60*24*time.Hour))
	if err == nil {
		t.Fatal("expected rejection: child period exceeds parent period")
	}
}

func TestSettle_DebitsActualReleasesEscrow(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	grant, _ := g.Reserve(ctx, RootGrant, req(40, 24*time.Hour))
	// While escrowed, root has 60 available (40 locked).
	if rem := g.Remaining(RootGrant); rem.Amount != 60 {
		t.Fatalf("escrowed: root remaining %.2f, want 60", rem.Amount)
	}
	// Actual came in under the reservation: only 25 spent.
	if err := g.Settle(ctx, GrantID(grant.GrantID), cost(25), Outcome{Exit: ExitDone}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	// Escrow released, actual debited: root should now have 75 (100 - 25).
	if rem := g.Remaining(RootGrant); rem.Amount != 75 {
		t.Fatalf("after settle: root remaining %.2f, want 75", rem.Amount)
	}
}

func TestRelease_ReturnsFullEscrow(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	grant, _ := g.Reserve(ctx, RootGrant, req(40, 24*time.Hour))
	if err := g.Release(ctx, GrantID(grant.GrantID)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if rem := g.Remaining(RootGrant); rem.Amount != 100 {
		t.Fatalf("after release: root remaining %.2f, want 100 (no charge)", rem.Amount)
	}
}

func TestSettle_OverspendRecordedHonestly(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	grant, _ := g.Reserve(ctx, RootGrant, req(40, 24*time.Hour))
	// Actual exceeded the reservation (estimate was wrong). M1 records it
	// honestly rather than clamping — the admission policy that prevents this is
	// M2. Root: 100 - 55 = 45.
	if err := g.Settle(ctx, GrantID(grant.GrantID), cost(55), Outcome{Exit: ExitDone}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if rem := g.Remaining(RootGrant); rem.Amount != 45 {
		t.Fatalf("after overspend settle: root remaining %.2f, want 45", rem.Amount)
	}
}

func TestNestedConservation(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	// Reserve a 60 sub-grant, then reserve children against IT.
	parent, _ := g.Reserve(ctx, RootGrant, req(60, 24*time.Hour))
	pid := GrantID(parent.GrantID)
	if _, err := g.Reserve(ctx, pid, req(50, 12*time.Hour)); err != nil {
		t.Fatalf("child reserve: %v", err)
	}
	// Σ children (50) ≤ parent (60): a 20 child now breaches.
	if _, err := g.Reserve(ctx, pid, req(20, 12*time.Hour)); !errors.Is(err, ErrConservation) {
		t.Fatalf("expected nested conservation breach, got: %v", err)
	}
	// A 10 child fits.
	if _, err := g.Reserve(ctx, pid, req(10, 12*time.Hour)); err != nil {
		t.Fatalf("10 should fit in remaining 10: %v", err)
	}
}

func TestSettle_CurrencyMismatchRejected(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	grant, _ := g.Reserve(ctx, RootGrant, req(40, 24*time.Hour))
	err := g.Settle(ctx, GrantID(grant.GrantID), acs.Cost{Amount: 10, Currency: "EUR"}, Outcome{})
	if err == nil {
		t.Fatal("expected currency-mismatch rejection")
	}
}

func TestConcurrentReserve_ConservationHolds(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	// 200 goroutines each try to reserve 1.0; only 100 can succeed (envelope=100).
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Reserve(ctx, RootGrant, req(1, time.Hour)); err == nil {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 100 {
		t.Fatalf("concurrent reserve granted %d, want exactly 100 (conservation must hold under races)", granted)
	}
	if rem := g.Remaining(RootGrant); rem.Amount != 0 {
		t.Fatalf("root remaining %.2f, want 0", rem.Amount)
	}
}

func TestCannotSettleOrReleaseRoot(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	if err := g.Settle(ctx, RootGrant, cost(1), Outcome{}); err == nil {
		t.Fatal("settling root must fail")
	}
	if err := g.Release(ctx, RootGrant); err == nil {
		t.Fatal("releasing root must fail")
	}
}
