// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

func newTestGateway(t *testing.T, env float64, backends map[string]Backend) (*Chokepoint, governor.Governor) {
	t.Helper()
	gov := governor.New(acs.Budget{Amount: env, Period: 30 * 24 * time.Hour, Currency: "USD"})
	costs := NewCostModel(CostModelConfig{
		Rates: map[string]Rates{
			"cloud-model": {Input: 3.0, Output: 15.0}, // realistic-ish $/M
		},
		SynthLocal: Rates{Input: 0.05, Output: 0.05},
	})
	gw, err := New(Config{Backends: backends, Governor: gov, Costs: costs, ReservePeriod: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return gw, gov
}

func req(text string, maxTok int) ModelRequest {
	return ModelRequest{Messages: []*agenkit.Message{agenkit.NewMessage("user", text)}, MaxTokens: maxTok}
}

// THE MILESTONE PROPERTY: a caller cannot tell which backend served the call.
// Two backends — one billing (cloud-like), one not (local-like) — return through
// the SAME Invoke with the same response shape. The only differences are in the
// metering record (Cost.Synthesized, Backend tag), which are accounting, not
// behavior the caller branches on.
func TestInvoke_BackendsIndistinguishableToCaller(t *testing.T) {
	cloud := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 100, comp: 50, reply: "answer"})
	local := fakeBackend("ollama", false, &fakeLLM{model: "local-model", prompt: 100, comp: 50, reply: "answer"})
	gw, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": cloud, "ollama": local})

	respCloud, costCloud, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("hello", 64))
	if err != nil {
		t.Fatalf("cloud invoke: %v", err)
	}
	respLocal, costLocal, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "ollama", Model: "local-model"}, req("hello", 64))
	if err != nil {
		t.Fatalf("local invoke: %v", err)
	}

	// Same response surface: both produced a message and the same usage shape.
	if respCloud.Message.ContentString() != respLocal.Message.ContentString() {
		t.Fatal("caller-visible content differs between backends")
	}
	if respCloud.Usage != respLocal.Usage {
		t.Fatalf("caller-visible usage differs: %+v vs %+v", respCloud.Usage, respLocal.Usage)
	}
	// Both produced a non-zero cost — neither metered as free.
	if costCloud.Amount <= 0 || costLocal.Amount <= 0 {
		t.Fatalf("a backend metered as free: cloud=%v local=%v", costCloud.Amount, costLocal.Amount)
	}
}

// Metering happens AT THE GATEWAY for both backends, including a SYNTHESIZED cost
// for the local backend (no provider billed it).
func TestInvoke_LocalCostSynthesizedAtGateway(t *testing.T) {
	cloud := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 1000, comp: 1000})
	local := fakeBackend("ollama", false, &fakeLLM{model: "local-model", prompt: 1000, comp: 1000})
	gw, _ := newTestGateway(t, 10000, map[string]Backend{"bedrock": cloud, "ollama": local})

	_, costCloud, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 1000))
	if err != nil {
		t.Fatal(err)
	}
	_, costLocal, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "ollama", Model: "local-model"}, req("x", 1000))
	if err != nil {
		t.Fatal(err)
	}

	if costCloud.Synthesized {
		t.Fatal("cloud cost must NOT be synthesized (a provider bills it)")
	}
	if !costLocal.Synthesized {
		t.Fatal("local cost MUST be synthesized (no provider bills it) — the reason metering lives at the gateway")
	}
	// Local synthesized cost is non-zero: local work is not free.
	if costLocal.Amount <= 0 {
		t.Fatal("synthesized local cost must be > 0")
	}
}

// The metered loop reserves before invoking and settles the actual after, so the
// grant reflects real spend. With a known rate we can check the exact amount.
func TestInvoke_MeteredAndSettledThroughGovernor(t *testing.T) {
	cloud := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 1_000_000, comp: 1_000_000})
	gw, gov := newTestGateway(t, 1000, map[string]Backend{"bedrock": cloud})

	_, cost, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	// 1M input @ $3/M + 1M output @ $15/M = $18.
	if cost.Amount != 18.0 {
		t.Fatalf("metered cost = %v, want 18.0", cost.Amount)
	}
	// Governor settled the actual: root reservoir down by 18, escrow released.
	if rem := gov.Remaining(governor.RootGrant); rem.Amount != 1000-18 {
		t.Fatalf("root remaining = %v, want %v (actual settled, escrow released)", rem.Amount, 1000.0-18)
	}
}

