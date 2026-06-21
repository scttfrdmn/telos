// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
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
	srv, err := NewServer(seed, nil, nil)
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

	// The instantiated graph must be reported — this is M0's deliverable.
	if resp.Graph.RootID != "root" {
		t.Fatalf("graph root = %q, want root", resp.Graph.RootID)
	}
	if resp.Graph.NodeCount != 10 {
		t.Fatalf("graph node_count = %d, want 10 (the seed)", resp.Graph.NodeCount)
	}
	if resp.Graph.Standard != string(acs.StandardConcordant) {
		t.Fatalf("graph standard = %q, want concordant (the seeded default)", resp.Graph.Standard)
	}
	// Budget surfaced as a grant rate, never a bare total (invariant 4).
	if resp.Graph.Budget.PeriodHours <= 0 || resp.Graph.Budget.RatePerDay <= 0 {
		t.Fatalf("budget must carry a period and a derived rate, got %+v", resp.Graph.Budget)
	}
	// All four patterns + acceptance present in the instantiated graph.
	seen := map[string]bool{}
	for _, n := range resp.Graph.Nodes {
		seen[n.Pattern] = true
		if n.Kind == "acceptance" && n.Trust == "same-envelope" {
			t.Fatalf("acceptance node %q is same-envelope (invariant 10 violated)", n.ID)
		}
	}
	for _, want := range []string{"sequential", "parallel", "supervisor", "react"} {
		if !seen[want] {
			t.Fatalf("instantiated graph missing pattern %q", want)
		}
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
