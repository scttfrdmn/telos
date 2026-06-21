// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package inproc fills cohort's PROVIDER seam for the goroutine rung — the
// default transport (architecture §7): same trust + budget tree, I/O-bound
// fan-out, near-zero marginal cost.
//
// It implements cohort.Actuator / cohort.Observer / cohort.Classifier for
// in-process placement. "Placing" a node on this substrate means reserving a
// goroutine slot in the same envelope — there is no microVM, no warm/hibernate,
// no capacity to exhaust. The agenkit patterns provide the actual goroutine
// fan-out when the node runs; this adapter holds cohort's lifecycle seam (bring
// the entity to PhaseReady), it does not duplicate agenkit's concurrency.
//
// This is the real cohort fill that replaces M0's placeholder (telos#12): the
// goroutine substrate is now reconciled by the unmodified cohort core, with a
// transport.Placement carrying no cloud vocabulary.
package inproc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/spore-host/cohort"
)

// Substrate is the in-process (goroutine) cohort provider.
type Substrate struct {
	mu       sync.Mutex
	launched map[cohort.EntityID]cohort.Observation
	now      func() time.Time
}

// New returns the in-process substrate.
func New() *Substrate {
	return &Substrate{
		launched: make(map[cohort.EntityID]cohort.Observation),
		now:      time.Now,
	}
}

// Name identifies the substrate in a node's Placement.
func (s *Substrate) Name() string { return "inproc" }

// Transport reports the rung this substrate occupies.
func (s *Substrate) Transport() acs.Transport { return acs.TransportGoroutine }

// --- cohort.Actuator ---------------------------------------------------------

// Launch reserves a goroutine slot for the entity. In-process there is no
// provisioning latency and no capacity limit, so the slot is immediately
// available: the entity is observed StateLaunching here and reports StateRunning
// on the next Observe. The Placement's rung name is recorded for legibility.
func (s *Substrate) Launch(ctx context.Context, intent cohort.EntityIntent) (cohort.Observation, error) {
	if err := ctx.Err(); err != nil {
		return cohort.Observation{}, err
	}
	obs := cohort.Observation{
		ID:         intent.ID,
		Generation: intent.Generation,
		State:      cohort.StateLaunching,
		ProviderID: "goroutine:" + string(intent.ID),
		ObservedAt: s.now(),
	}
	s.mu.Lock()
	s.launched[intent.ID] = obs
	s.mu.Unlock()
	return obs, nil
}

// Start resumes an entity. In-process there is no warm state, so Start is
// equivalent to a fresh slot — it returns the entity to Running.
func (s *Substrate) Start(ctx context.Context, id cohort.EntityID) (cohort.Observation, error) {
	if err := ctx.Err(); err != nil {
		return cohort.Observation{}, err
	}
	return s.transition(id, cohort.StateRunning), nil
}

// Stop is a no-op for the goroutine rung: there is nothing warm to keep. It
// accepts StopWarm (the only mode meaningful in-process) and records Stopped.
func (s *Substrate) Stop(ctx context.Context, id cohort.EntityID, mode cohort.StopMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.transition(id, cohort.StateStopped)
	return nil
}

// Terminate releases the goroutine slot. Idempotent.
func (s *Substrate) Terminate(ctx context.Context, id cohort.EntityID) error {
	s.mu.Lock()
	delete(s.launched, id)
	s.mu.Unlock()
	return nil
}

// --- cohort.Observer ---------------------------------------------------------

// Observe reports current state. A launched in-process entity is synchronously
// Running (no provisioning lag). An id never launched is StateUnknown — lag, not
// absence (cohort's eventual-consistency rule; the reconciler uses the
// idempotency token as ground truth, not the Observer).
func (s *Substrate) Observe(ctx context.Context, ids []cohort.EntityID) ([]cohort.Observation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]cohort.Observation, 0, len(ids))
	for _, id := range ids {
		if obs, ok := s.launched[id]; ok {
			// Slot is live: report Running.
			obs.State = cohort.StateRunning
			obs.ObservedAt = s.now()
			s.launched[id] = obs
			out = append(out, obs)
		} else {
			out = append(out, cohort.Observation{ID: id, State: cohort.StateUnknown, ObservedAt: s.now()})
		}
	}
	return out, nil
}

// --- cohort.Classifier -------------------------------------------------------

// Classify maps an in-process error to a cohort Fault. The goroutine rung cannot
// ICE (no capacity), so it never returns CapacityExhausted. Context cancellation
// and deadline are Terminal-for-this-attempt (the kill-switch / exhaustion);
// everything else is Terminal with the verbatim error preserved for legibility.
// It NEVER returns FaultAmbiguous (cohort contract — in-process mutation status
// is always known).
func (s *Substrate) Classify(err error) cohort.Fault {
	switch {
	case err == nil:
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "Nil"}
	case errors.Is(err, context.Canceled):
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "ContextCanceled", Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "DeadlineExceeded", Message: err.Error()}
	default:
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "InProcError", Message: err.Error()}
	}
}

// transition updates a tracked entity's state (or records it if absent).
func (s *Substrate) transition(id cohort.EntityID, st cohort.LifecycleState) cohort.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	obs := s.launched[id]
	obs.ID = id
	obs.State = st
	obs.ObservedAt = s.now()
	if obs.ProviderID == "" {
		obs.ProviderID = "goroutine:" + string(id)
	}
	s.launched[id] = obs
	return obs
}

var (
	_ cohort.Actuator   = (*Substrate)(nil)
	_ cohort.Observer   = (*Substrate)(nil)
	_ cohort.Classifier = (*Substrate)(nil)
)
