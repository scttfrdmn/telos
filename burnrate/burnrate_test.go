// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package burnrate

import (
	"testing"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// The default standard VISIBLY VARIES with grant phase — it is not a constant.
// A grant that is rich relative to its remaining time affords a higher standard
// than the same reservoir with little time left.
func TestDefaultStandard_VariesWithGrantPhase(t *testing.T) {
	b := New(nil) // default strategy

	// Rich phase: 90% of reservoir left, only 40% of the clock left → pace > 1.
	rich := b.DefaultStandard(
		governor.Reservoir{Remaining: 90, Original: 100},
		governor.Clock{Elapsed: 60, Total: 100},
	)
	// Lean phase: 20% of reservoir left, 60% of the clock left → pace < 1.
	lean := b.DefaultStandard(
		governor.Reservoir{Remaining: 20, Original: 100},
		governor.Clock{Elapsed: 40, Total: 100},
	)

	if StandardRank(rich) <= StandardRank(lean) {
		t.Fatalf("rich-phase standard (%s) must be stricter than lean-phase (%s)", rich, lean)
	}
}

// The landing curve is MONOTONIC in pace: as the grant runs richer per remaining
// time, the standard never drops. (The exact thresholds are an open fork; the
// monotonicity is what's committed.)
func TestLandingCurve_MonotonicInPace(t *testing.T) {
	s := PaceThresholdStrategy{}
	prev := -1
	// Sweep clock from nearly-done to fresh at a fixed reservoir fraction; pace
	// rises as clock fraction falls, so the standard should be non-decreasing.
	for _, clockFrac := range []float64{0.95, 0.8, 0.6, 0.4, 0.2, 0.05} {
		std := s.Standard(Signal{ReservoirFraction: 0.8, ClockFraction: clockFrac})
		r := StandardRank(std)
		if r < prev {
			t.Fatalf("non-monotonic: clockFrac=%.2f gave %s (rank %d) below previous rank %d", clockFrac, std, r, prev)
		}
		prev = r
	}
}

// The clinical-grade default is affordable when ahead and DECLINED when behind —
// the architecture's "month 2 vs month 11" property, made concrete.
func TestOracleAffordableEarlyDeclinedLate(t *testing.T) {
	b := New(nil)
	// Way ahead: barely spent, late in the clock → oracle affordable.
	ahead := b.DefaultStandard(governor.Reservoir{Remaining: 95, Original: 100}, governor.Clock{Elapsed: 50, Total: 100})
	if ahead != acs.StandardOracle {
		t.Fatalf("well-ahead grant should afford oracle, got %s", ahead)
	}
	// Way behind: nearly out of reservoir with time still to cover → not oracle.
	behind := b.DefaultStandard(governor.Reservoir{Remaining: 10, Original: 100}, governor.Clock{Elapsed: 20, Total: 100})
	if behind == acs.StandardOracle {
		t.Fatal("a grant running out of runway must NOT default to the clinical-grade standard")
	}
}

// No usable signal → no opinion (empty), so the caller falls back to the seed.
func TestNoSignalYieldsEmpty(t *testing.T) {
	b := New(nil)
	if got := b.DefaultStandard(governor.Reservoir{}, governor.Clock{}); got != "" {
		t.Fatalf("no signal should yield empty (fall back to seed), got %q", got)
	}
	if got := b.DefaultStandard(governor.Reservoir{Remaining: 50, Original: 100}, governor.Clock{Total: 0}); got != "" {
		t.Fatalf("no clock should yield empty, got %q", got)
	}
}

func TestStrategyName(t *testing.T) {
	if New(nil).StrategyName() != "pace-threshold" {
		t.Fatal("default strategy should be pace-threshold")
	}
}
