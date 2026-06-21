// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// echoBackend is a deterministic, dependency-free backend for offline operation
// and the local-acceptance check: no AWS creds, no running model server. It
// echoes a canned reply and reports synthetic token usage so the metered loop
// runs end to end (estimate → reserve → invoke → meter → settle) with no network.
//
// It is treated as NON-billing (bills()==false), so its cost is SYNTHESIZED at
// the gateway exactly like a real local model — which is the property the host
// wants to exercise offline.
type echoBackend struct {
	id string
}

// NewEchoBackend returns an offline echo backend under the given name. Used by
// the host as a fallback when neither Bedrock nor a local model is configured,
// so `make accept` works with no external dependencies.
func NewEchoBackend(name string) Backend {
	if name == "" {
		name = "echo"
	}
	return &echoBackend{id: name}
}

func (e *echoBackend) name() string { return e.id }
func (e *echoBackend) bills() bool  { return false }

func (e *echoBackend) complete(ctx context.Context, req ModelRequest) (*agenkit.Message, acs.TokenUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, acs.TokenUsage{}, err
	}
	prompt := ""
	if n := len(req.Messages); n > 0 && req.Messages[n-1] != nil {
		prompt = req.Messages[n-1].ContentString()
	}
	reply := fmt.Sprintf("[echo backend] %s", truncateForEcho(prompt, 200))
	msg := agenkit.NewMessage("agent", reply)

	// Synthetic but plausible usage so metering has something to price.
	in := estimateInputTokens(req)
	out := len(reply) / 4
	if out < 1 {
		out = 1
	}
	return msg, acs.TokenUsage{InputTokens: in, OutputTokens: out}, nil
}

func truncateForEcho(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
