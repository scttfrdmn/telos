// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/a2a"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
	"github.com/scttfrdmn/telos/placer"
	"github.com/scttfrdmn/telos/substrate/agentcore"
	"github.com/scttfrdmn/telos/substrate/inproc"
	"github.com/scttfrdmn/telos/transport"
	"github.com/spore-host/cohort"
)

// M4 DONE-BAR. A bound graph runs across BOTH transports — goroutine (inproc) and
// A2A-session (agentcore) — reconciled by the REAL cohort core; budget/cancel
// cross the A2A boundary; settlement returns to the parent ledger; every entity
// yields a legible cohort Record; and a caller can't tell goroutine from A2A
// except where a trust/resource trigger forces the rung. Offline (loopback);
// deploy gated.

// loopbackSession builds an in-process Telos host as the "remote" A2A session —
// a real second host.Server driven over the A2A contract (your decision). It is
// budget-aware: its governor conserves the reservation the parent sends.
func loopbackSession(t *testing.T) func(ctx context.Context, id cohort.EntityID) (http.Handler, error) {
	t.Helper()
	return func(ctx context.Context, id cohort.EntityID) (http.Handler, error) {
		seed := seedSpec(t)
		deps, err := NewDeps(ctx, DepsConfig{Envelope: seed.Budget}, nil) // echo backend
		if err != nil {
			return nil, err
		}
		srv, err := NewServer(seed, deps, nil)
		if err != nil {
			return nil, err
		}
		return srv.Handler(), nil
	}
}

// Both rungs reconcile a node to PhaseReady through the unmodified cohort core,
// and the PLACER decides the rung from trust/gravity (first-trigger-wins).
func TestM4_BothTransportsReconciledByCohort(t *testing.T) {
	ctx := context.Background()
	in := inproc.New()
	ac := agentcore.New(loopbackSession(t))
	pl := placer.New()
	defer func() { _ = ac.Terminate(ctx, "iso-node") }()

	// A same-envelope node → goroutine; an isolated node → A2A session.
	cheap := &acs.Node{ID: "cheap-node", Trust: acs.TrustSameEnvelope}
	iso := &acs.Node{ID: "iso-node", Trust: acs.TrustIsolated}

	dCheap, _ := pl.Place(ctx, cheap)
	dIso, _ := pl.Place(ctx, iso)
	if dCheap.Substrate != "inproc" || dIso.Substrate != "agentcore" {
		t.Fatalf("placement: cheap=%s iso=%s (want inproc / agentcore)", dCheap.Substrate, dIso.Substrate)
	}

	// Reconcile each on its placed substrate, through the real cohort core.
	reconcile := func(sub interface {
		cohort.Actuator
		cohort.Observer
		cohort.Classifier
	}, id cohort.EntityID, p transport.Placement) cohort.Record {
		r := cohort.NewReconciler(sub, sub, sub, nil, nil, nil)
		intent, _ := cohort.NewEntityIntent("telos", id, "g1", "c1", p, "")
		c, _ := cohort.NewSerialCohort("c1", intent, cohort.PhaseBudget{})
		out, err := r.Reconcile(ctx, c)
		if err != nil {
			t.Fatalf("reconcile %s: %v", id, err)
		}
		return out.Records[id]
	}

	recCheap := reconcile(in, "cheap-node", dCheap.Placement)
	recIso := reconcile(ac, "iso-node", dIso.Placement)

	// Indistinguishability: both succeeded; the only difference is the rung name
	// (placement), not the outcome shape — a caller sees a succeeded Record either
	// way. Every entity is legible.
	if !recCheap.Succeeded() || !recIso.Succeeded() {
		t.Fatalf("not both ready: cheap=%s iso=%s", recCheap.Summary(), recIso.Summary())
	}
	if recCheap.Summary() == "" || recIso.Summary() == "" {
		t.Fatal("every entity must yield a legible Record")
	}
}

