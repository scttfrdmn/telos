// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acceptance_test

import (
	"context"
	"testing"

	"github.com/scttfrdmn/telos/acceptance"
)

const (
	concordant = acceptance.StandardOfProof("concordant")
	oracle     = acceptance.StandardOfProof("oracle")
	plausible  = acceptance.StandardOfProof("plausible")
)

func judge() acceptance.Acceptance {
	return acceptance.NewSummaryJudge("judge").(acceptance.Acceptance)
}

func indepSource(supports bool) acceptance.Source {
	return acceptance.Source{ID: "src", Independent: true, Supports: supports}
}

// Unprovenanced claim FAILS acceptance (architecture §4).
func TestRender_UnprovenancedFails(t *testing.T) {
	v, err := judge().Render(context.Background(), acceptance.Record{
		NodeID: "p", Content: "X causes Y", Direction: acceptance.DirectionPositive,
		// no Sources
	}, concordant)
	if err != nil {
		t.Fatal(err)
	}
	if v.Accepted {
		t.Fatal("an unprovenanced claim must not be accepted")
	}
}

// Never a bare "true": an accepted verdict always carries a labeled basis.
func TestRender_LabeledBasisNeverBareTrue(t *testing.T) {
	v, _ := judge().Render(context.Background(), acceptance.Record{
		NodeID: "p", Direction: acceptance.DirectionPositive,
		Sources: []acceptance.Source{indepSource(true), indepSource(true)},
	}, concordant)
	if !v.Accepted {
		t.Fatal("two independent supporting sources should meet the concordant bar")
	}
	if v.Basis != acceptance.ConcordantUnderTest {
		t.Fatalf("expected concordant-under-test basis, got %q", v.Basis)
	}
}

func TestRender_OracleRequiresReproduction(t *testing.T) {
	base := acceptance.Record{NodeID: "p", Direction: acceptance.DirectionPositive,
		Sources: []acceptance.Source{indepSource(true), indepSource(true)}}

	// Without reproduction, the oracle standard is not met.
	notRepro := base
	if v, _ := judge().Render(context.Background(), notRepro, oracle); v.Accepted {
		t.Fatal("oracle standard without reproduction must not be accepted")
	}
	// With reproduction, it's oracle-verified.
	repro := base
	repro.Reproduced = true
	if v, _ := judge().Render(context.Background(), repro, oracle); !v.Accepted || v.Basis != acceptance.OracleVerified {
		t.Fatalf("reproduced + concordant should be oracle-verified, got accepted=%v basis=%q", v.Accepted, v.Basis)
	}
}

// Genuine contestation is a first-class ACCEPTED outcome.
func TestRender_ContestedIsAccepted(t *testing.T) {
	v, _ := judge().Render(context.Background(), acceptance.Record{
		NodeID: "p", Direction: acceptance.DirectionInconclusive,
		Sources: []acceptance.Source{indepSource(true), {ID: "d", Independent: true, Supports: false}},
	}, concordant)
	if !v.Accepted {
		t.Fatal("a genuinely contested record is accepted (earned verdict of due process)")
	}
	if v.Basis != acceptance.Contested {
		t.Fatalf("expected contested basis, got %q", v.Basis)
	}
}

// DIRECTION-NEUTRALITY: a well-supported negative is accepted exactly like a
// well-supported positive, with the same basis. If negatives were treated worse,
// that would be rebuilt publication bias.
func TestRender_DirectionNeutral(t *testing.T) {
	mk := func(dir acceptance.Direction) acceptance.Record {
		return acceptance.Record{NodeID: "p", Direction: dir,
			Sources: []acceptance.Source{indepSource(true), indepSource(true)}}
	}
	pos, _ := judge().Render(context.Background(), mk(acceptance.DirectionPositive), concordant)
	neg, _ := judge().Render(context.Background(), mk(acceptance.DirectionNegative), concordant)

	if pos.Accepted != neg.Accepted {
		t.Fatalf("direction must not change acceptance: positive=%v negative=%v", pos.Accepted, neg.Accepted)
	}
	if pos.Basis != neg.Basis {
		t.Fatalf("direction must not change basis: positive=%q negative=%q", pos.Basis, neg.Basis)
	}
	if !neg.Accepted {
		t.Fatal("a well-supported negative must be accepted")
	}
}

// The standard gates strictness: the same record meets a lower bar but not a
// higher one.
func TestRender_StandardGatesStrictness(t *testing.T) {
	rec := acceptance.Record{NodeID: "p", Direction: acceptance.DirectionPositive,
		Sources: []acceptance.Source{indepSource(true)}} // one independent source

	// plausible needs 1 → accepted.
	if v, _ := judge().Render(context.Background(), rec, plausible); !v.Accepted {
		t.Fatal("one independent source should meet the plausible bar")
	}
	// concordant needs 2 → not accepted.
	if v, _ := judge().Render(context.Background(), rec, concordant); v.Accepted {
		t.Fatal("one independent source should NOT meet the concordant bar")
	}
}
