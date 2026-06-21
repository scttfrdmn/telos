// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package a2a carries the conservation invariant and kill-switch ACROSS the
// session boundary (architecture §9). In-process, budget/deadline/cancel are
// free context primitives; across an A2A session they are EXPLICIT wire fields:
//
//	out:  {grant_id, reservation, deadline, cancel_token}
//	back: {result, cost_settled, outcome, child_settlements[]}
//
// The discipline that must survive the boundary: a parent's budget still bounds
// a remote child's (Σ child ≤ parent, fails closed); cancel propagates to the
// remote session; and the remote's spend SETTLES back to the parent's ledger.
// M4 exercises this over a local loopback session (an in-process host driven via
// the A2A contract); M5 reconciles distributed settlement via the WAL.
package a2a

import (
	"time"

	"github.com/scttfrdmn/telos/acs"
)

// Budget is the explicit budget envelope sent ON the wire to a remote session.
// It is the in-context grant made wire-explicit (§5/§9). The remote child is
// bounded by Reservation (amount over period — a grant rate, never a bare total,
// invariant 4); CancelToken lets the parent cancel the remote session out of band
// (cancel also rides HTTP request-context cancellation).
type Budget struct {
	GrantID     string        `json:"grant_id"`
	Amount      float64       `json:"reservation_amount"`
	Period      time.Duration `json:"reservation_period"`
	Currency    string        `json:"currency,omitempty"`
	DeadlineNs  int64         `json:"deadline_unix_nano,omitempty"`
	CancelToken string        `json:"cancel_token,omitempty"`
}

// Reservation renders the wire budget as an acs.BudgetRequest the remote's
// governor can reserve against — preserving the grant-rate shape across the wire.
func (b Budget) Reservation() acs.BudgetRequest {
	return acs.BudgetRequest{Amount: b.Amount, Period: b.Period}
}

// Settlement is what a remote session returns to the parent's ledger after
// running. cost_settled + outcome + child_settlements[] (§9). The parent settles
// CostSettled against the grant it reserved for this child.
type Settlement struct {
	// CostSettled is the total cost the remote run incurred (in Currency).
	CostSettled float64 `json:"cost_settled"`
	// CostSynthesized is the MODELED portion of CostSettled (issue #23) — kept
	// distinguishable across the wire so the parent ledger never blends a measured
	// and a modeled quantity.
	CostSynthesized float64 `json:"cost_synthesized"`
	Currency        string  `json:"currency,omitempty"`
	// Outcome is the four-exit kind the remote run reported ("done", "negative", …).
	Outcome string `json:"outcome"`
	// Accepted is the remote run's acceptance verdict (from its separate-envelope
	// judge) — surplus banks at the parent only if accepted (lexicographic, §9).
	Accepted bool `json:"accepted"`
	// ChildSettlements are the remote run's own children's settlements, so the
	// parent ledger can reconcile the full sub-tree (M5 walks these; M4 carries them).
	ChildSettlements []Settlement `json:"child_settlements,omitempty"`
}

// Cost renders the settlement's spend as an acs.Cost (metered vs synthesized
// split preserved) for the parent governor's Settle.
func (s Settlement) Cost() acs.Cost {
	cur := s.Currency
	if cur == "" {
		cur = "USD"
	}
	return acs.Cost{Amount: s.CostSettled, Synthesized: s.CostSynthesized, Currency: cur}
}