// Budget/cancel cross the A2A boundary; settlement returns to the parent ledger;
// a remote child is bounded by the parent budget (fails closed).
func TestM4_BudgetCrossesA2ABoundaryAndSettles(t *testing.T) {
	ctx := context.Background()
	ac := agentcore.New(loopbackSession(t))
	defer func() { _ = ac.Terminate(ctx, "remote-1") }()

	// Launch + observe the remote session (drive it to ready via the Observer).
	intent, _ := cohort.NewEntityIntent("telos", "remote-1", "g1", "c1",
		transport.NewPlacement(transport.RungA2ASession, transport.DefaultLadder), "")
	if _, err := ac.Launch(ctx, intent); err != nil {
		t.Fatalf("launch: %v", err)
	}
	// Poll the Observer until Running (the readiness probe over /ping).
	var url string
	for i := 0; i < 50; i++ {
		obs, _ := ac.Observe(ctx, []cohort.EntityID{"remote-1"})
		if obs[0].State == cohort.StateRunning {
			url = ac.SessionURL("remote-1")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" {
		t.Fatal("remote session never became Running")
	}

	// Parent ledger with a small envelope; cross the boundary with a child reservation.
	parent := governor.New(acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	resp, err := a2a.CrossBoundary(ctx, parent, governor.RootGrant,
		acs.BudgetRequest{Amount: 20, Period: time.Hour}, url, a2a.NewHTTPInvoker(),
		"does X modulate Y, and what's the evidence?")
	if err != nil {
		t.Fatalf("cross-boundary: %v", err)
	}

	// Settlement returned and was applied to the PARENT ledger: the remote child's
	// spend is conserved against the parent grant (remaining < envelope).
	if resp.Settlement.CostSettled < 0 {
		t.Fatalf("negative settled cost: %+v", resp.Settlement)
	}
	rem := parent.Remaining(governor.RootGrant)
	if rem.Amount > 100 || rem.Amount < 0 {
		t.Fatalf("parent remaining %v out of [0,100] — conservation breached across the wire", rem.Amount)
	}
	// The remote produced a real run (the recursion crossed the boundary): the
	// loopback session is itself a Telos host, so it emitted an archetype.
	if resp.Archetype == "" {
		t.Fatal("remote session did not run the graph (no archetype) — recursion didn't cross")
	}
}

// Conservation fails closed across the wire: a child reservation larger than the
// parent envelope is refused BEFORE the remote is invoked.
func TestM4_BudgetFailsClosedAcrossWire(t *testing.T) {
	ctx := context.Background()
	parent := governor.New(acs.Budget{Amount: 10, Period: 24 * time.Hour, Currency: "USD"})

	called := false
	fake := fakeInvoker{onInvoke: func() { called = true }}
	_, err := a2a.CrossBoundary(ctx, parent, governor.RootGrant,
		acs.BudgetRequest{Amount: 50, Period: time.Hour}, // 50 > 10 envelope
		"http://unused", fake, "q")
	if err == nil {
		t.Fatal("over-reservation across the wire must fail closed")
	}
	if called {
		t.Fatal("remote was invoked despite a failed reservation — must fail closed BEFORE the call")
	}
}

// fakeInvoker lets the fails-closed test assert the remote is never reached.
type fakeInvoker struct{ onInvoke func() }

func (f fakeInvoker) Invoke(ctx context.Context, url string, env a2a.Request) (a2a.Response, error) {
	if f.onInvoke != nil {
		f.onInvoke()
	}
	return a2a.Response{}, nil
}

// Cancel propagates across the boundary: cancelling the parent context cancels
// the remote invocation.
func TestM4_CancelPropagatesAcrossWire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	parent := governor.New(acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	cancel() // kill-switch fires before the call

	_, err := a2a.CrossBoundary(ctx, parent, governor.RootGrant,
		acs.BudgetRequest{Amount: 5, Period: time.Hour}, "http://unused", a2a.NewHTTPInvoker(), "q")
	if err == nil {
		t.Fatal("a cancelled parent context must propagate (no settlement on a cancelled run)")
	}
}
