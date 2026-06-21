// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

func walPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ledger.wal")
}

// THE MILESTONE GATE: WAL survives a kill-and-restart. Reserve+settle some
// grants, simulate a crash (reopen the file WITHOUT a clean Close), and the
// replayed ledger shows exactly the conserved state — no double-spend, no lost
// settlement, surplus banking preserved.
func TestWAL_SurvivesKillAndRestart(t *testing.T) {
	path := walPath(t)
	ctx := context.Background()

	// --- "process 1" ---
	g1, err := Open(path, env(100))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ga, _ := g1.Reserve(ctx, RootGrant, req(40, time.Hour))
	gb, _ := g1.Reserve(ctx, RootGrant, req(30, time.Hour))
	// Settle one accepted (banks surplus), leave the other escrowed (in flight).
	if err := g1.Settle(ctx, GrantID(ga.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	// Simulate a CRASH: do NOT call Close (no graceful flush beyond per-record
	// fsync). The journal on disk is the only thing that survives.
	preRemaining := g1.Remaining(RootGrant).Amount
	preSurplus := g1.BankedSurplus(GrantID(ga.GrantID))

	// --- "process 2": restart from the same WAL ---
	g2, err := Open(path, env(999)) // envelope arg ignored; replayed envelope wins
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer g2.Close()

	// Root reservoir: 100 − 10 spent (settled ga) − 30 escrowed (gb still open) = 60.
	if got := g2.Remaining(RootGrant).Amount; got != preRemaining {
		t.Fatalf("replayed root remaining = %v, want %v (state must survive crash)", got, preRemaining)
	}
	if got := g2.Remaining(RootGrant).Amount; got != 60 {
		t.Fatalf("replayed root remaining = %v, want 60 (100 − 10 settled − 30 escrowed)", got)
	}
	// Banked surplus preserved across restart.
	if got := g2.BankedSurplus(GrantID(ga.GrantID)); got != preSurplus || got != 30 {
		t.Fatalf("replayed banked surplus = %v, want 30", got)
	}
	// The still-open grant gb is replayed as open: settling it must succeed once
	// (no double-settle of ga, which is closed).
	if err := g2.Settle(ctx, GrantID(gb.GrantID), cost(5), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatalf("settle replayed-open grant: %v", err)
	}
	// ga is closed: re-settling must fail (no double-spend).
	if err := g2.Settle(ctx, GrantID(ga.GrantID), cost(1), Outcome{Accepted: true}); err == nil {
		t.Fatal("re-settling an already-settled grant must fail (no double-spend after replay)")
	}
}

// A torn final record (partial line from a crash mid-append) is tolerated: the
// records before it are conserved.
func TestWAL_TornFinalRecordTolerated(t *testing.T) {
	path := walPath(t)
	ctx := context.Background()

	g1, err := Open(path, env(100))
	if err != nil {
		t.Fatal(err)
	}
	gr, _ := g1.Reserve(ctx, RootGrant, req(40, time.Hour))
	_ = g1.Settle(ctx, GrantID(gr.GrantID), cost(10), Outcome{Accepted: true})
	g1.Close()

	// Append a torn (incomplete) JSON line, as a crash mid-write would leave.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"op":"reserve","id":"g99","parent":"","amoun`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Reopen: the torn record is dropped; prior state is intact.
	g2, err := Open(path, env(100))
	if err != nil {
		t.Fatalf("reopen with torn tail: %v", err)
	}
	defer g2.Close()
	if got := g2.Remaining(RootGrant).Amount; got != 90 {
		t.Fatalf("remaining after torn-tail replay = %v, want 90 (100 − 10 settled)", got)
	}
}

// Conservation still fails closed when reservations are replayed from the WAL.
func TestWAL_ConservationHoldsAfterReplay(t *testing.T) {
	path := walPath(t)
	ctx := context.Background()
	g1, _ := Open(path, env(100))
	g1.Reserve(ctx, RootGrant, req(70, time.Hour))
	g1.Close()

	g2, _ := Open(path, env(100))
	defer g2.Close()
	// 70 already escrowed; a 40 reservation must fail closed (would breach 100).
	if _, err := g2.Reserve(ctx, RootGrant, req(40, time.Hour)); err == nil {
		t.Fatal("conservation must hold after replay: 70 escrowed + 40 > 100 should fail closed")
	}
	// A 30 fits exactly.
	if _, err := g2.Reserve(ctx, RootGrant, req(30, time.Hour)); err != nil {
		t.Fatalf("30 should fit in remaining 30 after replay: %v", err)
	}
}

var _ = acs.Budget{}
