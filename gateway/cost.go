// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import "github.com/scttfrdmn/telos/acs"

// CostModel turns token usage into an acs.Cost. There are two cost sources, kept
// rigorously distinct because burnrate paces the grant partly on these numbers
// (architecture §9):
//
//   - METERED cost — real provider rates (e.g. Bedrock). A provider bills it.
//   - SYNTHESIZED cost — a MODELED price the gateway assigns to local work that
//     no provider bills. This is a POLICY FORMULA, not a measurement: it is named
//     (a SynthesisStrategy), configurable, and the resulting acs.Cost records the
//     synthesized amount separately so it never silently blends with metered spend.
//
// Rates are per-MILLION tokens, kept as input/output/cache triples so warm-prefix
// tokens bill at the cache rate (caching savings bank unconditionally — §9).
type CostModel struct {
	rates    map[string]Rates // real provider rates by model id
	synth    SynthesisStrategy
	currency string
}

// Rates is a per-million-token price triple.
type Rates struct {
	Input     float64 // $ per 1M input tokens
	Output    float64 // $ per 1M output tokens
	CacheRead float64 // $ per 1M cache-read (warm-prefix) tokens
}

// SynthesisStrategy is the explicit, named formula that assigns a MODELED cost to
// local (non-billing) work. It is an interface, not a constant, so the synthesis
// policy is visible and swappable — and so a reader can never mistake the modeled
// number for a measured one. (The exact "right" local price is a later refinement;
// what matters now is that the formula is explicit and the output is tagged.)
type SynthesisStrategy interface {
	// Name identifies the strategy for provenance/observability.
	Name() string
	// Synthesize returns the MODELED rate to apply to local usage of a model.
	Synthesize(model string, usage acs.TokenUsage) Rates
}

// FlatRateSynthesis is the default synthesis policy: a flat per-million-token
// rate regardless of model or usage. It is deliberately simple and small but
// non-zero — local work must cost SOMETHING so the grant sees it — and explicitly
// a placeholder for a future amortized-hardware model.
type FlatRateSynthesis struct {
	Rate Rates
}

// Name implements SynthesisStrategy.
func (f FlatRateSynthesis) Name() string { return "flat-rate" }

// Synthesize implements SynthesisStrategy.
func (f FlatRateSynthesis) Synthesize(model string, usage acs.TokenUsage) Rates {
	return f.Rate
}

// DefaultLocalSynthRate is the flat rate used when no SynthesisStrategy is given.
// Small but non-zero by design: the point is that local work is METERED (the grant
// sees its modeled cost), not that the number is precise.
var DefaultLocalSynthRate = Rates{Input: 0.05, Output: 0.05, CacheRead: 0.005}

// CostModelConfig configures a CostModel.
type CostModelConfig struct {
	// Rates by model id (real provider rates, e.g. Bedrock).
	Rates map[string]Rates
	// Synthesis is the named formula for MODELED local cost. If nil, a
	// FlatRateSynthesis at DefaultLocalSynthRate is used.
	Synthesis SynthesisStrategy
	// Currency for produced costs; defaults to USD.
	Currency string
}

// NewCostModel builds a cost model. Unknown models fall back to a small default
// so an un-priced model never meters as free.
func NewCostModel(cfg CostModelConfig) *CostModel {
	synth := cfg.Synthesis
	if synth == nil {
		synth = FlatRateSynthesis{Rate: withDefaultCacheRate(DefaultLocalSynthRate)}
	}
	cur := cfg.Currency
	if cur == "" {
		cur = "USD"
	}
	rates := make(map[string]Rates, len(cfg.Rates))
	for k, v := range cfg.Rates {
		rates[k] = withDefaultCacheRate(v)
	}
	return &CostModel{rates: rates, synth: synth, currency: cur}
}

// SynthesisName reports the active synthesis strategy's name (for observability).
func (m *CostModel) SynthesisName() string { return m.synth.Name() }

// withDefaultCacheRate fills a missing cache rate at 10% of the input rate — the
// conventional warm-prefix discount — so cache-awareness works even when a rate
// triple only specifies input/output.
func withDefaultCacheRate(r Rates) Rates {
	if r.CacheRead == 0 {
		r.CacheRead = r.Input * 0.10
	}
	return r
}

// unknownModelRate is the floor for a model with no configured price: small but
// non-zero, so un-priced real spend is still visible.
var unknownModelRate = Rates{Input: 0.5, Output: 1.5, CacheRead: 0.05}

// Price computes the cost of usage for a model on a backend. When the backend
// does not bill (local), the cost is MODELED via the synthesis strategy and the
// returned acs.Cost records the whole amount as synthesized. Otherwise the
// model's real rate (or the unknown-model fallback) applies and the cost is
// fully metered (Synthesized == 0).
func (m *CostModel) Price(model string, billsReal bool, usage acs.TokenUsage) acs.Cost {
	if !billsReal {
		r := withDefaultCacheRate(m.synth.Synthesize(model, usage))
		amount := applyRates(r, usage)
		return acs.SynthesizedCost(amount, m.currency, usage)
	}

	r, ok := m.rates[model]
	if !ok {
		r = unknownModelRate
	}
	amount := applyRates(r, usage)
	return acs.MeteredCost(amount, m.currency, usage)
}

// applyRates is the cache-aware token→dollars formula shared by metered and
// synthesized pricing. Warm-prefix (cache-read) input tokens bill at the cache
// rate; the rest of the input (including cache-creation, which costs normal
// input) bills at the full input rate.
func applyRates(r Rates, usage acs.TokenUsage) float64 {
	const perMillion = 1_000_000.0
	fullInput := usage.InputTokens + usage.CacheCreationTokens
	return float64(fullInput)/perMillion*r.Input +
		float64(usage.OutputTokens)/perMillion*r.Output +
		float64(usage.CacheReadTokens)/perMillion*r.CacheRead
}

// Estimate computes a WORST-CASE cost for escrow before a call runs: input tokens
// as given, output tokens assumed at the max (maxTokens). This is what the metered
// loop reserves; the actual (usually lower) is settled afterward.
func (m *CostModel) Estimate(model string, billsReal bool, inputTokens, maxTokens int) acs.Cost {
	return m.Price(model, billsReal, acs.TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: maxTokens,
	})
}
