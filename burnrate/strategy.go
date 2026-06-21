// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package burnrate

import "github.com/scttfrdmn/telos/acs"

// Signal is the reservoir-over-clock input a LandingStrategy maps to a standard.
// It is the normalized form of the grant phase so a strategy can be a pure,
// inspectable function.
type Signal struct {
	// Reservoir is the remaining grant amount (absolute). Strategies that pace on
	// burn rate use ReservoirFraction; this is kept for strategies that key off
	// absolute headroom.
	Reservoir float64

	// ReservoirFraction is remaining/original reservoir, in [0,1]. 1.0 = nothing
	// spent; →0 = reservoir nearly gone. Zero when the original is unknown.
	ReservoirFraction float64

	// ClockFraction is remaining/total period, in [0,1]. 1.0 = whole period left;
	// →0 = at the deadline.
	ClockFraction float64
}

// Pace is the ratio of reservoir-fraction to clock-fraction — the core landing
// signal. Pace > 1 means the grant is RICH relative to time (spent slower than
// the clock; can afford a higher standard). Pace < 1 means LEAN relative to time
// (spent faster than the clock; must step the standard down to land near zero).
// Pace == 1 is on-track. Returns 1 (on-track) when the clock signal is absent.
func (s Signal) Pace() float64 {
	if s.ClockFraction <= 0 {
		return 1
	}
	return s.ReservoirFraction / s.ClockFraction
}

// LandingStrategy maps a reservoir-over-clock Signal to a default StandardOfProof.
// It is NAMED and swappable: the EXACT curve is an open empirical fork (§15:
// burn-rate landing policy), so the strategy is the seam at which that choice is
// made, kept out of the consumers.
type LandingStrategy interface {
	// Name identifies the strategy for provenance/observability.
	Name() string
	// Standard maps a grant-phase signal to a default standard of proof. Must be
	// a pure function of the signal so the landing curve is inspectable/testable.
	Standard(s Signal) acs.StandardOfProof
}

// DefaultStrategy returns the provisional landing strategy. PLACEHOLDER CURVE:
// its exact thresholds are NOT the resolution of the §15 landing-policy fork —
// it is a reasonable, monotonic default chosen so the system has a working
// thermostat while the curve is studied empirically.
func DefaultStrategy() LandingStrategy { return PaceThresholdStrategy{} }

// PaceThresholdStrategy is a simple monotonic landing curve: the default standard
// steps up as the grant runs richer-per-remaining-time (higher Pace) and down as
// it runs leaner. The thresholds are provisional (the curve shape is the open
// fork); what is committed is the MONOTONICITY — a richer pace never yields a
// lower standard.
type PaceThresholdStrategy struct{}

// Name implements LandingStrategy.
func (PaceThresholdStrategy) Name() string { return "pace-threshold" }

// Standard implements LandingStrategy. Monotonic in Pace:
//
//	pace ≥ 1.5  → oracle      (well ahead — afford the strongest standard)
//	pace ≥ 0.9  → concordant  (on track — the workhorse standard)
//	pace ≥ 0.5  → plausible   (behind — economize)
//	pace <  0.5 → scoping     (nearly out of runway — cheapest bar only)
func (PaceThresholdStrategy) Standard(s Signal) acs.StandardOfProof {
	switch p := s.Pace(); {
	case p >= 1.5:
		return acs.StandardOracle
	case p >= 0.9:
		return acs.StandardConcordant
	case p >= 0.5:
		return acs.StandardPlausible
	default:
		return acs.StandardScoping
	}
}

// standardRank orders standards from cheapest to strictest, for testing
// monotonicity. Exported via StandardRank for consumers/tests.
func standardRank(s acs.StandardOfProof) int {
	switch s {
	case acs.StandardScoping:
		return 0
	case acs.StandardPlausible:
		return 1
	case acs.StandardConcordant:
		return 2
	case acs.StandardOracle:
		return 3
	default:
		return -1
	}
}

// StandardRank returns an ordinal for a standard of proof (cheapest=0, strictest
// highest), or -1 if unknown. Useful for asserting a landing curve is monotonic.
func StandardRank(s acs.StandardOfProof) int { return standardRank(s) }
