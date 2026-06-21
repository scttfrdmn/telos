// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/governor"
)

// Chokepoint is the concrete Gateway: the one place model work is routed,
// escrowed, metered, and settled (invariant 5). It is backend-agnostic — a
// caller cannot tell from Invoke whether the model ran on Bedrock or locally.
type Chokepoint struct {
	backends map[string]Backend // keyed by provider name (binding.Provider)
	gov      governor.Governor
	costs    *CostModel

	// defaultMaxTokens bounds output when a request omits MaxTokens, so the
	// worst-case escrow is always finite.
	defaultMaxTokens int
	// reservePeriod is the slice of the parent's clock a single call reserves
	// over. A call is short, so this is small; it keeps reservations rate-shaped
	// (invariant 4) rather than letting a call claim the whole grant period.
	reservePeriod time.Duration

	// launcher / pricer back the compute (RunWork) path (§8). Both nil = compute
	// disabled (RunWork returns ErrComputePathNotImplemented). They are
	// compute.Launcher / compute.Pricer INTERFACES — the concrete AWS impl lives in
	// the separate substrate/sporehost module, so the gateway never imports the
	// AWS-SDK tree (the core stays light; dep isolation).
	launcher compute.Launcher
	pricer   compute.Pricer
	// computeCurrency denominates synthesized compute cost (default USD).
	computeCurrency string
}

// Config configures a Chokepoint.
type Config struct {
	// Backends keyed by provider name (must match router ModelBinding.Provider).
	Backends map[string]Backend
	// Governor escrows and settles each call. Required.
	Governor governor.Governor
	// Costs turns usage into cost (real + synthesized). Required.
	Costs *CostModel
	// DefaultMaxTokens bounds output when unset on a request (default 1024).
	DefaultMaxTokens int
	// ReservePeriod is the per-call reservation horizon (default 1 minute).
	ReservePeriod time.Duration

	// Launcher / Pricer enable the compute (RunWork) path. Both nil = compute
	// disabled. Supplied by the host wiring the substrate/sporehost module.
	Launcher compute.Launcher
	Pricer   compute.Pricer
	// ComputeCurrency denominates synthesized compute cost (default USD).
	ComputeCurrency string
}

// New builds a Chokepoint.
func New(cfg Config) (*Chokepoint, error) {
	if cfg.Governor == nil {
		return nil, errors.New("gateway: governor is required")
	}
	if cfg.Costs == nil {
		return nil, errors.New("gateway: cost model is required")
	}
	if len(cfg.Backends) == 0 {
		return nil, errors.New("gateway: at least one backend is required")
	}
	maxTok := cfg.DefaultMaxTokens
	if maxTok <= 0 {
		maxTok = 1024
	}
	period := cfg.ReservePeriod
	if period <= 0 {
		period = time.Minute
	}
	cur := cfg.ComputeCurrency
	if cur == "" {
		cur = "USD"
	}
	return &Chokepoint{
		backends:         cfg.Backends,
		gov:              cfg.Governor,
		costs:            cfg.Costs,
		defaultMaxTokens: maxTok,
		reservePeriod:    period,
		launcher:         cfg.Launcher,
		pricer:           cfg.Pricer,
		computeCurrency:  cur,
	}, nil
}

// RegisterBackend adds or replaces a backend under a provider name. Used by the
// host to assemble the backend set (Bedrock, Ollama, or fakes for offline runs).
func (c *Chokepoint) RegisterBackend(provider string, b Backend) {
	c.backends[provider] = b
}

// Invoke runs the metered loop for one model call (architecture §6):
//
//	estimate worst-case (output ← max_tokens)
//	  → governor.Reserve(escrow)        // fails closed on conservation breach
//	  → route to backend (by binding.Provider)
//	  → invoke
//	  → meter ACTUAL cost               // synthesized for local; metered HERE
//	  → governor.Settle(actual)
//
// The caller cannot tell which backend served the call: the return shape is
// identical, and metering happens here regardless of whether a provider billed.
func (c *Chokepoint) Invoke(ctx context.Context, binding acs.ModelBinding, req ModelRequest) (ModelResponse, acs.Cost, error) {
	if err := ctx.Err(); err != nil {
		return ModelResponse{}, acs.Cost{}, err
	}
	be, ok := c.backends[binding.Provider]
	if !ok {
		return ModelResponse{}, acs.Cost{}, fmt.Errorf("%w: %q", ErrNoBackend, binding.Provider)
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.defaultMaxTokens
		req.MaxTokens = maxTokens
	}

	// 1. Estimate worst-case cost: input as counted, output at the ceiling.
	inputTokens := estimateInputTokens(req)
	worstCase := c.costs.Estimate(binding.Model, be.bills(), inputTokens, maxTokens)

	// 2. Reserve the worst-case against the parent grant. FAILS CLOSED.
	parent := ParentGrant(ctx)
	grant, err := c.gov.Reserve(ctx, parent, acs.BudgetRequest{
		Amount: worstCase.Amount,
		Period: c.reservePeriod,
	})
	if err != nil {
		if errors.Is(err, governor.ErrConservation) {
			return ModelResponse{}, acs.Cost{}, fmt.Errorf("%w: %v", ErrReservationDenied, err)
		}
		return ModelResponse{}, acs.Cost{}, fmt.Errorf("gateway: reserve: %w", err)
	}
	gid := governor.GrantID(grant.GrantID)

	// 3. Invoke the backend.
	msg, usage, err := be.complete(ctx, req)
	if err != nil {
		// The call didn't produce billable work: release the escrow, no charge.
		_ = c.gov.Release(ctx, gid)
		return ModelResponse{}, acs.Cost{}, fmt.Errorf("gateway: backend %q: %w", be.name(), err)
	}

	// 4. Meter ACTUAL cost from real usage. For a local backend this is where
	//    cost is SYNTHESIZED — no provider billed, but the grant must still see
	//    the spend. This is the whole reason metering lives at the gateway.
	cacheHit := usage.CacheReadTokens > 0
	actual := c.costs.Price(binding.Model, be.bills(), usage)

	// 5. Settle the actual against the grant (releases escrow, debits actual).
	//    M1 settles unconditionally (ExitDone); acceptance-gated surplus is M2.
	if err := c.gov.Settle(ctx, gid, actual, governor.Outcome{Exit: governor.ExitDone}); err != nil {
		return ModelResponse{}, acs.Cost{}, fmt.Errorf("gateway: settle: %w", err)
	}

	return ModelResponse{
		Message:  msg,
		Usage:    usage,
		CacheHit: cacheHit,
		Backend:  be.name(),
	}, actual, nil
}

// RunWork — the metered compute path — is implemented in runwork.go.

// estimateInputTokens approximates prompt tokens from message content. M1 uses
// the standard ~4-chars-per-token heuristic — good enough for a worst-case
// ESCROW estimate (the actual is metered from real usage afterward). A real
// tokenizer is a later refinement and does not change the metered-loop shape.
func estimateInputTokens(req ModelRequest) int {
	chars := 0
	for _, m := range req.Messages {
		if m != nil {
			chars += len(m.ContentString())
		}
	}
	tokens := chars / 4
	if tokens < 1 && chars > 0 {
		tokens = 1
	}
	return tokens
}

var _ Gateway = (*Chokepoint)(nil)
