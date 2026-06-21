// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scttfrdmn/telos/acs"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	seed, err := acs.LoadFile("../bootstrap.acs")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	// Wire deps (offline echo backend + planner) so the planning-root recursion
	// actually closes — the invocation tests exercise the emitted graph.
	deps, err := NewDeps(context.Background(), DepsConfig{Envelope: seed.Budget}, nil)
	if err != nil {
		t.Fatalf("deps: %v", err)
	}
	srv, err := NewServer(seed, deps, nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func TestPing(t *testing.T) {
	h := testServer(t).Handler()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want 200", rec.Code)
	}
	var resp PingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if resp.Status != "healthy" {
		t.Fatalf("ping status = %q, want healthy", resp.Status)
	}
	if !strings.HasPrefix(resp.SeedHash, "sha256:") {
		t.Fatalf("ping seed_hash = %q, want sha256:…", resp.SeedHash)
	}
}

func TestInvocations(t *testing.T) {
	h := testServer(t).Handler()
	body := strings.NewReader(`{"prompt":"does X cause Y, and what is the evidence?"}`)
	req := httptest.NewRequest(http.MethodPost, "/invocations", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("invocations status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp InvocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode invocation: %v", err)
	}

	// The SEED is now the minimal planning-root base case (M3): a single node
	// that EMITS the real graph at runtime. The seed spec reported is that base
	// case; the emitted inquiry's shape is reported via Archetype.
	if resp.Graph.RootID != "root" {
		t.Fatalf("graph root = %q, want root", resp.Graph.RootID)
	}
	if resp.Graph.NodeCount != 1 {
		t.Fatalf("seed node_count = %d, want 1 (planning-root base case)", resp.Graph.NodeCount)
	}
	// Budget surfaced as a grant rate, never a bare total (invariant 4).
	if resp.Graph.Budget.PeriodHours <= 0 || resp.Graph.Budget.RatePerDay <= 0 {
		t.Fatalf("budget must carry a period and a derived rate, got %+v", resp.Graph.Budget)
	}
	// The recursion closed: the planner inferred an archetype for the question.
	// "does X cause Y, and what is the evidence" is composite.
	if resp.Archetype != "composite" {
		t.Fatalf("expected composite archetype for a conjoined question, got %q", resp.Archetype)
	}
	// The separate-envelope acceptance node rendered a verdict (live, M2/M3).
	if resp.Basis == "" {
		t.Fatal("expected a labeled acceptance basis in the response")
	}
	if resp.Output == "" {
		t.Fatal("invocation produced no output")
	}
}

func TestInvocations_EmptyBody(t *testing.T) {
	h := testServer(t).Handler()
	req := httptest.NewRequest(http.MethodPost, "/invocations", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-body invocation status = %d, want 200 (seed runs on its own prompt)", rec.Code)
	}
}

func TestPing_WrongMethod(t *testing.T) {
	h := testServer(t).Handler()
	req := httptest.NewRequest(http.MethodPost, "/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("POST /ping should not be 200")
	}
	_, _ = io.ReadAll(rec.Body)
}
