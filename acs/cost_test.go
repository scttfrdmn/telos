// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import "testing"

// The pin (step 0): a metered cost and a synthesized (modeled) cost must NEVER
// blend into one indistinguishable quantity, because burnrate paces the grant on
// these numbers. Summing must keep the modeled portion recoverable.
func TestCost_MeteredAndSynthesizedStaySeparable(t *testing.T) {
	metered := MeteredCost(5, "USD", TokenUsage{InputTokens: 100})
	synth := SynthesizedCost(3, "USD", TokenUsage{InputTokens: 200})

	sum := metered.Add(synth)
	if sum.Amount != 8 {
		t.Fatalf("total = %v, want 8", sum.Amount)
	}
	// The modeled portion is preserved, not OR'd away into a boolean.
	if sum.Synthesized != 3 {
		t.Fatalf("synthesized portion = %v, want 3 (must survive aggregation)", sum.Synthesized)
	}
	// True (billed) spend is recoverable.
	if sum.Metered() != 5 {
		t.Fatalf("metered portion = %v, want 5", sum.Metered())
	}
	if !sum.HasSynthesized() {
		t.Fatal("a sum containing modeled cost must report HasSynthesized")
	}
	if sum.FullySynthesized() {
		t.Fatal("a mixed sum is not fully synthesized")
	}
}

func TestCost_MeteredIsFullyBilled(t *testing.T) {
	c := MeteredCost(10, "USD", TokenUsage{})
	if c.HasSynthesized() {
		t.Fatal("a metered cost has no synthesized portion")
	}
	if c.Metered() != 10 {
		t.Fatalf("metered = %v, want 10", c.Metered())
	}
}

func TestCost_SynthesizedIsFullyModeled(t *testing.T) {
	c := SynthesizedCost(10, "USD", TokenUsage{})
	if !c.FullySynthesized() {
		t.Fatal("a synthesized cost is fully modeled")
	}
	if c.Metered() != 0 {
		t.Fatalf("metered = %v, want 0 (nothing billed)", c.Metered())
	}
}

func TestCost_AddManyPreservesSplit(t *testing.T) {
	// Three cloud calls ($2 each, metered) + two local calls ($1 each, modeled).
	var total Cost
	for i := 0; i < 3; i++ {
		total = total.Add(MeteredCost(2, "USD", TokenUsage{}))
	}
	for i := 0; i < 2; i++ {
		total = total.Add(SynthesizedCost(1, "USD", TokenUsage{}))
	}
	if total.Amount != 8 {
		t.Fatalf("total = %v, want 8", total.Amount)
	}
	if total.Synthesized != 2 {
		t.Fatalf("synthesized = %v, want 2 (the two local calls)", total.Synthesized)
	}
	if total.Metered() != 6 {
		t.Fatalf("metered = %v, want 6 (the three cloud calls)", total.Metered())
	}
}

func TestCost_AddCurrencyMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("adding mismatched currencies must panic")
		}
	}()
	_ = MeteredCost(1, "USD", TokenUsage{}).Add(MeteredCost(1, "EUR", TokenUsage{}))
}
