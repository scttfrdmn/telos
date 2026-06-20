// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// stubTool is a deterministic no-op tool. A React node's agenkit constructor
// requires at least one tool; in M0 tools are not wired to anything real (tool
// resolution and policy-gating are the binder's job, M1+), so each declared
// acs.ToolRef becomes a stubTool that echoes its parameters.
type stubTool struct {
	name string
}

func newStubTool(ref acs.ToolRef) *stubTool { return &stubTool{name: ref.Name} }

func (t *stubTool) Name() string        { return t.name }
func (t *stubTool) Description() string { return "stub tool (" + t.name + "): no-op in M0" }

func (t *stubTool) Execute(ctx context.Context, params map[string]any) (*agenkit.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return agenkit.NewToolResult(map[string]any{"tool": t.name, "params": params, "stub": true}), nil
}
