// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package burnrate is the landing controller: a reservoir-over-clock thermostat
// that sets the DEFAULT standard of proof so the grant lands near-zero at the
// deadline — neither starving early nor dying rich (architecture §9).
//
// The load-bearing idea (invariant 4, time half): the default standard is a
// VISIBLE FUNCTION of reservoir-over-clock, not a constant. A clinical-grade
// default that is affordable in month 2 of a grant is declined in month 11. As
// the grant gets richer per remaining unit of time, burn-rate can afford a
// higher default standard; as it gets leaner, it steps the default down.
//
// The EXACT curve is an open empirical fork (§15: burn-rate landing policy) and
// is deliberately NOT resolved here. burnrate selects among NAMED, swappable
// LandingStrategy implementations so the curve is inspectable and testable and
// can be changed without touching consumers.
package burnrate

import (
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// BurnRate sets the default standard of proof from a reservoir-over-clock signal.
// It is the §5 interface, made concrete: DefaultStandard is a pure function of
// the live grant signal so the landing behavior is observable and testable.
type BurnRate struct {
	strategy LandingStrategy
}

// New builds a BurnRate with a landing strategy. A nil strategy uses
// DefaultStrategy (a provisional curve — the shape is an open fork).
func New(strategy LandingStrategy) *BurnRate {
	if strategy == nil {
		strategy = DefaultStrategy()
	}
	return &BurnRate{strategy: strategy}
}

// DefaultStandard returns the default StandardOfProof for the current grant phase
// (architecture §5). It is a pure function of (reservoir, clock): given the same
// signal it always returns the same standard, so the landing curve is inspectable.
//
// When the signal is absent or degenerate (no clock, no reservoir), it returns
// the empty standard — the caller then falls back to the seed default (see the
// host's resolution order). burn-rate is a SOURCE of the default, not the only
// one: no signal → no opinion, not a wrong opinion.
func (b *BurnRate) DefaultStandard(reservoir governor.Reservoir, clock governor.Clock) acs.StandardOfProof {
	if clock.Total <= 0 || reservoir.Remaining <= 0 {
		return "" // no usable signal; caller falls back to the seed default
	}
	return b.strategy.Standard(Signal{
		Reservoir:         reservoir.Remaining,
		ReservoirFraction: reservoir.Fraction(),
		ClockFraction:     clock.Fraction(), // 1.0 = whole period left, →0 = at the wall
	})
}

// StrategyName reports the active landing strategy's name (for observability).
func (b *BurnRate) StrategyName() string { return b.strategy.Name() }
