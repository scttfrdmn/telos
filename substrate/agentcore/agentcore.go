// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package agentcore fills cohort's PROVIDER seam for the A2A-session rung — the
// second transport rung (architecture §7): isolation / untrusted / resource
// boundary. A node placed here runs in its own session (its own host instance),
// reached over the AgentCore contract (the M0 GET /ping + POST /invocations).
//
// "The launch is easy; the Observer is the design" (invariant 7). The Actuator
// stands a session up; the OBSERVER — mapping session readiness/lifecycle onto
// cohort's phase model, and turning a dead session into a legible cohort Record
// rather than a bare error — is the real adapter design.
//
// M4 scope: LOCAL LOOPBACK. The session is an in-process host driven through the
// real A2A request/response envelope (an httptest server over the host's
// Handler), so budget/cancel cross a real protocol boundary without a network.
// Deploying to a real AgentCore session stays gated (a separate step). To avoid
// importing the host package (which depends on substrates), the session's
// contract surface is injected as an http.Handler — the dependency points one
// way: host wires agentcore, never the reverse.
package agentcore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/spore-host/cohort"
)

// SessionFactory produces the contract surface (an http.Handler answering
// GET /ping + POST /invocations) for a new session. The host supplies this; the
// substrate never imports host. Returning an error means the session could not
// be constructed (a legible launch fault, not a panic).
type SessionFactory func(ctx context.Context, id cohort.EntityID) (http.Handler, error)

// session is one launched A2A session: an in-process httptest server over the
// injected contract handler, plus its cohort lifecycle state.
type session struct {
	id        cohort.EntityID
	srv       *httptest.Server
	state     cohort.LifecycleState
	launchErr error
}

// Substrate is the AgentCore (A2A-session) cohort provider.
type Substrate struct {
	factory SessionFactory
	mu      sync.Mutex
	live    map[cohort.EntityID]*session
	now     func() time.Time
}

// New returns the A2A-session substrate. The factory builds each session's
// contract surface (in M4, an in-process host).
func New(factory SessionFactory) *Substrate {
	return &Substrate{
		factory: factory,
		live:    make(map[cohort.EntityID]*session),
		now:     time.Now,
	}
}

// Name identifies the substrate in a node's Placement.
func (s *Substrate) Name() string { return "agentcore" }

// Transport reports the rung this substrate occupies.
func (s *Substrate) Transport() acs.Transport { return acs.TransportA2A }

// --- cohort.Actuator ---------------------------------------------------------

// Launch stands up a session: build its contract surface via the factory and
// start an in-process server over it (the A2A boundary). A factory error is
// returned as an error (cohort classifies it); the entity is left observable as
// Failed so the Observer can render a legible Record rather than vanishing.
func (s *Substrate) Launch(ctx context.Context, intent cohort.EntityIntent) (cohort.Observation, error) {
	if err := ctx.Err(); err != nil {
		return cohort.Observation{}, err
	}
	handler, err := s.factory(ctx, intent.ID)
	if err != nil {
		s.mu.Lock()
		s.live[intent.ID] = &session{id: intent.ID, state: cohort.StateFailed, launchErr: err}
		s.mu.Unlock()
		return cohort.Observation{}, fmt.Errorf("agentcore: session launch for %q: %w", intent.ID, err)
	}
	srv := httptest.NewServer(handler)
	sess := &session{id: intent.ID, srv: srv, state: cohort.StateLaunching}
	s.mu.Lock()
	s.live[intent.ID] = sess
	s.mu.Unlock()
	return cohort.Observation{
		ID:         intent.ID,
		Generation: intent.Generation,
		State:      cohort.StateLaunching,
		ProviderID: "a2a:" + srv.URL,
		Address:    srv.URL,
		ObservedAt: s.now(),
	}, nil
}

// Start resumes a session. Sessions have no warm state in M4; Start re-probes.
func (s *Substrate) Start(ctx context.Context, id cohort.EntityID) (cohort.Observation, error) {
	obs := s.observeOne(ctx, id)
	return obs, nil
}

