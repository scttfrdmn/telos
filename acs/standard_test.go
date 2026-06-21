// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import "testing"

func TestResolveStandard_Precedence(t *testing.T) {
	s := &Spec{Standard: StandardPlausible} // seed default
	nodeOverride := &Node{Standard: StandardOracle}
	noOverride := &Node{}

	// 1. Node override wins over everything (per-question stakes).
	if got := s.ResolveStandard(nodeOverride, StandardConcordant); got != StandardOracle {
		t.Fatalf("node override should win, got %s", got)
	}
	// 2. With no node override, burn-rate's default wins over the seed.
	if got := s.ResolveStandard(noOverride, StandardConcordant); got != StandardConcordant {
		t.Fatalf("burn-rate default should win over seed, got %s", got)
	}
	// 3. With no override and no burn-rate signal (""), fall back to the seed.
	if got := s.ResolveStandard(noOverride, ""); got != StandardPlausible {
		t.Fatalf("should fall back to seed default, got %s", got)
	}
}

func TestSeedDefaultStandard_Fallback(t *testing.T) {
	// An empty seed standard falls back to the system fallback.
	s := &Spec{}
	if got := s.SeedDefaultStandard(); got != StandardConcordant {
		t.Fatalf("empty seed should fall back to concordant, got %s", got)
	}
	// A set seed standard is returned as-is.
	s2 := &Spec{Standard: StandardOracle}
	if got := s2.SeedDefaultStandard(); got != StandardOracle {
		t.Fatalf("seed standard should be returned, got %s", got)
	}
}
