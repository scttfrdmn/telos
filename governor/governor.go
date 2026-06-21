// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package governor is admission, conservation, and the surplus objective —
// ASBB retargeted at agents and compute, made grant-aware.
//
// INVARIANT 4 (unrecoverable if wrong): the governor is grant-RATE-aware, not
// total-budget-aware. The conserved quantity is dollars-over-period. Everything
// here operates on acs.Budget (amount AND period); nothing reduces a grant to a
// bare total.
//
// M1 SCOPE: this package provides the Governor interface and a conservation-only,
// in-memory implementation (see mem.go) — just enough for the gateway's metered
// loop to escrow and settle honestly. The following are deliberately DEFERRED to
// M2 and are NOT implemented here:
//   - Admit (admission against a Quote / Reservoir / Clock)
//   - burnrate default-standard modulation
//   - surplus-banks-iff-accepted (M1 settles unconditionally to actual cost)
//   - WAL / persistence / distributed reconciliation
//
// Keeping the interface real (rather than a private counter in the gateway)
// means the metered loop is exercised against the actual conservation contract
// from M1, without pulling M2's open policy forward.
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

// Governor admits and conserves spend. M1 implements the conservation subset;
// see the package doc for what M2 adds (Admit, surplus, WAL).
type Governor interface {
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

// Outcome is the result a node reports at settlement. The architecture's full
// Outcome (§5) carries ExitKind, Accepted (from a Verdict), Surplus, and Cause.
// M1 needs only enough to settle; the acceptance-gated fields land in M2 with
// the acceptance judgment, so they are present but inert here.
type Outcome struct {
	// Exit is the four-exit kind. M1 nodes that simply complete use ExitDone.
	Exit ExitKind

	// Accepted comes from a Verdict rendered by a separate-envelope acceptance
	// node — NEVER self-rendered (invariant 10). In M1 there is no acceptance
	// judgment yet, so this is always false and surplus never banks on it.
	Accepted bool
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
