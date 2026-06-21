// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/router"
)

// gatewayAgent is a leaf agent that obtains its model output by invoking THROUGH
// the gateway — never by talking to a model directly (invariant 5). Its model is
// chosen by resolving the node's capability constraint via the router, so the
// agent code names no model and cannot tell whether Bedrock or a local model
// served the call (that is the gateway's guarantee, M1).
//
// This is the one wired path M1 delivers: it proves an agent can run real model
// work through the chokepoint. Wiring EVERY leaf with per-node bindings + budget
// is the binder's job at M3.
type gatewayAgent struct {
	node      *acs.Node
	gw        gateway.Gateway
	rtr       router.Router
	maxTokens int
}

func newGatewayAgent(n *acs.Node, gw gateway.Gateway, rtr router.Router) *gatewayAgent {
	return &gatewayAgent{node: n, gw: gw, rtr: rtr, maxTokens: 512}
}

func (a *gatewayAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Resolve the node's capability constraint to a concrete binding. The agent
	// names no model; the router (the only place names live) picks one.
	binding, err := a.rtr.Select(ctx, a.node.Model, a.budgetCeiling())
	if err != nil {
		return nil, fmt.Errorf("host: %s: resolve model: %w", a.node.ID, err)
	}

	prompt := a.node.Role
	if message != nil {
		if c := message.ContentString(); c != "" {
			prompt = c
		}
	}

	// Invoke through the gateway. The agent cannot tell which backend ran; it
	// just gets a message and a metered cost (cost is recorded at the gateway).
	resp, cost, err := a.gw.Invoke(ctx, binding, gateway.ModelRequest{
		Messages:  []*agenkit.Message{agenkit.NewMessage("user", prompt)},
		MaxTokens: a.maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("host: %s: gateway invoke: %w", a.node.ID, err)
	}

	out := resp.Message
	if out == nil {
		out = agenkit.NewMessage("agent", "")
	}
	// Surface metering provenance in metadata (not as behavior): which node,
	// what it cost, and — for honest accounting — whether cost was synthesized.
	out.WithMetadata("telos.node", string(a.node.ID))
	out.WithMetadata("telos.cost", cost.Amount)
	out.WithMetadata("telos.cost_synthesized", cost.Synthesized)
	out.WithMetadata("telos.backend", resp.Backend)
	return out, nil
}

// budgetCeiling expresses the node's budget request as a ceiling Budget for the
// router. M1 does not yet cascade on it; passed for interface completeness.
func (a *gatewayAgent) budgetCeiling() acs.Budget {
	return acs.Budget{Amount: a.node.Budget.Amount, Period: a.node.Budget.Period}
}

func (a *gatewayAgent) Name() string           { return string(a.node.ID) }
func (a *gatewayAgent) Capabilities() []string { return []string{"reason", "gateway"} }
func (a *gatewayAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}
