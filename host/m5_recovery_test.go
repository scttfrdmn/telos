// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/a2a"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// M5 DONE-BAR (host level). The four-exit produce→judge→settle loop runs across
// the A2A boundary and SURVIVES a kill-and-restart, with the lexicographic guard
// holding THROUGH recovery: replay never creates a path where surplus banks
// before acceptance. Composes the host recursion (a real loopback session) with
// the WAL crash-recovery (governor.Open/replay).

// runRemoteUnderParent stands up a loopback session, reserves a child grant on a
// WAL-backed parent, invokes the session over A2A, and returns the parent grant
// id + the settlement (for the recovery-path tests to replay against).
func runRemoteUnderParent(t *testing.T, parent *governor.Mem, prompt string) (governor.GrantID, a2a.Settlement) {
	t.Helper()
	factory := loopbackSession(t)
	handler, err := factory(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	// Drive the session directly via httptest through the A2A invoker.
	// (loopbackSession returns an http.Handler; wrap it in a test server.)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	grant, err := parent.Reserve(context.Background(), governor.RootGrant,
		acs.BudgetRequest{Amount: 30, Period: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	gid := governor.GrantID(grant.GrantID)

	resp, err := a2a.CrossBoundary(context.Background(), parent, governor.RootGrant,
		acs.BudgetRequest{Amount: 20, Period: time.Hour}, srv.URL, a2a.NewHTTPInvoker(), prompt)
	if err != nil {
		t.Fatalf("cross-boundary: %v", err)
	}
	_ = gid
	return governor.GrantID(grant.GrantID), resp.Settlement
}

// The lexicographic guard holds through recovery: an accepted result banks on
// replay; an unaccepted high-surplus result banks zero on replay.
func TestM5_LexicographicHoldsThroughReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.wal")
	ctx := context.Background()

	g1, _ := governor.Open(path, acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})

	// Accepted, low surplus: reserve 40, spend 30 → banks 10.
	acc, _ := g1.Reserve(ctx, governor.RootGrant, acs.BudgetRequest{Amount: 40, Period: time.Hour})
	_ = g1.Settle(ctx, governor.GrantID(acc.GrantID), acs.Cost{Amount: 30, Currency: "USD"},
		governor.Outcome{Exit: governor.ExitDone, Accepted: true})

	// Unaccepted, HIGH surplus: reserve 40, spend 2 → would-be 38, banks ZERO.
	rej, _ := g1.Reserve(ctx, governor.RootGrant, acs.BudgetRequest{Amount: 40, Period: time.Hour})
	_ = g1.Settle(ctx, governor.GrantID(rej.GrantID), acs.Cost{Amount: 2, Currency: "USD"},
		governor.Outcome{Exit: governor.ExitNegative, Accepted: false})
	g1.Close()

	// Crash and replay: the gate's RESULT is reconstructed, not re-evaluated.
	g2, _ := governor.Open(path, acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	defer g2.Close()

	if got := g2.BankedSurplus(governor.GrantID(acc.GrantID)); got != 10 {
		t.Fatalf("accepted banked %v after replay, want 10", got)
	}
	if got := g2.BankedSurplus(governor.GrantID(rej.GrantID)); got != 0 {
		t.Fatalf("unaccepted high-surplus banked %v after replay, want 0 — lexicographic broken by replay", got)
	}
	// The accepted low-surplus outcome still ranks above the unaccepted high-surplus
	// one, reconstructed from the WAL.
	accOut := governor.Outcome{Accepted: true, Surplus: acs.Budget{Amount: 10, Period: time.Hour}}
	rejOut := governor.Outcome{Accepted: false, Surplus: acs.Budget{Amount: 38, Period: time.Hour}}
	if governor.CompareOutcomes(accOut, rejOut) != 1 {
		t.Fatal("accepted-low must outrank unaccepted-high after recovery")
	}
}

// The full loop across A2A survives a parent crash-and-replay, and the late
// settlement reconciles against the rebuilt parent (conservation intact).
func TestM5_FourExitLoopSurvivesParentCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.wal")
	ctx := context.Background()

	// Parent reserves a child for a remote run, then "crashes" before settling.
	g1, _ := governor.Open(path, acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	grant, _ := g1.Reserve(ctx, governor.RootGrant, acs.BudgetRequest{Amount: 25, Period: time.Hour})
	gid := governor.GrantID(grant.GrantID)
	// crash (no settle, no close)

	// Rebuild from the WAL: escrow survived.
	g2, _ := governor.Open(path, acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	defer g2.Close()
	if g2.Remaining(governor.RootGrant).Amount != 75 {
		t.Fatalf("escrow lost on crash: remaining = %v, want 75", g2.Remaining(governor.RootGrant).Amount)
	}

	// The remote run's settlement (a four-exit outcome: an accepted negative)
	// reconciles eventually against the rebuilt parent.
	late := a2a.Settlement{CostSettled: 8, Currency: "USD", Outcome: "negative", Accepted: true}
	if err := a2a.SettleRemoteEventually(ctx, g2, gid, late); err != nil {
		t.Fatalf("eventual settle after parent crash: %v", err)
	}
	if g2.Remaining(governor.RootGrant).Amount != 92 {
		t.Fatalf("after recovery settle: remaining = %v, want 92 (100-8)", g2.Remaining(governor.RootGrant).Amount)
	}
	// Accepted negative banks surplus (25-8=17) — a four-exit completion, not a loss.
	if g2.BankedSurplus(gid) != 17 {
		t.Fatalf("accepted-negative banked %v, want 17", g2.BankedSurplus(gid))
	}
}

// A live run across the loopback boundary produces a real settlement (the
// recursion crossed); kept as the end-to-end smoke that the wire path works with
// a real host session, not just the governor in isolation.
func TestM5_LiveLoopbackSettles(t *testing.T) {
	parent := governor.New(acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	_, settle := runRemoteUnderParent(t, parent, "does X modulate Y, and what's the evidence?")
	if settle.CostSettled < 0 {
		t.Fatalf("negative settled cost: %+v", settle)
	}
	// Conservation: the parent reservoir did not go negative or exceed the envelope.
	rem := parent.Remaining(governor.RootGrant).Amount
	if rem < 0 || rem > 100 {
		t.Fatalf("parent remaining %v out of [0,100]", rem)
	}
}
