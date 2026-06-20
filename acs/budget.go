// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import (
	"fmt"
	"time"
)

// Budget is a GRANT: an amount over a period. Both axes are conserved.
//
// INVARIANT 4 (unrecoverable if wrong — architecture §9, hand-off note).
// The conserved quantity is dollars-OVER-PERIOD, not dollars. A bare total is
// not a budget here: an amount with no clock cannot say whether it is being
// spent too fast or too slow, and the entire surplus/burn-rate machine depends
// on the rate. Both underspend and overspend are failures.
//
// This type is deliberately shaped so that you cannot obtain "how much money is
// left" without also stating "over what remaining time." The governor (M2) is
// rate-aware against this; burnrate (M2) reads reservoir-over-clock from it.
type Budget struct {
	// Amount is the reservoir in whole currency units (USD). The reservoir half
	// of the grant.
	Amount float64 `json:"amount"`

	// Period is the clock the amount is spread across — the other half of the
	// grant. A Budget with a zero Period is invalid by construction: it would be
	// a total, which this system does not model.
	Period time.Duration `json:"period"`

	// Currency is the denomination. Default "USD" when empty.
	Currency string `json:"currency,omitempty"`
}

// Rate returns the grant's spend rate — amount per unit time — as currency units
// per second. This is the quantity the governor admits against and burnrate
// lands to zero at the deadline. It is the reason a clinical-grade answer
// affordable in month 2 is declined in month 11.
//
// Returns 0 for a zero/negative period; callers should reject such a Budget via
// Validate rather than divide by it.
func (b Budget) Rate() float64 {
	if b.Period <= 0 {
		return 0
	}
	return b.Amount / b.Period.Seconds()
}

// RatePerDay is Rate expressed per 24h — a more legible unit for grants whose
// period is measured in months.
func (b Budget) RatePerDay() float64 {
	return b.Rate() * (24 * time.Hour).Seconds()
}

// Denomination returns the budget's currency, defaulting to "USD" when unset.
// Currencies that differ cannot be combined: conservation arithmetic (Σ child ≤
// parent) is only meaningful within one denomination.
func (b Budget) Denomination() string {
	if b.Currency == "" {
		return "USD"
	}
	return b.Currency
}

// Validate enforces the grant shape: a positive amount over a positive period.
// A zero period is the canonical "someone modeled a total" bug and is rejected
// here so it cannot propagate.
func (b Budget) Validate() error {
	if b.Amount < 0 {
		return fmt.Errorf("budget amount is negative (%v)", b.Amount)
	}
	if b.Period <= 0 {
		return fmt.Errorf("budget period must be positive (a grant is amount OVER period, not a total); got %v", b.Period)
	}
	return nil
}

// BudgetRequest is a child node's ask against its parent's grant. It is distinct
// from a granted BudgetGrant (which the governor issues on Reserve) so the type
// system separates "what was requested" from "what was conserved and granted."
//
// A request is a fraction of the parent's rate over a slice of the parent's
// remaining period — never an absolute total, for the same reason Budget isn't.
type BudgetRequest struct {
	// Amount requested from the parent reservoir.
	Amount float64 `json:"amount"`

	// Period over which the child intends to spend it. Must be positive and
	// must fit within the parent's remaining clock (checked by the governor).
	Period time.Duration `json:"period"`

	// MaxFraction optionally caps this request as a fraction (0,1] of the
	// parent's remaining grant — a declarative conservation hint the binder and
	// governor can honor before computing absolute amounts. Zero means unset.
	MaxFraction float64 `json:"max_fraction,omitempty"`
}

// Validate checks a request is well-formed in isolation. The cross-node
// conservation check — Σ(child requests) ≤ parent remaining, recursively, fails
// closed — is the governor's job (M2), not the schema's; the schema only ensures
// each request carries a clock so the governor CAN check a rate.
func (r BudgetRequest) Validate() error {
	if r.Amount < 0 {
		return fmt.Errorf("budget request amount is negative (%v)", r.Amount)
	}
	if r.Period <= 0 {
		return fmt.Errorf("budget request period must be positive (rate, not total); got %v", r.Period)
	}
	if r.MaxFraction < 0 || r.MaxFraction > 1 {
		return fmt.Errorf("budget request max_fraction must be in (0,1]; got %v", r.MaxFraction)
	}
	return nil
}

// asBudget materializes a request as a Budget (amount over its own period). Used
// when a request is the whole of a sub-run's grant.
func (r BudgetRequest) asBudget(currency string) Budget {
	return Budget{Amount: r.Amount, Period: r.Period, Currency: currency}
}
