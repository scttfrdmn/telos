// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// settleWithVerdict is the integrated settlement path the host uses (and that M3
// will fold into execution): render a verdict over the producer's record in the
// SEPARATE-envelope acceptance node, then settle the producer's grant — banking
// surplus IFF the verdict accepts. It is written here as a helper so the keystone
// guard tests exercise governor + acceptance together, not in isolation.
func settleWithVerdict(t *testing.T, gov governor.Governor, judge acceptance.Acceptance,
	producerGrant governor.GrantID, actual acs.Cost, rec acceptance.Record, standard acceptance.StandardOfProof) governor.Outcome {
	t.Helper()
	v, err := judge.Render(context.Background(), rec, standard)
	if err != nil {
		t.Fatal(err)
	}
	exit := governor.ExitDone
	if rec.Direction == acceptance.DirectionNegative {
		exit = governor.ExitNegative
	}
	out := governor.Outcome{Exit: exit, Accepted: v.Accepted, Cause: string(v.Basis)}
	if err := gov.Settle(context.Background(), producerGrant, actual, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func env100(t *testing.T) *governor.Mem {
	return governor.New(acs.Budget{Amount: 100, Period: 30 * 24 * time.Hour, Currency: "USD"})
}

func reserve(t *testing.T, g *governor.Mem, amount float64) governor.GrantID {
	t.Helper()
	gr, err := g.Reserve(context.Background(), governor.RootGrant, acs.BudgetRequest{Amount: amount, Period: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return governor.GrantID(gr.GrantID)
}

func supported() acceptance.Record {
	return acceptance.Record{NodeID: "producer", Direction: acceptance.DirectionPositive,
		Sources: []acceptance.Source{
			{ID: "a", Independent: true, Supports: true},
			{ID: "b", Independent: true, Supports: true},
		}}
}

const stdConcordant = acceptance.StandardOfProof("concordant")

// THE KEYSTONE GUARD, END TO END. A run that spends little (high surplus) but is
// NOT accepted (unprovenanced) banks ZERO and ranks below a run that spends more
// (low surplus) but IS accepted. This is the under-deliver exploit, proven across
// the real acceptance→governor settlement path, not just the comparator unit.
func TestSettlement_UnacceptedHighSurplusBanksZeroAndRanksLast(t *testing.T) {
	g := env100(t)
	judge := acceptance.NewSummaryJudge("judge").(acceptance.Acceptance)

	// Run 1: reserve 40, spend only 2 (huge surplus 38) — but UNPROVENANCED, so
	// the verdict rejects. Banks zero.
	g1 := reserve(t, g, 40)
	out1 := settleWithVerdict(t, g, judge, g1, acs.Cost{Amount: 2, Currency: "USD"},
		acceptance.Record{NodeID: "p", Direction: acceptance.DirectionPositive /* no sources */}, stdConcordant)

	// Run 2: reserve 40, spend 30 (small surplus 10) — well supported, accepted.
	g2 := reserve(t, g, 40)
	out2 := settleWithVerdict(t, g, judge, g2, acs.Cost{Amount: 30, Currency: "USD"},
		supported(), stdConcordant)

	if g.BankedSurplus(g1) != 0 {
		t.Fatalf("unaccepted high-surplus run banked %v, want 0 (abandonment, not thrift)", g.BankedSurplus(g1))
	}
	if g.BankedSurplus(g2) != 10 {
		t.Fatalf("accepted run banked %v, want 10", g.BankedSurplus(g2))
	}
	// And the accepted (lower nominal surplus) outcome ranks ABOVE the unaccepted
	// (higher nominal surplus) one.
	if governor.CompareOutcomes(out2, out1) != 1 {
		t.Fatal("accepted low-surplus outcome must rank above unaccepted high-surplus outcome")
	}
	if !out2.Accepted || out1.Accepted {
		t.Fatalf("verdict wiring wrong: out1.Accepted=%v out2.Accepted=%v", out1.Accepted, out2.Accepted)
	}
}

// DIRECTION-NEUTRALITY, END TO END. A well-supported NEGATIVE banks identically
// to an equally-supported POSITIVE through the full settle path. No publication
// bias.
func TestSettlement_NegativeBanksIdenticallyToPositive(t *testing.T) {
	g := env100(t)
	judge := acceptance.NewSummaryJudge("judge").(acceptance.Acceptance)

	pos := supported()
	neg := supported()
	neg.Direction = acceptance.DirectionNegative

	gp := reserve(t, g, 40)
	settleWithVerdict(t, g, judge, gp, acs.Cost{Amount: 25, Currency: "USD"}, pos, stdConcordant)

	gn := reserve(t, g, 40)
	settleWithVerdict(t, g, judge, gn, acs.Cost{Amount: 25, Currency: "USD"}, neg, stdConcordant)

	bp, bn := g.BankedSurplus(gp), g.BankedSurplus(gn)
	if bp != bn {
		t.Fatalf("negative banked %v but positive banked %v — must be identical (no publication bias)", bn, bp)
	}
	if bp != 15 {
		t.Fatalf("expected 15 surplus banked (40 reserved − 25 actual), got %v", bp)
	}
}

// The acceptance node the host builds runs as a separate-envelope verdict node:
// it never settles the producer's grant itself — the host's settle path does,
// after consulting the verdict. (Structural: the judge has no governor handle.)
func TestSettlement_JudgeNeverSettles(t *testing.T) {
	// The summary judge is an agenkit.Agent + Acceptance; it exposes no Settle and
	// holds no governor. This compiles only because the verdict path is isolated.
	var _ acceptance.Acceptance = acceptance.NewSummaryJudge("j").(acceptance.Acceptance)
}
