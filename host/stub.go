// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// stubAgent is a deterministic leaf worker. M0 proves COMPOSITION, not model
// integration: no leaf calls a model (model routing is the gateway's job, M1).
// A stub echoes a structured, deterministic trace of the node it stands for so
// an invocation result visibly shows which graph was instantiated and run.
type stubAgent struct {
	nodeID acs.NodeID
	kind   acs.NodeKind
	role   string
	tier   acs.Tier
}

func newStubAgent(n *acs.Node) *stubAgent {
	return &stubAgent{nodeID: n.ID, kind: n.Kind, role: n.Role, tier: n.Model.Tier}
}

// Process returns a deterministic acknowledgement of the input, tagged with the
// node it represents. Determinism matters: the local acceptance check asserts on
// the output, and nothing here may depend on a model or wall-clock.
func (a *stubAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in := ""
	if message != nil {
		in = message.ContentString()
	}
	content := fmt.Sprintf("[%s/%s] %s | in=%q", a.nodeID, a.kind, a.role, truncate(in, 120))
	out := agenkit.NewMessage("agent", content)
	out.WithMetadata("telos.node", string(a.nodeID))
	out.WithMetadata("telos.kind", string(a.kind))
	if a.tier != "" {
		out.WithMetadata("telos.tier", string(a.tier))
	}
	return out, nil
}

func (a *stubAgent) Name() string { return string(a.nodeID) }

func (a *stubAgent) Capabilities() []string {
	return []string{"stub", string(a.kind)}
}

func (a *stubAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}

// reactInnerAgent is the reasoning agent inside a React node's loop. The React
// pattern drives a Thought/Action/Observation loop and parses the agent's text
// for "Final Answer:". A real reasoner would emit tool calls; the M0 stub
// terminates immediately with a deterministic final answer so the loop is
// exercised (one pass) without a model and without burning steps.
type reactInnerAgent struct {
	nodeID acs.NodeID
	role   string
}

func (a *reactInnerAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Returning a Final Answer on the first step makes the ReAct loop terminate
	// deterministically via StopReasonFinalAnswer.
	content := fmt.Sprintf("Thought: scoping stub for %s needs no tools\nFinal Answer: [%s] scoped (stub)", a.nodeID, a.nodeID)
	return agenkit.NewMessage("agent", content), nil
}

func (a *reactInnerAgent) Name() string           { return string(a.nodeID) + ".reasoner" }
func (a *reactInnerAgent) Capabilities() []string { return []string{"stub", "react-reasoner"} }
func (a *reactInnerAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