// Conservation fails closed: if the worst-case escrow can't be reserved, Invoke
// refuses BEFORE calling the backend.
func TestInvoke_FailsClosedWhenGrantExhausted(t *testing.T) {
	called := false
	llm := &fakeLLM{model: "cloud-model", prompt: 10, comp: 10}
	be := &recordingBackend{inner: fakeBackend("bedrock", true, llm), onComplete: func() { called = true }}
	// Tiny envelope: worst-case for maxTokens=1M output @ $15/M = $15 >> $1.
	gw, _ := newTestGateway(t, 1.0, map[string]Backend{"bedrock": be})

	_, _, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 1_000_000))
	if !errors.Is(err, ErrReservationDenied) {
		t.Fatalf("expected ErrReservationDenied, got: %v", err)
	}
	if called {
		t.Fatal("backend was invoked despite failed reservation — must fail closed BEFORE the call")
	}
}

// Cache path: warm-prefix tokens bill at the cache rate, strictly less than the
// same tokens at the full input rate.
func TestInvoke_CacheReadCostsLess(t *testing.T) {
	// Backend A: 1000 input, all cold.
	cold := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 1000, comp: 0})
	// Backend B: same 1000 tokens but reported as cache-read (warm prefix).
	warm := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 0, comp: 0, cacheRead: 1000})
	gwCold, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": cold})
	gwWarm, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": warm})

	_, costCold, _ := gwCold.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 1))
	respWarm, costWarm, _ := gwWarm.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 1))

	if !respWarm.CacheHit {
		t.Fatal("warm-prefix response must report CacheHit")
	}
	if !(costWarm.Amount < costCold.Amount) {
		t.Fatalf("warm-prefix cost (%v) must be less than cold cost (%v)", costWarm.Amount, costCold.Amount)
	}
}

// Bedrock-style int32 usage and Ollama-style int usage normalize identically.
func TestUsageNormalization_AcrossProviderTypes(t *testing.T) {
	int32Backend := fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model", prompt: 100, comp: 50, intUsage: false})
	intBackend := fakeBackend("ollama", false, &fakeLLM{model: "local-model", prompt: 100, comp: 50, intUsage: true})
	gw, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": int32Backend, "ollama": intBackend})

	r1, _, _ := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "bedrock", Model: "cloud-model"}, req("x", 64))
	r2, _, _ := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "ollama", Model: "local-model"}, req("x", 64))
	if r1.Usage != r2.Usage {
		t.Fatalf("int32 vs int usage normalized differently: %+v vs %+v", r1.Usage, r2.Usage)
	}
	if r1.Usage.InputTokens != 100 || r1.Usage.OutputTokens != 50 {
		t.Fatalf("usage not normalized correctly: %+v", r1.Usage)
	}
}

func TestInvoke_UnknownBackend(t *testing.T) {
	gw, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model"})})
	_, _, err := gw.Invoke(context.Background(), acs.ModelBinding{Provider: "nope", Model: "x"}, req("x", 8))
	if !errors.Is(err, ErrNoBackend) {
		t.Fatalf("expected ErrNoBackend, got: %v", err)
	}
}

func TestRunWork_StubbedUntilM6(t *testing.T) {
	gw, _ := newTestGateway(t, 1000, map[string]Backend{"bedrock": fakeBackend("bedrock", true, &fakeLLM{model: "cloud-model"})})
	_, _, err := gw.RunWork(context.Background(), WorkloadSpec{})
	if !errors.Is(err, ErrComputePathNotImplemented) {
		t.Fatalf("expected ErrComputePathNotImplemented, got: %v", err)
	}
}

// recordingBackend wraps a backend to observe whether complete() was called.
type recordingBackend struct {
	inner      Backend
	onComplete func()
}

func (r *recordingBackend) name() string { return r.inner.name() }
func (r *recordingBackend) bills() bool  { return r.inner.bills() }
func (r *recordingBackend) complete(ctx context.Context, req ModelRequest) (*agenkit.Message, acs.TokenUsage, error) {
	if r.onComplete != nil {
		r.onComplete()
	}
	return r.inner.complete(ctx, req)
}
