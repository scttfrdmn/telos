// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"log/slog"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/governor"
	"github.com/scttfrdmn/telos/router"
)

// defaultEnvelopePeriod is the nominal grant clock used when a caller supplies an
// amount with no period. A grant is amount-over-period (invariant 4); this keeps
// a missing clock from collapsing the grant into a bare total.
func defaultEnvelopePeriod() time.Duration { return 24 * time.Hour }

// DepsConfig describes which model backends to wire behind the gateway. Any
// combination may be enabled; if NONE are, the gateway falls back to an offline
// echo backend so the host runs (and `make accept` passes) with no AWS creds and
// no local model server.
type DepsConfig struct {
	// Envelope is the run's grant the governor conserves against (amount + period).
	Envelope acs.Budget

	// Bedrock, when non-nil, wires a real Bedrock backend.
	Bedrock *BedrockConfig
	// Ollama, when non-nil, wires a real local Ollama backend.
	Ollama *OllamaConfig
}

// BedrockConfig configures the cloud backend.
type BedrockConfig struct {
	ModelID string
	Region  string
}

// OllamaConfig configures the local backend.
type OllamaConfig struct {
	Model   string
	BaseURL string
}

// NewDeps assembles the gateway (with its backends), governor, and router into a
// Deps the host wires into Reason leaves. The router table is built to match the
// backends that are actually available, so an unbound capability constraint
// always resolves to a real provider.
//
// Offline by default: with no Bedrock and no Ollama configured, it wires a single
// echo backend and points every tier at it — the host then runs end to end with
// no external dependency, exercising the full metered loop (with synthesized
// cost) against a local stand-in.
func NewDeps(ctx context.Context, cfg DepsConfig, log *slog.Logger) (*Deps, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Envelope.Period <= 0 {
		// A grant must be amount-over-period (invariant 4). Default to a nominal
		// clock if the caller did not set one.
		cfg.Envelope = acs.Budget{Amount: cfg.Envelope.Amount, Period: defaultEnvelopePeriod(), Currency: cfg.Envelope.Denomination()}
	}

	backends := map[string]gateway.Backend{}
	var entries []router.Entry

	if cfg.Bedrock != nil {
		be, err := gateway.NewBedrockBackend(ctx, cfg.Bedrock.ModelID, cfg.Bedrock.Region)
		if err != nil {
			return nil, err
		}
		backends["bedrock"] = be
		// Bedrock serves mid/frontier tiers.
		entries = append(entries,
			router.Entry{Tier: acs.TierMid, Provider: "bedrock", Model: cfg.Bedrock.ModelID},
			router.Entry{Tier: acs.TierFrontier, Provider: "bedrock", Model: cfg.Bedrock.ModelID},
		)
		log.Info("wired bedrock backend", "model", cfg.Bedrock.ModelID, "region", cfg.Bedrock.Region)
	}

	if cfg.Ollama != nil {
		backends["ollama"] = gateway.NewOllamaBackend(cfg.Ollama.Model, cfg.Ollama.BaseURL)
		// Ollama serves the cheap tier (cascade floor).
		entries = append([]router.Entry{
			{Tier: acs.TierCheap, Provider: "ollama", Model: cfg.Ollama.Model},
		}, entries...)
		log.Info("wired ollama backend", "model", cfg.Ollama.Model)
	}

	if len(backends) == 0 {
		// Offline fallback: one echo backend serving every tier.
		const echoModel = "echo"
		backends["echo"] = gateway.NewEchoBackend("echo")
		entries = []router.Entry{
			{Tier: acs.TierCheap, Provider: "echo", Model: echoModel},
			{Tier: acs.TierMid, Provider: "echo", Model: echoModel},
			{Tier: acs.TierFrontier, Provider: "echo", Model: echoModel},
		}
		log.Info("no model backend configured — using offline echo backend (synthesized cost)")
	}

	rtr, err := router.NewTable(entries)
	if err != nil {
		return nil, err
	}

	gov := governor.New(cfg.Envelope)
	costs := gateway.NewCostModel(gateway.CostModelConfig{
		Rates:    defaultBedrockRates(),
		Currency: cfg.Envelope.Denomination(),
	})
	gw, err := gateway.New(gateway.Config{
		Backends: backends,
		Governor: gov,
		Costs:    costs,
	})
	if err != nil {
		return nil, err
	}

	return &Deps{Gateway: gw, Router: rtr, Governor: gov, LiveAcceptance: true}, nil
}

// defaultBedrockRates seeds a few well-known Bedrock model rates ($/M tokens).
// Unknown models fall back to the gateway cost model's built-in floor.
func defaultBedrockRates() map[string]gateway.Rates {
	return map[string]gateway.Rates{
		"anthropic.claude-3-5-haiku-20241022-v1:0":  {Input: 0.80, Output: 4.00},
		"anthropic.claude-3-5-sonnet-20241022-v2:0": {Input: 3.00, Output: 15.00},
	}
}
