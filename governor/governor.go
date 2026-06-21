// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package governor is admission, conservation, and the surplus objective —
// ASBB retargeted at agents and compute, made grant-aware.
//
// INVARIANT 4 (unrecoverable if wrong): the governor is grant-RATE-aware, not
// total-budget-aware. The conserved quantity is dollars-over-period. Everything
// here operates on acs.Budget (amount AND period); nothing reduces a grant to a
// bare total.
//
// SCOPE: conservation (M1) + admission, surplus, and a WAL-backed ledger (M2).
//   - Reserve/Settle/Release/Remaining: conservation, Σ child ≤ parent, fails
//     closed (M1; mem.go).
//   - Admit: the grant-RATE admission decision — budget left relative to TIME
//     left (M2; admit.go).
//   - Surplus = grant − actual, credited ONLY on acceptance (M2; the lexicographic
//     acceptance→surplus ordering is enforced here and tested in the acceptance
//     package).
//   - WAL: escrow/settle written ahead to an append-only log and replayed on
//     restart, so conservation survives a crash (M2; wal.go).
//
// Still DEFERRED: distributed reconciliation across A2A (M5). burnrate (the
// default-standard thermostat) lives in its own package and consumes the
// reservoir/clock this governor exposes.
package governor

import (
	"context"

	"github.com/scttfrdmn/telos/acs"
)

// GrantID identifies a grant (a reserved slice of a parent's reservoir) in the
// ledger. The root grant of a run has no parent.
type GrantID string

// RootGrant is the conventional parent ID for a run's top-level grant — the one
// reserved directly against the run envelope, with no parent above it.
const RootGrant GrantID = ""

// Governor admits and conserves spend.
type Governor interface {
	// Admit makes the grant-RATE admission decision for a proposed unit of work:
	// is there budget left RELATIVE TO TIME LEFT (reservoir over clock), not just
	// budget left. ASBB as a spend-rate controller, not a total controller. It
	// reads (does not mutate) state; Reserve is the mutation that follows an
	// admit. Fails closed.
	Admit(ctx context.Context, q Quote, reservoir Reservoir, clock Clock) (Admission, error)

	// Reserve escrows a child's request against its parent grant, returning a
	// granted allocation. It FAILS CLOSED: if the request would breach
	// conservation (Σ children > parent remaining) it returns an error and
	// reserves nothing. The parent must already exist (Open it first); use
	// RootGrant as the parent to reserve against the run envelope.
	Reserve(ctx context.Context, parent GrantID, req acs.BudgetRequest) (*acs.BudgetGrant, error)

	// Settle records the ACTUAL cost of the work a grant funded and closes the
	// grant. The escrowed amount is released and the actual is debited from the
	// parent. In M1 settlement is unconditional (no acceptance gate); surplus
	// banking on acceptance is M2.
	Settle(ctx context.Context, id GrantID, actual acs.Cost, outcome Outcome) error

	// Release closes a grant without a charge (the work didn't run), returning
	// the full escrow to the parent.
	Release(ctx context.Context, id GrantID) error

	// Remaining reports a grant's currently unescrowed, unspent reservoir as a
	// Budget (amount over its remaining period — still a rate, never a bare
	// total).
	Remaining(id GrantID) acs.Budget
}

// Outcome is the result a node reports at settlement (architecture §5).
type Outcome struct {
	// Exit is the four-exit kind. A node that simply completes uses ExitDone;
	// ExitNegative (honest negative / contested) is a first-class completion.
	Exit ExitKind

	// Accepted comes from a Verdict rendered by a SEPARATE-envelope acceptance
	// node — NEVER self-rendered (invariant 10). Surplus banks only when this is
	// true; an unaccepted outcome banks zero (abandonment, not thrift). The
	// acceptance→surplus ordering is strictly lexicographic (see CompareOutcomes):
	// acceptance dominates surplus, never a weighted sum.
	Accepted bool

	// Surplus is grant − actual for this work, expressed as a Budget (a rate, not
	// a bare total — invariant 4). It is CREDITED only when Accepted; on an
	// unaccepted outcome it banks nothing regardless of its size. Settle computes
	// it; this field carries an outcome's reported surplus for ranking/feedback.
	Surplus acs.Budget

	// Cause explains WHY surplus arose (feeds planner/burnrate recalibration —
	// architecture §9: "surplus is a signal").
	Cause string
}

// ExitKind enumerates the four ranked exits (architecture invariant 9). Defined
// now so Outcome is complete; the reward semantics (surplus banking, negative
// results bank) are exercised in M2/M5.
type ExitKind string

const (
	ExitDone      ExitKind = "done"      // completed
	ExitHandoff   ExitKind = "handoff"   // handed off
	ExitNegative  ExitKind = "negative"  // honest negative / contested — a real result
	ExitExhausted ExitKind = "exhausted" // backstop exit (lowest reward)
)
