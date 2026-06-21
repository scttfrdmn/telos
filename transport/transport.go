// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package transport defines Telos's placement vocabulary for the cohort provider
// seam (cohort v0.2.0). It supplies a cohort.Placement whose fallback ladder is
// the architecture's transport ladder (§7) — goroutine → A2A session → instance
// — carrying NO cloud/capacity-market vocabulary.
//
// This package is the concrete evidence that Telos is cohort's third independent
// consumer (after MPI and Slurm) compiling against the UNMODIFIED core: the
// generalized Placement seam (cohort#1 / cohort PR #3, shipped in v0.2.0) lets a
// non-provisioning consumer construct a cohort.EntityIntent without fabricating
// an instance type. cohort's ports.go is untouched by Telos.
package transport

import "github.com/spore-host/cohort"

// Rung is one rung of Telos's transport ladder (§7). It is the placement
// vocabulary of an AGENT runtime, not a cloud — no instance type, no AZ, no
// capacity model. The justification bar rises left to right; goroutine is the
// default, the instance rung is reserved for M6/M7.
type Rung string

const (
	// RungGoroutine: same trust + budget tree, I/O-bound fan-out, near-zero
	// marginal cost. The default rung.
	RungGoroutine Rung = "goroutine"
	// RungA2ASession: isolation / untrusted / resource boundary — own session
	// (its own host instance, reached over the A2A contract).
	RungA2ASession Rung = "a2a-session"
	// RungInstance: spore.host instance (GPU / sovereign data / heavy compute).
	// Present in the ladder for completeness; not placeable until M6/M7.
	RungInstance Rung = "instance"
)

// DefaultLadder is the approved fallback ladder, cheapest/least-isolated first.
// The placer selects a STARTING rung by trigger (§7, first-trigger-wins); the
// reconciler may Advance along the remaining ladder on a fallback-eligible fault.
var DefaultLadder = []Rung{RungGoroutine, RungA2ASession, RungInstance}

// Placement is Telos's cohort.Placement: a transport rung plus the approved
// ladder it may fall back along. It satisfies cohort.Placement with two methods
// and never exposes a cloud field — the core renders Current() and advances the
// ladder via Advance(), learning only the rung's legible name and class.
type Placement struct {
	// Ladder is the approved sequence of rungs (never substituted outside).
	Ladder []Rung
	// idx is the currently selected rung within Ladder.
	idx int
}

// NewPlacement builds a Placement starting at the given rung, falling back along
// the approved ladder from that rung onward. An unknown rung yields a single-rung
// placement (no fallback) rather than panicking — the placer validates rungs.
func NewPlacement(start Rung, ladder []Rung) Placement {
	if len(ladder) == 0 {
		ladder = DefaultLadder
	}
	for i, r := range ladder {
		if r == start {
			return Placement{Ladder: ladder, idx: i}
		}
	}
	// Start rung not in the ladder: treat it as a standalone single rung.
	return Placement{Ladder: []Rung{start}, idx: 0}
}

// Rung returns the currently selected transport rung.
func (p Placement) Rung() Rung { return p.Ladder[p.idx] }

// Current implements cohort.Placement: the legible identity of the active rung.
// Name is the rung; Class is "transport" (a transport has no capacity model).
// WarmStart is always false — an agent transport has no warm-resume concept.
func (p Placement) Current() cohort.PlacementRung {
	return cohort.PlacementRung{
		Name:      string(p.Ladder[p.idx]),
		Class:     "transport",
		WarmStart: false,
	}
}

// Advance implements cohort.Placement: the next rung in the approved ladder, or
// false when exhausted. It never substitutes a rung outside Ladder. The returned
// Placement is a new value; the receiver is not mutated.
func (p Placement) Advance() (cohort.Placement, bool) {
	if p.idx+1 >= len(p.Ladder) {
		return nil, false
	}
	return Placement{Ladder: p.Ladder, idx: p.idx + 1}, true
}

// Compile-time proof Telos's transport Placement satisfies the cohort seam.
var _ cohort.Placement = Placement{}
