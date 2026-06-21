// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import "fmt"

// Cost is the metered cost of one unit of work — a model call or (later) a
// synthesized computation. It lives in acs alongside Budget because both are
// money types, and because the gateway (which produces Cost) and the governor
// (which settles it) must share the type without importing each other.
//
// METERED vs MODELED — kept distinguishable through aggregation. A cost has two
// components that always sum to Amount:
//
//   - the METERED portion: what a provider actually billed (real dollars), and
//   - the SYNTHESIZED portion: what the gateway ASSIGNED because no provider
//     billed it (local-model work) — a modeled quantity from a policy formula,
//     not a measurement.
//
// Synthesized holds the modeled portion explicitly (not a boolean), so summing a
// metered cost and a synthesized cost preserves the split: "$8 total, $3 of it
// modeled" rather than a lossy "$8, synthesized." This matters because burnrate
// paces the grant partly on these numbers (architecture §9) — a measured and a
// modeled quantity must never silently blend into one "this run cost $X."
//
// A Cost is a point amount in a currency, NOT a grant: it is what something
// actually cost, settled against a grant's reservoir. Unlike Budget it carries
// no period — a single call's cost is an instantaneous draw; the RATE lives on
// the Budget it settles against (invariant 4 keeps the clock on the grant).
type Cost struct {
	// Amount is the TOTAL cost in Currency units (metered + synthesized).
	Amount float64 `json:"amount"`

	// Synthesized is the portion of Amount the gateway MODELED rather than a
	// provider billing it (local work). 0 ≤ Synthesized ≤ Amount. A fully-billed
	// cloud cost has Synthesized == 0; a fully-local cost has Synthesized ==
	// Amount; a sum of both keeps the modeled portion separable.
	Synthesized float64 `json:"synthesized,omitempty"`

	// Currency denominates the amounts; defaults to "USD" when empty.
	Currency string `json:"currency,omitempty"`

	// Tokens is the usage breakdown that produced Amount (zero for non-model
	// work). Carried for honest accounting and so a cache-aware cost is auditable.
	Tokens TokenUsage `json:"tokens,omitempty"`
}

// TokenUsage is the token breakdown behind a model Cost. Cache fields are
// populated when the backend reports them (Bedrock cache tokens land via
// agenkit v0.86.0).
type TokenUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`     // warm-prefix, billed at cache rate
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"` // cache-write
}

// Total returns all tokens accounted (input + output + cache read + cache
// creation). Useful for sanity checks, not billing (rates differ per class).
func (u TokenUsage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// MeteredCost constructs a fully-billed (provider-metered) cost.
func MeteredCost(amount float64, currency string, tokens TokenUsage) Cost {
	return Cost{Amount: amount, Synthesized: 0, Currency: currency, Tokens: tokens}
}

// SynthesizedCost constructs a fully-modeled (gateway-assigned) cost — the
// local-model case, where the whole amount is synthesized, not billed.
func SynthesizedCost(amount float64, currency string, tokens TokenUsage) Cost {
	return Cost{Amount: amount, Synthesized: amount, Currency: currency, Tokens: tokens}
}

// Metered returns the portion of the cost a provider actually billed (real
// dollars): Amount − Synthesized. This is the quantity to use when a consumer
// needs true spend rather than total modeled+real.
func (c Cost) Metered() float64 { return c.Amount - c.Synthesized }

// HasSynthesized reports whether any part of the cost is modeled rather than
// billed. A consumer that must not conflate the two checks this.
func (c Cost) HasSynthesized() bool { return c.Synthesized > 0 }

// FullySynthesized reports whether the entire cost is modeled (no provider
// billed any of it) — the pure local-model case.
func (c Cost) FullySynthesized() bool { return c.Amount > 0 && c.Synthesized >= c.Amount }

// Denomination returns the cost's currency, defaulting to "USD".
func (c Cost) Denomination() string {
	if c.Currency == "" {
		return "USD"
	}
	return c.Currency
}

// Add sums two costs of the same denomination, accumulating the metered and
// synthesized portions INDEPENDENTLY so the modeled fraction stays recoverable.
// This is the basic accumulation the ledger uses. Mismatched currencies are a
// programming error (conservation arithmetic is only meaningful within one
// denomination) and panic, mirroring Budget.
func (c Cost) Add(o Cost) Cost {
	if c.Denomination() != o.Denomination() {
		panic(fmt.Sprintf("acs: cannot add costs of different currencies (%s + %s)", c.Denomination(), o.Denomination()))
	}
	return Cost{
		Amount:      c.Amount + o.Amount,
		Synthesized: c.Synthesized + o.Synthesized, // modeled portion preserved, never OR'd away
		Currency:    c.Denomination(),
		Tokens: TokenUsage{
			InputTokens:         c.Tokens.InputTokens + o.Tokens.InputTokens,
			OutputTokens:        c.Tokens.OutputTokens + o.Tokens.OutputTokens,
			CacheReadTokens:     c.Tokens.CacheReadTokens + o.Tokens.CacheReadTokens,
			CacheCreationTokens: c.Tokens.CacheCreationTokens + o.Tokens.CacheCreationTokens,
		},
	}
}
