// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"testing"

	"github.com/scttfrdmn/telos/acs"
)

func bud(amount float64) acs.Budget { return acs.Budget{Amount: amount, Period: 1, Currency: "USD"} }

// THE UNDER-DELIVER GUARD (hard requirement, §9). A high-surplus but UNACCEPTED
// outcome must bank ZERO and rank BELOW a low-surplus but ACCEPTED one. If this
// test ever fails, someone has blended acceptance and surplus into one score —
// the exploit the lexicographic ordering exists to prevent.
func TestLexicographic_UnacceptedHighSurplusRanksLast(t *testing.T) {
	accepted := Outcome{Exit: ExitDone, Accepted: true, Surplus: bud(1)}     // banks 1
	unaccepted := Outcome{Exit: ExitDone, Accepted: false, Surplus: bud(99)} // banks 0

	// Banked surplus: the unaccepted outcome banks nothing despite its huge nominal surplus.
	if unaccepted.BankedSurplus() != 0 {
		t.Fatalf("unaccepted banked = %v, want 0", unaccepted.BankedSurplus())
	}
	if accepted.BankedSurplus() != 1 {
		t.Fatalf("accepted banked = %v, want 1", accepted.BankedSurplus())
	}

	// Ranking: accepted strictly outranks unaccepted regardless of surplus size.
	if CompareOutcomes(accepted, unaccepted) != 1 {
		t.Fatal("accepted (low surplus) must rank ABOVE unaccepted (high surplus)")
	}
	if CompareOutcomes(unaccepted, accepted) != -1 {
		t.Fatal("unaccepted (high surplus) must rank BELOW accepted (low surplus)")
	}
}

// Within the same acceptance tier, larger surplus ranks higher (tier-2 tiebreak).
func TestLexicographic_SurplusBreaksTieWithinAcceptance(t *testing.T) {
	big := Outcome{Accepted: true, Surplus: bud(10)}
	small := Outcome{Accepted: true, Surplus: bud(3)}
	if CompareOutcomes(big, small) != 1 {
		t.Fatal("among accepted outcomes, larger surplus ranks higher")
	}
	// Two unaccepted outcomes tie at 0 regardless of nominal surplus.
	u1 := Outcome{Accepted: false, Surplus: bud(50)}
	u2 := Outcome{Accepted: false, Surplus: bud(5)}
	if CompareOutcomes(u1, u2) != 0 {
		t.Fatal("two unaccepted outcomes both bank 0 and must tie (surplus cannot lift them)")
	}
}

// Direction-neutrality (#C3 mechanism at the governor layer): a well-supported
// NEGATIVE that is accepted banks identically to an accepted positive with the
// same surplus. No publication bias in the banking math.
func TestLexicographic_NegativeBanksLikePositive(t *testing.T) {
	positive := Outcome{Exit: ExitDone, Accepted: true, Surplus: bud(7)}
	negative := Outcome{Exit: ExitNegative, Accepted: true, Surplus: bud(7)}
	if positive.BankedSurplus() != negative.BankedSurplus() {
		t.Fatalf("accepted negative banked %v but positive banked %v — must be identical (direction-neutral)",
			negative.BankedSurplus(), positive.BankedSurplus())
	}
	if CompareOutcomes(positive, negative) != 0 {
		t.Fatal("an accepted negative and an accepted positive with equal surplus must rank equal")
	}
}
