// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/agenkit-go/patterns"
	"github.com/scttfrdmn/telos/acs"
)

// stubPlanner is the PlannerAgent a Supervisor node needs. agenkit's
// SupervisorAgent calls Plan to decompose a message into Subtasks (each routed
// to a specialist by Type) and Synthesize to combine specialist results.
//
// In M0 the planner is deterministic and content-free: it emits exactly one
// subtask per child specialist, in a stable order, and synthesizes by
// concatenating their outputs. This exercises the Supervisor pattern's plan →
// delegate → synthesize flow without a model. (The real planner-as-root,
// architecture invariant 3, arrives in M3.)
type stubPlanner struct {
	nodeID    acs.NodeID
	specTypes []string // specialist keys, stable order
}

func newStubPlanner(nodeID acs.NodeID, specialistTypes []string) *stubPlanner {
	sorted := append([]string(nil), specialistTypes...)
	sort.Strings(sorted)
	return &stubPlanner{nodeID: nodeID, specTypes: sorted}
}

// Plan emits one subtask per specialist, preserving the input message.
func (p *stubPlanner) Plan(ctx context.Context, message *agenkit.Message) ([]patterns.Subtask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	subtasks := make([]patterns.Subtask, 0, len(p.specTypes))
	for _, t := range p.specTypes {
		subtasks = append(subtasks, patterns.Subtask{
			Type:     t,
			Message:  message,
			Metadata: map[string]interface{}{"supervisor": string(p.nodeID)},
		})
	}
	return subtasks, nil
}

// Synthesize concatenates specialist results in a stable order.
func (p *stubPlanner) Synthesize(ctx context.Context, original *agenkit.Message, results map[string]*agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "[%s/supervisor] synthesized %d specialist result(s):", p.nodeID, len(keys))
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  - %s: %s", k, results[k].ContentString())
	}
	return agenkit.NewMessage("agent", b.String()), nil
}

// Process is the PlannerAgent's own direct handling (used when Plan returns no
// subtasks). It just acknowledges, deterministically.
func (p *stubPlanner) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return agenkit.NewMessage("agent", fmt.Sprintf("[%s/supervisor] no subtasks", p.nodeID)), nil
}

func (p *stubPlanner) Name() string           { return string(p.nodeID) + ".planner" }
func (p *stubPlanner) Capabilities() []string { return []string{"stub", "planner"} }
func (p *stubPlanner) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(p)
}
