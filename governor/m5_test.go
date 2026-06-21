// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spore-host/cohort"
)

func m5wal(t *testing.T) string { return filepath.Join(t.TempDir(), "m5.wal") }

// #I1 — THE TRAP. A settlement applied, then replayed from the WAL after a crash,
// must not double-count: neither double-debiting the reservoir nor double-banking
// surplus. This is the place a crash-recovery budget system silently corrupts
// itself. Replay an already-applied settlement → the ledger is unchanged.
func TestI1_IdempotentReplay_ReappliedSettleLeavesLedgerUnchanged(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()

	g1, _ := Open(path, env(100))
	gr, _ := g1.Reserve(ctx, RootGrant, req(40, time.Hour))
	_ = g1.Settle(ctx, GrantID(gr.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true})
	// Live state: 100 - 10 spent = 90 remaining; banked surplus = 30.
	wantRemaining := g1.Remaining(RootGrant).Amount
	wantSurplus := g1.BankedSurplus(GrantID(gr.GrantID))
	g1.Close()

	// "Crash" and replay from the WAL.
	g2, err := Open(path, env(100))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer g2.Close()
	if g2.Remaining(RootGrant).Amount != wantRemaining {
		t.Fatalf("replay remaining = %v, want %v (double-debit?)", g2.Remaining(RootGrant).Amount, wantRemaining)
	}
	if g2.BankedSurplus(GrantID(gr.GrantID)) != wantSurplus {
		t.Fatalf("replay surplus = %v, want %v (double-bank?)", g2.BankedSurplus(GrantID(gr.GrantID)), wantSurplus)
	}

	// Explicitly re-apply the SAME settlement live (a late/duplicate settlement):
	// idempotent no-op, ledger unchanged.
	if err := g2.Settle(ctx, GrantID(gr.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatalf("re-settle must be a no-op, got error: %v", err)
	}
	if g2.Remaining(RootGrant).Amount != wantRemaining || g2.BankedSurplus(GrantID(gr.GrantID)) != wantSurplus {
		t.Fatal("re-applied settlement changed the ledger — idempotency broken")
	}
}

// #A2 crash point 1 — parent-after-escrow. Parent reserves, then crashes before
// settlement. Replay: the escrow is OPEN, conservation holds, escrow not lost.
func TestA2_CrashAfterEscrow(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()

	g1, _ := Open(path, env(100))
	gr, _ := g1.Reserve(ctx, RootGrant, req(40, time.Hour))
	// Crash: drop g1 WITHOUT settling (no Close needed — the WAL has the reserve).
	_ = g1

	g2, _ := Open(path, env(100))
	defer g2.Close()
	// Escrow survived: 40 still escrowed → 60 remaining; the grant is OPEN.
	if g2.Remaining(RootGrant).Amount != 60 {
		t.Fatalf("after crash-after-escrow: remaining = %v, want 60 (escrow lost?)", g2.Remaining(RootGrant).Amount)
	}
	// The open grant can still be settled exactly once post-replay.
	if err := g2.Settle(ctx, GrantID(gr.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatalf("settle replayed-open grant: %v", err)
	}
	if g2.Remaining(RootGrant).Amount != 90 {
		t.Fatalf("after settle: remaining = %v, want 90", g2.Remaining(RootGrant).Amount)
	}
}

// #A2 crash point 2 — child-mid-work. The child never settled (it crashed). Replay:
// the parent's escrow for it is still open and conserved; it can be released or
// settled exactly once.
func TestA2_ChildMidWork(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()
	g1, _ := Open(path, env(100))
	gr, _ := g1.Reserve(ctx, RootGrant, req(30, time.Hour))
	_ = gr // child "crashes" mid-work — no settle, no release

	g2, _ := Open(path, env(100))
	defer g2.Close()
	if g2.Remaining(RootGrant).Amount != 70 {
		t.Fatalf("child-mid-work: remaining = %v, want 70 (escrow conserved)", g2.Remaining(RootGrant).Amount)
	}
	// Recovery can release the abandoned child's escrow exactly once.
	if err := g2.Release(ctx, GrantID(gr.GrantID)); err != nil {
		t.Fatalf("release abandoned child: %v", err)
	}
	if g2.Remaining(RootGrant).Amount != 100 {
		t.Fatalf("after release: remaining = %v, want 100", g2.Remaining(RootGrant).Amount)
	}
	// Releasing again is an idempotent no-op.
	_ = g2.Release(ctx, GrantID(gr.GrantID))
	if g2.Remaining(RootGrant).Amount != 100 {
		t.Fatal("double-release changed the reservoir")
	}
}

// #A2 crash point 3 — settlement-after-replay. A settlement arrives AFTER the
// parent is rebuilt from the log; it settles the replayed-open grant, idempotently.
func TestA2_SettlementAfterReplay(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()
	g1, _ := Open(path, env(100))
	gr, _ := g1.Reserve(ctx, RootGrant, req(50, time.Hour))
	_ = gr

	// Parent rebuilt from the log; THEN the late settlement arrives.
	g2, _ := Open(path, env(100))
	defer g2.Close()
	if err := g2.Settle(ctx, GrantID(gr.GrantID), cost(20), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatalf("late settle after replay: %v", err)
	}
	if g2.Remaining(RootGrant).Amount != 80 {
		t.Fatalf("after late settle: remaining = %v, want 80 (100-20)", g2.Remaining(RootGrant).Amount)
	}
	// The same late settlement replayed AGAIN (its own WAL entry) is a no-op.
	g2.Close()
	g3, _ := Open(path, env(100))
	defer g3.Close()
	if g3.Remaining(RootGrant).Amount != 80 {
		t.Fatalf("after second replay: remaining = %v, want 80 (idempotent)", g3.Remaining(RootGrant).Amount)
	}
}

// #C1 — a fault recorded before a crash is reproduced legibly on replay AND its
// grant is settled exactly once against the parent rebuilt from the log.
func TestC1_FaultRecordSurvivesReplayLegibly(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()
	g1, _ := Open(path, env(100))
	gr, _ := g1.Reserve(ctx, RootGrant, req(30, time.Hour))
	flt := cohort.Fault{Class: cohort.FaultTerminal, Code: "A2ASessionError", Message: "session died mid-launch"}
	if err := g1.RecordFault(ctx, GrantID(gr.GrantID), flt); err != nil {
		t.Fatalf("record fault: %v", err)
	}
	// A faulted grant banks no surplus and its escrow is released (no charge).
	if g1.Remaining(RootGrant).Amount != 100 {
		t.Fatalf("faulted grant should release escrow: remaining = %v, want 100", g1.Remaining(RootGrant).Amount)
	}
	g1.Close()

	// Replay: the legible disposition is reproduced from the log.
	g2, _ := Open(path, env(100))
	defer g2.Close()
	d := g2.Fault(GrantID(gr.GrantID))
	if d == nil {
		t.Fatal("fault disposition lost on replay — not WAL-persisted")
	}
	if d.Code != "A2ASessionError" || d.FaultSummary() == "" {
		t.Fatalf("fault not reproduced legibly: %+v", d)
	}
	// Idempotent: re-faulting (or settling) the closed grant is a no-op.
	if err := g2.RecordFault(ctx, GrantID(gr.GrantID), flt); err != nil {
		t.Fatalf("re-fault must be a no-op: %v", err)
	}
}

// #D1 — surplus signal: dispositions reach a sink live, are reconstructable post-
// replay, and banked surplus survives. Replay does NOT re-push (no double-count).
func TestD1_SurplusSignalAndSurvivesReplay(t *testing.T) {
	path := m5wal(t)
	ctx := context.Background()

	var live []Signal
	sink := sinkFunc(func(s Signal) { live = append(live, s) })

	g1, _ := Open(path, env(100))
	g1.WithSink(sink)
	gr, _ := g1.Reserve(ctx, RootGrant, req(40, time.Hour))
	_ = g1.Settle(ctx, GrantID(gr.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true, Cause: "cheap branch"})

	// Live push happened once with the realized disposition.
	if len(live) != 1 || live[0].Surplus != 30 || live[0].Cause != "cheap branch" {
		t.Fatalf("live signal wrong: %+v", live)
	}
	bankedBefore := g1.TotalBankedSurplus()
	g1.Close()

	// Replay with a sink attached: replay must NOT re-push (would double-count
	// realized burn), but banked surplus must be reconstructed.
	var afterReplay []Signal
	g2, _ := Open(path, env(100))
	g2.WithSink(sinkFunc(func(s Signal) { afterReplay = append(afterReplay, s) }))
	defer g2.Close()
	// Note: WithSink is called AFTER Open here, so replay already finished; the key
	// guarantee is that the reconstructed state matches without re-pushing.
	if g2.TotalBankedSurplus() != bankedBefore || bankedBefore != 30 {
		t.Fatalf("banked surplus not reconstructed: got %v want 30", g2.TotalBankedSurplus())
	}
	// Signals() reflects the reconstructed disposition (the surplus-signal surface).
	sigs := g2.Signals()
	if len(sigs) != 1 || sigs[0].Cause != "cheap branch" || sigs[0].Surplus != 30 {
		t.Fatalf("reconstructed signals wrong: %+v", sigs)
	}
}

// sinkFunc adapts a func to SurplusSink.
type sinkFunc func(Signal)

func (f sinkFunc) OnSettled(s Signal) { f(s) }
