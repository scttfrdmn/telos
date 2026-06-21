// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import "github.com/scttfrdmn/telos/acs"

// CostModel turns token usage into an acs.Cost. There are two cost sources —
// real provider rates (Bedrock) and SYNTHESIZED rates (local, where no one
// bills) — but one Cost type and one cache-aware formula.
//
// Rates are per-MILLION tokens (the conventional unit), kept as input/output/
// cache triples so warm-prefix tokens bill at the cache rate. Caching savings
// bank unconditionally (architecture §9), so the cache rate is applied whenever
// the backend reports cache-read tokens.
type CostModel struct {
	// rates maps a model id to its per-million-token rates.
	rates map[string]Rates
	// synthLocal is the rate applied to a backend that does not bill (local).
	// It makes local compute cost SOMETHING so it cannot meter as free.
	synthLocal Rates
	currency   string
}

// Rates is a per-million-token price triple. CacheRead defaults to a fraction of
// Input when unset via the constructor helper.
type Rates struct {
	Input     float64 // $ per 1M input tokens
	Output    float64 // $ per 1M output tokens
	CacheRead float64 // $ per 1M cache-read (warm-prefix) tokens
}

// CostModelConfig configures a CostModel.
type CostModelConfig struct {
	// Rates by model id (real provider rates, e.g. Bedrock).
	Rates map[string]Rates
	// SynthLocal is the synthesized rate for non-billing (local) backends. If
	// zero, DefaultLocalSynthRate is used — a nominal compute price so local
	// work is metered, not free.
	SynthLocal Rates
	// Currency for produced costs; defaults to USD.
	Currency string
}

// DefaultLocalSynthRate is a nominal synthesized price for local model tokens.
// It is deliberately small but non-zero: the point is that local work is METERED
// (so the grant sees its cost), not that the number is precise. The real
// amortized-hardware rate is a later refinement.
var DefaultLocalSynthRate = Rates{Input: 0.05, Output: 0.05, CacheRead: 0.005}

// NewCostModel builds a cost model. Unknown models fall back to a small default
// so an un-priced model never meters as free.
func NewCostModel(cfg CostModelConfig) *CostModel {
	synth := cfg.SynthLocal
	if synth == (Rates{}) {
		synth = DefaultLocalSynthRate
	}
	cur := cfg.Currency
	if cur == "" {
		cur = "USD"
	}
	rates := make(map[string]Rates, len(cfg.Rates))
	for k, v := range cfg.Rates {
		rates[k] = withDefaultCacheRate(v)
	}
	return &CostModel{rates: rates, synthLocal: withDefaultCacheRate(synth), currency: cur}
}

// withDefaultCacheRate fills a missing cache rate at 10% of the input rate —
// the conventional warm-prefix discount — so cache-awareness works even when a
// rate triple only specifies input/output.
func withDefaultCacheRate(r Rates) Rates {
	if r.CacheRead == 0 {
		r.CacheRead = r.Input * 0.10
	}
	return r
}

// unknownModelRate is the floor for a model with no configured price: small but
// non-zero, so un-priced real spend is still visible.
var unknownModelRate = Rates{Input: 0.5, Output: 1.5, CacheRead: 0.05}

// Price computes the cost of usage for a model on a backend. If the backend does
// not bill (local), the synthesized local rate is used and Cost.Synthesized is
// set. Otherwise the model's real rate (or the unknown-model fallback) applies.
func (m *CostModel) Price(model string, billsReal bool, usage acs.TokenUsage) acs.Cost {
	var r Rates
	synthesized := !billsReal
	if synthesized {
		r = m.synthLocal
	} else if rr, ok := m.rates[model]; ok {
		r = rr
	} else {
		r = unknownModelRate
	}

	const perMillion = 1_000_000.0
	// Warm-prefix (cache-read) input tokens bill at the cache rate; the rest of
	// the input bills at the full input rate. Cache-creation tokens bill at full
	// input rate (writing the cache costs normal input).
	fullInput := usage.InputTokens + usage.CacheCreationTokens
	amount := float64(fullInput)/perMillion*r.Input +
		float64(usage.OutputTokens)/perMillion*r.Output +
		float64(usage.CacheReadTokens)/perMillion*r.CacheRead

	return acs.Cost{
		Amount:      amount,
		Currency:    m.currency,
		Tokens:      usage,
		Synthesized: synthesized,
	}
}

// Estimate computes a WORST-CASE cost for escrow before a call runs: input
// tokens as given, output tokens assumed at the max (maxTokens). This is what
// the metered loop reserves; the actual (usually lower) is settled afterward.
func (m *CostModel) Estimate(model string, billsReal bool, inputTokens, maxTokens int) acs.Cost {
	return m.Price(model, billsReal, acs.TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: maxTokens,
	})
}
