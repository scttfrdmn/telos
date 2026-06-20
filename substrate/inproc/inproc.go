// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package inproc is the goroutine substrate adapter: the default transport rung
// (architecture §7). Same trust + budget tree, I/O-bound fan-out, near-zero
// marginal cost. In M0 it is where every node runs.
//
// The architecture's Substrate interface embeds cohort.Actuator and
// cohort.Observer (§5). The cohort module (spore-host/cohort) is NOT yet
// published, so this package defines a MINIMAL LOCAL placeholder for the
// Actuator/Observer shape — just enough for an in-process supervisor — to be
// swapped for the real cohort interfaces when they land. The placeholder is
// deliberately small and clearly marked so the swap is mechanical.
package inproc

import (
	"context"

	"github.com/scttfrdmn/telos/acs"
)

// Readiness is the placeholder lifecycle signal a substrate Observer reports.
// "The launch is easy; the Observer is the design" (invariant 7) — the real
// signal set arrives with cohort.Observer.
type Readiness string

const (
	ReadyUnknown Readiness = "unknown"
	ReadyPending Readiness = "pending"
	ReadyReady   Readiness = "ready"
	ReadyDone    Readiness = "done"
	ReadyFailed  Readiness = "failed"
)

// Actuator is the placeholder for cohort.Actuator: it launches a unit of work.
// TODO(cohort): replace with cohort.Actuator when spore-host/cohort is published.
type Actuator interface {
	// Launch starts the node's work on this substrate. In-process this is a
	// goroutine in the same envelope; no isolation.
	Launch(ctx context.Context, nodeID acs.NodeID) error
}

// Observer is the placeholder for cohort.Observer: it reports readiness.
// TODO(cohort): replace with cohort.Observer when spore-host/cohort is published.
type Observer interface {
	// Observe reports the current readiness of a launched node.
	Observe(ctx context.Context, nodeID acs.NodeID) (Readiness, error)
}

// Substrate is the in-process substrate. M0 runs the whole graph here; the
// agenkit patterns themselves provide the goroutine fan-out (e.g. ParallelAgent),
// so this adapter is intentionally thin — it exists to hold the seam, not to
// duplicate agenkit's concurrency.
type Substrate struct{}

// New returns the in-process substrate.
func New() *Substrate { return &Substrate{} }

// Transport reports the rung this substrate occupies.
func (s *Substrate) Transport() acs.Transport { return acs.TransportGoroutine }

// Name identifies the substrate in a node's Placement.
func (s *Substrate) Name() string { return "inproc" }

// Launch is a no-op in M0: the host instantiates agenkit agents directly and the
// patterns spawn their own goroutines. Present so the Actuator seam is real.
func (s *Substrate) Launch(ctx context.Context, nodeID acs.NodeID) error {
	return ctx.Err()
}

// Observe reports readiness. In-process work is synchronous, so a launched node
// is reported ready immediately.
func (s *Substrate) Observe(ctx context.Context, nodeID acs.NodeID) (Readiness, error) {
	if err := ctx.Err(); err != nil {
		return ReadyFailed, err
	}
	return ReadyReady, nil
}

var (
	_ Actuator = (*Substrate)(nil)
	_ Observer = (*Substrate)(nil)
)
