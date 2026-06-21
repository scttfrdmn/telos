// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"

	llmadapter "github.com/scttfrdmn/agenkit-go/adapter/llm"
	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// Backend is the gateway's model abstraction. Both the Bedrock and the local
// backend implement it, and the gateway treats them identically — the whole
// point of the chokepoint. It is a thin seam over agenkit's adapter/llm (its LLM
// interface already gives provider-agnostic Complete); this layer adds (1) a
// stable name for metering/tests and (2) normalized token usage, so the metered
// loop never touches a provider-specific metadata shape.
//
// It is a SEALED interface: its methods are unexported, so only this package's
// constructors (NewBedrockBackend, NewOllamaBackend, NewEchoBackend) can produce
// a Backend. Callers (the host) can hold and pass Backend values and assemble a
// map[string]Backend, but cannot implement their own — backends only come from
// the gateway, which keeps the chokepoint the sole source of metered model access.
type Backend interface {
	// name identifies the backend for metering and tests (e.g. "bedrock",
	// "ollama"). It is NOT exposed to callers as a behavioral difference.
	name() string

	// bills reports whether a real provider bills for this backend's calls. A
	// local backend returns false, which is the signal that the gateway must
	// SYNTHESIZE the cost (no one else will). This is the reason metering lives
	// at the gateway.
	bills() bool

	// complete runs one call and returns the reply plus normalized usage. It
	// hides whether the model is in the cloud or local.
	complete(ctx context.Context, req ModelRequest) (*agenkit.Message, acs.TokenUsage, error)
}

// llmBackend adapts any agenkit adapter/llm.LLM into a gateway backend. Both the
// Bedrock and Ollama adapters are llm.LLM, so this one type serves both — the
// name/bills distinction is all that differs.
type llmBackend struct {
	id        string
	billsReal bool
	llm       llmadapter.LLM
}

func (b *llmBackend) name() string { return b.id }
func (b *llmBackend) bills() bool  { return b.billsReal }

func (b *llmBackend) complete(ctx context.Context, req ModelRequest) (*agenkit.Message, acs.TokenUsage, error) {
	opts := callOptions(req)
	msg, err := b.llm.Complete(ctx, req.Messages, opts...)
	if err != nil {
		return nil, acs.TokenUsage{}, err
	}
	return msg, usageFromMessage(msg), nil
}

// callOptions translates a backend-agnostic ModelRequest into agenkit CallOptions.
func callOptions(req ModelRequest) []llmadapter.CallOption {
	var opts []llmadapter.CallOption
	if req.MaxTokens > 0 {
		opts = append(opts, llmadapter.WithMaxTokens(req.MaxTokens))
	}
	if req.Temperature != nil {
		opts = append(opts, llmadapter.WithTemperature(*req.Temperature))
	}
	return opts
}

// usageFromMessage normalizes the token usage an agenkit adapter records on a
// response into our acs.TokenUsage. As of agenkit v0.86.0 (agenkit#664) the
// adapter package exposes a typed, provider-normalized accessor — including the
// Bedrock prompt-cache token counts added in the same release (agenkit#665) — so
// this is now a thin mapping rather than hand-rolled metadata parsing.
func usageFromMessage(msg *agenkit.Message) acs.TokenUsage {
	u, ok := llmadapter.UsageFromMessage(msg)
	if !ok {
		return acs.TokenUsage{}
	}
	return acs.TokenUsage{
		InputTokens:         u.PromptTokens,
		OutputTokens:        u.CompletionTokens,
		CacheReadTokens:     u.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens,
	}
}
