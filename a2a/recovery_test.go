// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package a2a

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

func env(amount float64) acs.Budget {
	return acs.Budget{Amount: amount, Period: 30 * 24 * time.Hour, Currency: "USD"}
}

// fakeRemote captures the budget envelope it received and returns a settlement.
type fakeRemote struct {
	gotBudget Budget
	settle    Settlement
}

func (f *fakeRemote) Invoke(ctx context.Context, url string, env Request) (Response, error) {
	f.gotBudget = env.Budget
	return Response{Output: "ok", Archetype: "composite", Accepted: true, Settlement: f.settle}, nil
}

// HAPPY PATH (sync): CrossBoundary reserves against the parent (write-ahead
// journaled), sends the budget envelope, and settles inline.
func TestCrossBoundary_SyncHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent.wal")
	gov, _ := governor.Open(path, env(100))
	defer gov.Close()

	remote := &fakeRemote{settle: Settlement{CostSettled: 12, CostSynthesized: 5, Currency: "USD", Outcome: "done", Accepted: true}}
	resp, err := CrossBoundary(context.Background(), gov, governor.RootGrant,
		acs.BudgetRequest{Amount: 30, Period: time.Hour}, "http://session", remote, "q")
	if err != nil {
		t.Fatalf("cross-boundary: %v", err)
	}
	// The remote was bounded by the parent's reservation (grant-rate, amount+period).
	if remote.gotBudget.Amount != 30 || remote.gotBudget.Period != time.Hour {
		t.Fatalf("remote budget envelope = %+v, want amount=30 period=1h", remote.gotBudget)
	}
	// Settlement applied to the parent ledger: 100 - 12 spent = 88; surplus banked.
	if rem := gov.Remaining(governor.RootGrant).Amount; rem != 88 {
		t.Fatalf("parent remaining = %v, want 88 (100-12)", rem)
	}
	if resp.Settlement.CostSynthesized != 5 {
		t.Fatalf("synthesized portion lost across the wire: %+v", resp.Settlement)
	}
}

// RECOVERY PATH (eventual): the parent reserves (escrow journaled), CRASHES before
// the remote returns, is rebuilt from the WAL, and a LATE settlement settles
// eventually against the replayed parent — idempotently. This is #A1 + #B1 +
// crash-point-3 at the wire level.
func TestSettleRemoteEventually_AfterParentReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent.wal")
	ctx := context.Background()

	// Parent reserves a child grant for a remote session — write-ahead journaled.
	gov1, _ := governor.Open(path, env(100))
	grant, err := gov1.Reserve(ctx, governor.RootGrant, acs.BudgetRequest{Amount: 40, Period: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	gid := governor.GrantID(grant.GrantID)
	// CRASH: the remote is off doing work; the parent dies before settlement.
	// (No Close — the WAL on disk is all that survives.)

	// Parent rebuilt from the WAL: the escrow survived (#A1).
	gov2, _ := governor.Open(path, env(100))
	defer gov2.Close()
	if gov2.Remaining(governor.RootGrant).Amount != 60 {
		t.Fatalf("replayed parent remaining = %v, want 60 (escrow survived crash)", gov2.Remaining(governor.RootGrant).Amount)
	}

	// The LATE settlement arrives and settles eventually against the replayed parent.
	late := Settlement{CostSettled: 15, Currency: "USD", Outcome: "negative", Accepted: true}
	if err := SettleRemoteEventually(ctx, gov2, gid, late); err != nil {
		t.Fatalf("eventual settle: %v", err)
	}
	if gov2.Remaining(governor.RootGrant).Amount != 85 {
		t.Fatalf("after eventual settle: remaining = %v, want 85 (100-15)", gov2.Remaining(governor.RootGrant).Amount)
	}
	// An accepted negative banks surplus identically to a positive (40-15=25).
	if gov2.BankedSurplus(gid) != 25 {
		t.Fatalf("accepted-negative surplus = %v, want 25 (direction-neutral)", gov2.BankedSurplus(gid))
	}

	// Idempotent: the same late settlement applied AGAIN is a no-op.
	_ = SettleRemoteEventually(ctx, gov2, gid, late)
	if gov2.Remaining(governor.RootGrant).Amount != 85 || gov2.BankedSurplus(gid) != 25 {
		t.Fatal("re-applied eventual settlement changed the ledger — idempotency broken")
	}
}

// Fails closed across the wire AND survives the discipline on the recovery path:
// a child reservation bigger than the parent envelope is refused before invoking.
func TestCrossBoundary_FailsClosed(t *testing.T) {
	gov := governor.New(env(10))
	called := false
	remote := &fakeRemote{}
	_, err := CrossBoundary(context.Background(), gov, governor.RootGrant,
		acs.BudgetRequest{Amount: 50, Period: time.Hour}, "http://x",
		invokerFunc(func(ctx context.Context, url string, e Request) (Response, error) {
			called = true
			return remote.Invoke(ctx, url, e)
		}), "q")
	if err == nil {
		t.Fatal("over-reservation must fail closed")
	}
	if called {
		t.Fatal("remote invoked despite failed reservation")
	}
}

type invokerFunc func(context.Context, string, Request) (Response, error)

func (f invokerFunc) Invoke(ctx context.Context, url string, e Request) (Response, error) {
	return f(ctx, url, e)
}
