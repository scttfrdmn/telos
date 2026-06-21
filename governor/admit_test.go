// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

func quote(amount float64) Quote {
	return Quote{Estimate: acs.Cost{Amount: amount, Currency: "USD"}, Over: time.Hour}
}

// The rate decision: the SAME remaining amount admits differently depending on
// remaining TIME. This is the whole point of grant-rate admission (§9).
func TestAdmit_RateAwareNotTotalAware(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	res := Reservoir{Remaining: 100, Currency: "USD"}

	// A draw of 60 (a 60% slice) — early in the grant (90% clock left) it is
	// admitted but flagged as a disproportionate slice; we mainly check the
	// rate axis below. Use a draw whose admit decision flips on the CLOCK.
	q := quote(60)

	early := Clock{Elapsed: 1 * time.Hour, Total: 100 * time.Hour}  // lots of time left
	late := Clock{Elapsed: 100 * time.Hour, Total: 100 * time.Hour} // no time left

	aEarly, err := g.Admit(ctx, q, res, early)
	if err != nil {
		t.Fatal(err)
	}
	aLate, err := g.Admit(ctx, q, res, late)
	if err != nil {
		t.Fatal(err)
	}
	if !aEarly.Admitted {
		t.Fatal("a 60-of-100 draw with nearly the whole clock left should be admitted")
	}
	if aLate.Admitted {
		t.Fatal("same amount, same reservoir, but clock exhausted → must NOT admit (rate-aware, not total-aware)")
	}
}

func TestAdmit_ExceedsReservoirFailsClosed(t *testing.T) {
	g := New(env(100))
	a, err := g.Admit(context.Background(), quote(150), Reservoir{Remaining: 100, Currency: "USD"},
		Clock{Elapsed: 0, Total: 100 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if a.Admitted {
		t.Fatal("a draw larger than the reservoir must fail closed")
	}
}

func TestAdmit_DisproportionateSliceEscalates(t *testing.T) {
	g := New(env(100))
	// 80 of 100 remaining, plenty of clock: affordable but a disproportionate
	// slice → admitted with the escalate flag (policy of who/how is later).
	a, err := g.Admit(context.Background(), quote(80), Reservoir{Remaining: 100, Currency: "USD"},
		Clock{Elapsed: 1 * time.Hour, Total: 100 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Admitted {
		t.Fatal("80 of 100 with clock left is affordable; should admit")
	}
	if !a.Escalate {
		t.Fatal("consuming a disproportionate slice should flag escalate")
	}
}

// Surplus banks ONLY on acceptance; an unaccepted (even high-surplus) outcome
// banks zero.
func TestSettle_SurplusBanksOnlyOnAcceptance(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()

	// Accepted: reserve 40, spend 10 → 30 surplus banked.
	ga, _ := g.Reserve(ctx, RootGrant, req(40, time.Hour))
	if err := g.Settle(ctx, GrantID(ga.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if got := g.BankedSurplus(GrantID(ga.GrantID)); got != 30 {
		t.Fatalf("accepted surplus banked = %v, want 30", got)
	}

	// Unaccepted: reserve 40, spend 10 → 30 nominal surplus, but banks ZERO.
	gb, _ := g.Reserve(ctx, RootGrant, req(40, time.Hour))
	if err := g.Settle(ctx, GrantID(gb.GrantID), cost(10), Outcome{Exit: ExitDone, Accepted: false}); err != nil {
		t.Fatal(err)
	}
	if got := g.BankedSurplus(GrantID(gb.GrantID)); got != 0 {
		t.Fatalf("unaccepted surplus banked = %v, want 0 (abandonment, not thrift)", got)
	}
}

func TestSettle_TracksSynthesizedPortion(t *testing.T) {
	g := New(env(100))
	ctx := context.Background()
	gr, _ := g.Reserve(ctx, RootGrant, req(40, time.Hour))
	// Actual $10, of which $4 is modeled (local) cost (issue #23).
	actual := acs.Cost{Amount: 10, Synthesized: 4, Currency: "USD"}
	if err := g.Settle(ctx, GrantID(gr.GrantID), actual, Outcome{Exit: ExitDone, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	total, synth := g.Spent(GrantID(gr.GrantID))
	if total != 10 || synth != 4 {
		t.Fatalf("spent split = (%v,%v), want (10,4) — modeled portion must stay distinguishable", total, synth)
	}
}
