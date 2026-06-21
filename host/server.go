// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package host is the generic agenkit-go runtime that answers the AgentCore
// contract and instantiates an agent graph from an ACS (architecture §2, M0).
//
// It is generic by design: it knows patterns and the ACS schema, nothing about
// research. It serves GET /ping and POST /invocations on 0.0.0.0:8080, holds a
// seed Spec (bootstrap.acs), and on each invocation Builds the seed into an
// agenkit graph and runs it. Real model calls, budgeting, placement, and
// acceptance policy arrive in later milestones; M0 proves the contract and the
// composition.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acs"
)

// Server answers the AgentCore contract for a single seed Spec.
type Server struct {
	seed *acs.Spec
	deps *Deps // optional; when set, Reason leaves invoke through the gateway
	log  *slog.Logger
}

// NewServer constructs a host serving the given seed Spec. deps is optional:
// nil gives the M0 composition-only behavior (stub leaves); a non-nil deps wires
// Reason leaves to invoke models through the gateway (invariant 5).
func NewServer(seed *acs.Spec, deps *Deps, log *slog.Logger) (*Server, error) {
	if seed == nil {
		return nil, fmt.Errorf("host: nil seed spec")
	}
	if log == nil {
		log = slog.Default()
	}
	// Fail fast if the seed cannot be instantiated — a host that can't build its
	// own base case should not report healthy.
	if _, err := BuildWithDeps(seed, deps); err != nil {
		return nil, fmt.Errorf("host: seed is not instantiable: %w", err)
	}
	return &Server{seed: seed, deps: deps, log: log}, nil
}

// Handler returns the HTTP mux implementing the contract.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", s.handlePing)
	mux.HandleFunc("POST /invocations", s.handleInvocations)
	return mux
}

// PingResponse is the GET /ping body.
type PingResponse struct {
	Status   string `json:"status"`              // "healthy"
	SeedHash string `json:"seed_hash,omitempty"` // content hash of the seed Spec
	Time     string `json:"time"`
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, PingResponse{
		Status:   "healthy",
		SeedHash: s.seed.Hash,
		Time:     timestamp(r.Context()),
	})
}

// InvocationRequest is the POST /invocations body. The contract is intentionally
// small in M0: a prompt and an optional input message.
type InvocationRequest struct {
	// Prompt is the question/input for this invocation. Recorded for provenance;
	// the seed graph runs regardless.
	Prompt string `json:"prompt"`
	// Input optionally overrides the message handed to the root agent.
	Input string `json:"input,omitempty"`
}

// InvocationResponse is the POST /invocations result.
type InvocationResponse struct {
	// Output is the root agent's final message content.
	Output string `json:"output"`
	// Graph describes the instantiated SEED spec (the base case). With the M3
	// recursion the seed is a single planning node; the emitted graph it produced
	// is reported via Archetype + the agent metadata.
	Graph GraphSummary `json:"graph"`
	// Archetype is the inquiry shape the planner inferred for this question (the
	// emitted graph's shape) — "composite", "mechanistic", or "evidence-synthesis".
	Archetype string `json:"archetype,omitempty"`
	// Accepted / Basis report the separate-envelope acceptance verdict over the
	// run record (M2/M3). For a provenanced contested result, Accepted is true and
	// Basis is "contested".
	Accepted bool   `json:"accepted"`
	Basis    string `json:"basis,omitempty"`
	// Metadata carries the root message's metadata (scoping, for/against record,
	// metering — the auditable §14 surface).
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (s *Server) handleInvocations(w http.ResponseWriter, r *http.Request) {
	var req InvocationRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			// An empty body is allowed (the seed runs on its own prompt); a
			// malformed body is a client error.
			if err.Error() != "EOF" {
				writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
				return
			}
		}
	}

	root, err := BuildWithDeps(s.seed, s.deps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("instantiate seed: %w", err))
		return
	}

	input := req.Input
	if input == "" {
		input = req.Prompt
	}
	if input == "" {
		input = s.seed.Prompt
	}

	msg := agenkit.NewMessage("user", input)
	out, err := root.Process(r.Context(), msg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("run graph: %w", err))
		return
	}

	resp := InvocationResponse{
		Output:    out.ContentString(),
		Graph:     summarize(s.seed),
		Archetype: metaString(out, "telos.archetype"),
		Accepted:  metaBool(out, "telos.accepted"),
		Basis:     metaString(out, "telos.basis"),
		Metadata:  out.Metadata,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListenAndServe runs the host on addr (e.g. "0.0.0.0:8080") with sane timeouts
// and graceful shutdown on ctx cancellation.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		s.log.Info("host listening", "addr", addr, "seed_hash", s.seed.Hash)
		errc <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.log.Info("host shutting down")
		return srv.Shutdown(shutCtx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// timestamp returns an RFC3339 timestamp, or empty if a clock isn't desired.
// Kept as a function so it can be made deterministic in tests via context.
func timestamp(ctx context.Context) string {
	if v, ok := ctx.Value(clockKey{}).(string); ok {
		return v
	}
	return time.Now().UTC().Format(time.RFC3339)
}

type clockKey struct{}

// metaString reads a string metadata value from a message, or "" if absent.
func metaString(m *agenkit.Message, key string) string {
	if m == nil || m.Metadata == nil {
		return ""
	}
	s, _ := m.Metadata[key].(string)
	return s
}

// metaBool reads a bool metadata value from a message, or false if absent.
func metaBool(m *agenkit.Message, key string) bool {
	if m == nil || m.Metadata == nil {
		return false
	}
	b, _ := m.Metadata[key].(bool)
	return b
}
