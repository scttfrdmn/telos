// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

// The objective is LEXICOGRAPHIC, never weighted (architecture §9, hard
// requirement): (1) reach attested acceptance, THEN (2) maximize surplus. There
// must be NO code path where acceptance and surplus combine into one score — if
// an agent could bank margin by under-delivering, the ordering would be wrong.
//
// CompareOutcomes is the single comparison the system ranks by. It compares
// acceptance FIRST and only breaks ties by surplus. There is deliberately no
// function here that returns a blended float: the type of the comparison is the
// guard. The under-deliver-exploit test (acceptance package) asserts a
// high-surplus-but-unaccepted outcome ranks below a low-surplus-but-accepted one.

// CompareOutcomes orders two outcomes by the lexicographic objective. It returns
//
//	-1 if a ranks BELOW b,
//	+1 if a ranks ABOVE b,
//	 0 if they rank equal.
//
// Tier 1 — acceptance: an Accepted outcome always ranks above an unaccepted one,
// regardless of surplus. Tier 2 — surplus: among outcomes with the SAME
// acceptance, the larger banked surplus ranks higher. Surplus only ever breaks a
// tie within the same acceptance tier; it can never lift an unaccepted outcome
// above an accepted one.
func CompareOutcomes(a, b Outcome) int {
	if a.Accepted != b.Accepted {
		if a.Accepted {
			return 1
		}
		return -1
	}
	// Same acceptance tier: rank by BANKED surplus. An unaccepted outcome banks
	// nothing (BankedSurplus == 0), so two unaccepted outcomes tie at 0 here
	// regardless of their nominal surplus — under-delivery banks nothing.
	as, bs := a.BankedSurplus(), b.BankedSurplus()
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// BankedSurplus is the surplus an outcome actually banks: its surplus amount if
// Accepted, otherwise ZERO. This is the only place "surplus that counts" is
// computed, and it is gated purely on Accepted — never blended with it. Surplus
// on an unaccepted result is abandonment, not thrift, and banks nothing
// (invariant 8).
func (o Outcome) BankedSurplus() float64 {
	if !o.Accepted {
		return 0
	}
	return o.Surplus.Amount
}
