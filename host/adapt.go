// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"

	"github.com/scttfrdmn/agenkit-go/agenkit"
)

// processor is the subset of agenkit.Agent that the agenkit pattern types
// (SequentialAgent, ParallelAgent, SupervisorAgent, ReActAgent) actually
// implement in v0.85.0. Those types provide Name/Capabilities/Process but NOT
// Introspect — so they do not satisfy agenkit.Agent and cannot be nested as
// children of one another or returned as agenkit.Agent without help.
//
// (This is an upstream inconsistency: the framework's own composition patterns
// don't implement the framework's own Agent interface. Isolated here so the rest
// of the host can treat everything uniformly as agenkit.Agent.)
type processor interface {
	Name() string
	Capabilities() []string
	Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error)
}

// adapt wraps a pattern agent and supplies the missing Introspect method, making
// it a full agenkit.Agent that can be composed and returned. It returns an error
// (always nil) so call sites can stay uniform with the constructors that do.
func adapt(p processor, err error) (agenkit.Agent, error) {
	if err != nil {
		return nil, err
	}
	return adapted{inner: p}, nil
}

type adapted struct {
	inner processor
}

func (a adapted) Name() string           { return a.inner.Name() }
func (a adapted) Capabilities() []string { return a.inner.Capabilities() }
func (a adapted) Process(ctx context.Context, m *agenkit.Message) (*agenkit.Message, error) {
	return a.inner.Process(ctx, m)
}
func (a adapted) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}
