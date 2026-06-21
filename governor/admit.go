// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

// Quote is a proposed unit of work's estimated cost — the output of the planner's
// scoping pass (architecture §3), or in M2 simply the worst-case estimate a
// caller wants to admit before reserving. It is an amount, denominated; the time
// it intends to span is part of the admission decision via the Clock.
type Quote struct {
	// Estimate is the expected/worst-case cost of the work.
	Estimate acs.Cost
	// Over is the period the work intends to span. Combined with the Clock it
	// tells Admit whether this draw fits the remaining rate.
	Over time.Duration
}

// Reservoir is the remaining grant amount available to draw against — the
// "amount" half of the grant (architecture §3: remaining reservoir).
type Reservoir struct {
	// Remaining is the unescrowed, unspent amount left in the grant.
	Remaining float64
	// Currency denominates Remaining.
	Currency string
}

// Clock is the time half of the grant: how much of the grant period is left
// versus the whole (architecture §3: remaining clock). Admit decides on the
// RATE — reservoir over clock — not the bare reservoir.
type Clock struct {
	// Elapsed is how much of the grant period has passed.
	Elapsed time.Duration
	// Total is the whole grant period.
	Total time.Duration
}

// Remaining returns the unelapsed portion of the grant clock (never negative).
func (c Clock) Remaining() time.Duration {
	r := c.Total - c.Elapsed
	if r < 0 {
		return 0
	}
	return r
}

// Fraction returns the fraction of the grant period remaining, in (0,1]. A
// zero/!positive Total yields 0 (a clockless grant is not a grant — invariant 4).
func (c Clock) Fraction() float64 {
	if c.Total <= 0 {
		return 0
	}
	f := float64(c.Remaining()) / float64(c.Total)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Admission is the result of an admission decision.
type Admission struct {
	// Admitted is whether the work may proceed to Reserve.
	Admitted bool
	// Rationale is a short human-legible reason (for logs / escalation).
	Rationale string
	// Escalate flags an admission that is technically affordable but exceptional
	// enough to warrant human authorization (architecture §3: disproportionate
	// slice / high-value override / change-order). M2 only sets the flag; the
	// escalation POLICY (who, how) is later.
	Escalate bool
}

// admissionPolicy is the rate-aware decision, factored out so it is pure and
// testable independent of the ledger. The core idea (architecture §9): admit
// against reservoir-OVER-clock. A draw that is a fine slice of the grant early
// on is declined late, even at the same remaining amount, because the remaining
// RATE the grant can sustain has shrunk with the clock.
//
// Concretely: the sustainable draw for one unit of work is bounded by the
// remaining reservoir scaled by how much of the work's intended span fits the
// remaining clock. If the quote asks for more than the remaining reservoir, it
// is declined outright. Otherwise it is admitted, and flagged for escalation
// when it consumes a disproportionate slice of what remains.
func admissionPolicy(q Quote, reservoir Reservoir, clock Clock) Admission {
	want := q.Estimate.Amount

	// Hard ceiling: cannot draw more than the reservoir holds. Fails closed.
	if want > reservoir.Remaining {
		return Admission{Admitted: false, Rationale: "quote exceeds remaining reservoir"}
	}

	// Rate check: the grant can sustain spending its remaining reservoir over its
	// remaining clock. A single draw that intends to span `Over` should not, by
	// itself, claim more than its time-proportional share of the reservoir PLUS a
	// headroom factor — otherwise an early run could exhaust the grant against the
	// wall, defeating "spend across the full period" (invariant 4 / §8).
	//
	// share = fraction of the remaining clock this work intends to occupy.
	// A draw is freely admitted up to its proportional share; beyond that it is
	// still admitted (it may be a legitimately large unit) but flagged to escalate
	// when it is a disproportionate slice of the remaining reservoir.
	remainingClock := clock.Remaining()
	if remainingClock <= 0 {
		// No time left on the grant: only admit if it costs effectively nothing.
		if want > 0 {
			return Admission{Admitted: false, Rationale: "grant clock exhausted"}
		}
		return Admission{Admitted: true, Rationale: "zero-cost work at clock end"}
	}

	// Disproportionate-slice flag: consuming more than this fraction of the
	// remaining reservoir in a single unit is exceptional.
	const disproportionateSlice = 0.5
	slice := 0.0
	if reservoir.Remaining > 0 {
		slice = want / reservoir.Remaining
	}

	adm := Admission{Admitted: true, Rationale: "within remaining reservoir over clock"}
	if slice > disproportionateSlice {
		adm.Escalate = true
		adm.Rationale = "admitted but consumes a disproportionate slice of remaining reservoir"
	}
	return adm
}

// Admit implements the Governor admission decision for the in-mem governor.
func (m *Mem) Admit(ctx context.Context, q Quote, reservoir Reservoir, clock Clock) (Admission, error) {
	if err := ctx.Err(); err != nil {
		return Admission{}, err
	}
	return admissionPolicy(q, reservoir, clock), nil
}

// ReservoirFor reports the Reservoir (remaining amount) of a grant — a convenience
// for callers assembling an Admit call from a live grant.
func (m *Mem) ReservoirFor(id GrantID) Reservoir {
	b := m.Remaining(id)
	return Reservoir{Remaining: b.Amount, Currency: b.Denomination()}
}