// Stop accepts only StopWarm (a session has no hibernate in M4) and tears the
// session's server down — there is nothing warm to preserve.
func (s *Substrate) Stop(ctx context.Context, id cohort.EntityID, mode cohort.StopMode) error {
	return s.Terminate(ctx, id)
}

// Terminate closes the session's server. Idempotent.
func (s *Substrate) Terminate(ctx context.Context, id cohort.EntityID) error {
	s.mu.Lock()
	sess := s.live[id]
	delete(s.live, id)
	s.mu.Unlock()
	if sess != nil && sess.srv != nil {
		sess.srv.Close()
	}
	return nil
}

// --- cohort.Observer (THE DESIGN WORK, invariant 7) --------------------------

// Observe maps each session's A2A lifecycle onto cohort's state model. A session
// is Running once it answers GET /ping; a launch that failed is Failed (carrying
// its error for a legible Record); an unknown id is StateUnknown (lag, not
// absence — the reconciler uses the idempotency token as ground truth).
func (s *Substrate) Observe(ctx context.Context, ids []cohort.EntityID) ([]cohort.Observation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]cohort.Observation, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.observeOne(ctx, id))
	}
	return out, nil
}

// observeOne probes a single session's readiness via the A2A /ping contract.
func (s *Substrate) observeOne(ctx context.Context, id cohort.EntityID) cohort.Observation {
	s.mu.Lock()
	sess := s.live[id]
	s.mu.Unlock()

	if sess == nil {
		// Never launched / already torn down: lag, not absence.
		return cohort.Observation{ID: id, State: cohort.StateUnknown, ObservedAt: s.now()}
	}
	if sess.state == cohort.StateFailed {
		return cohort.Observation{ID: id, State: cohort.StateFailed,
			ProviderID: "a2a:failed", ObservedAt: s.now()}
	}
	// Readiness probe: the session is Running when it answers /ping.
	obs := cohort.Observation{ID: id, ProviderID: "a2a:" + sess.srv.URL, Address: sess.srv.URL, ObservedAt: s.now()}
	if s.ping(ctx, sess.srv.URL) {
		obs.State = cohort.StateRunning
		s.setState(id, cohort.StateRunning)
	} else {
		// Launched but not yet answering: lag (StateUnknown), not failure.
		obs.State = cohort.StateUnknown
	}
	return obs
}

// ping probes the session's GET /ping contract endpoint.
func (s *Substrate) ping(ctx context.Context, baseURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/ping", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *Substrate) setState(id cohort.EntityID, st cohort.LifecycleState) {
	s.mu.Lock()
	if sess := s.live[id]; sess != nil {
		sess.state = st
	}
	s.mu.Unlock()
}

// --- cohort.Classifier -------------------------------------------------------

// Classify maps an A2A/session error to a cohort Fault. A session-launch or
// transport failure is Terminal (the rung is unavailable — the placer/ladder may
// fall back, but it is not a capacity ICE). Context cancel/deadline are the
// kill-switch crossing the boundary. NEVER FaultAmbiguous (cohort contract).
func (s *Substrate) Classify(err error) cohort.Fault {
	switch {
	case err == nil:
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "Nil"}
	case ctxCanceled(err):
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "ContextCanceled", Message: err.Error()}
	case ctxDeadline(err):
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "DeadlineExceeded", Message: err.Error()}
	default:
		return cohort.Fault{Class: cohort.FaultTerminal, Code: "A2ASessionError", Message: err.Error()}
	}
}

// SessionURL returns the A2A base URL of a live session (for the wire layer to
// invoke against). Empty if the session is not live.
func (s *Substrate) SessionURL(id cohort.EntityID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.live[id]; sess != nil && sess.srv != nil {
		return sess.srv.URL
	}
	return ""
}

var (
	_ cohort.Actuator   = (*Substrate)(nil)
	_ cohort.Observer   = (*Substrate)(nil)
	_ cohort.Classifier = (*Substrate)(nil)
)
