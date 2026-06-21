// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// These smoke tests hit REAL backends and are skipped unless explicitly enabled,
// so the default `go test ./...` stays hermetic and offline. They prove the same
// Invoke code path works against the real Bedrock and Ollama adapters — not just
// fakes.
//
//	TELOS_SMOKE_BEDROCK=1   (with AWS creds + region) → real Bedrock
//	TELOS_SMOKE_OLLAMA=1    (with a running ollama)   → real Ollama
//	TELOS_OLLAMA_MODEL=...  (optional; default llama3.1:8b)

func smokeGateway(t *testing.T, provider string, be Backend, model string) *Chokepoint {
	t.Helper()
	gov := governor.New(acs.Budget{Amount: 100, Period: 24 * time.Hour, Currency: "USD"})
	costs := NewCostModel(CostModelConfig{
		Rates: map[string]Rates{model: {Input: 3.0, Output: 15.0}},
	})
	gw, err := New(Config{
		Backends: map[string]Backend{provider: be},
		Governor: gov, Costs: costs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gw
}

func TestSmoke_Bedrock(t *testing.T) {
	if os.Getenv("TELOS_SMOKE_BEDROCK") != "1" {
		t.Skip("set TELOS_SMOKE_BEDROCK=1 (with AWS creds) to run")
	}
	model := "anthropic.claude-3-5-haiku-20241022-v1:0"
	region := os.Getenv("AWS_REGION")
	be, err := NewBedrockBackend(context.Background(), model, region)
	if err != nil {
		t.Fatalf("bedrock backend: %v", err)
	}
	gw := smokeGateway(t, "bedrock", be, model)
	resp, cost, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: model},
		req("Reply with the single word: pong.", 32))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if resp.Message.ContentString() == "" {
		t.Fatal("empty reply")
	}
	if cost.HasSynthesized() {
		t.Fatal("Bedrock cost must not be synthesized")
	}
	if cost.Amount <= 0 {
		t.Fatal("Bedrock cost should be > 0")
	}
	t.Logf("bedrock reply=%q cost=$%.6f usage=%+v", resp.Message.ContentString(), cost.Amount, resp.Usage)
}

func TestSmoke_Ollama(t *testing.T) {
	if os.Getenv("TELOS_SMOKE_OLLAMA") != "1" {
		t.Skip("set TELOS_SMOKE_OLLAMA=1 (with a running ollama) to run")
	}
	model := os.Getenv("TELOS_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	be := NewOllamaBackend(model, os.Getenv("OLLAMA_HOST"))
	gw := smokeGateway(t, "ollama", be, model)
	resp, cost, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "ollama", Model: model},
		req("Reply with the single word: pong.", 32))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if resp.Message.ContentString() == "" {
		t.Fatal("empty reply")
	}
	// The whole point: a local model bills nothing, so the gateway synthesized
	// its cost — and it is non-zero.
	if !cost.FullySynthesized() {
		t.Fatal("Ollama cost MUST be synthesized at the gateway")
	}
	if cost.Amount <= 0 {
		t.Fatal("synthesized local cost should be > 0")
	}
	t.Logf("ollama reply=%q synthesized_cost=$%.6f usage=%+v", resp.Message.ContentString(), cost.Amount, resp.Usage)
}

// Sanity: the agenkit message helper is importable here (keeps the import used
// even if both smoke tests are skipped).
var _ = agenkit.NewMessage
