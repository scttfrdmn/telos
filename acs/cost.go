// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import "fmt"

// Cost is the metered cost of one unit of work — a model call or (later) a
// synthesized computation. It lives in acs alongside Budget because both are
// money types, and because the gateway (which produces Cost) and the governor
// (which settles it) must share the type without importing each other.
//
// A Cost is a point amount in a currency, NOT a grant: it is what something
// actually cost, settled against a grant's reservoir. Unlike Budget it carries
// no period — a single call's cost is an instantaneous draw; the RATE lives on
// the Budget it settles against (invariant 4 keeps the clock on the grant, not
// on each charge).
type Cost struct {
	// Amount is the cost in Currency units.
	Amount float64 `json:"amount"`

	// Currency denominates Amount; defaults to "USD" when empty.
	Currency string `json:"currency,omitempty"`

	// Tokens is the usage breakdown that produced Amount (zero for non-model
	// work). Carried for honest accounting and so a cache-aware cost is auditable.
	Tokens TokenUsage `json:"tokens,omitempty"`

	// Synthesized marks a cost the gateway ASSIGNED rather than one a provider
	// billed. Local-model work is the canonical case: no provider bills you, so
	// the gateway synthesizes the cost — this is the whole reason metering lives
	// at the gateway. Honest accounting must be able to tell real spend from
	// synthesized spend (e.g. for grant reporting).
	Synthesized bool `json:"synthesized,omitempty"`
}

// TokenUsage is the token breakdown behind a model Cost. Cache fields are
// best-effort: zero when the backend does not report them (see the Bedrock
// cache-token gap, agenkit#665).
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

// Denomination returns the cost's currency, defaulting to "USD".
func (c Cost) Denomination() string {
	if c.Currency == "" {
		return "USD"
	}
	return c.Currency
}

// Add sums two costs of the same denomination. It is the basic accumulation the
// ledger uses. Mismatched currencies are a programming error (conservation
// arithmetic is only meaningful within one denomination) and panic, mirroring
// how Budget treats denomination.
func (c Cost) Add(o Cost) Cost {
	if c.Denomination() != o.Denomination() {
		panic(fmt.Sprintf("acs: cannot add costs of different currencies (%s + %s)", c.Denomination(), o.Denomination()))
	}
	return Cost{
		Amount:   c.Amount + o.Amount,
		Currency: c.Denomination(),
		Tokens: TokenUsage{
			InputTokens:         c.Tokens.InputTokens + o.Tokens.InputTokens,
			OutputTokens:        c.Tokens.OutputTokens + o.Tokens.OutputTokens,
			CacheReadTokens:     c.Tokens.CacheReadTokens + o.Tokens.CacheReadTokens,
			CacheCreationTokens: c.Tokens.CacheCreationTokens + o.Tokens.CacheCreationTokens,
		},
		// A sum is synthesized if either part was — mixed accounting is flagged
		// conservatively so synthesized spend never hides inside a real total.
		Synthesized: c.Synthesized || o.Synthesized,
	}
}
