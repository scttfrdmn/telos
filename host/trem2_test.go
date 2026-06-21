// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The §14 gate (the keystone). On the TREM2 question against a REAL capable
// backend, the system must: emit a two-phase composite graph; scope the entity
// expansion between flatten and explode (inspectable); return an EARNED,
// provenanced Contested; and complete banking surplus on the accepted-contested
// result, with the lexicographic guard holding live.
//
// It is gated behind TELOS_SMOKE_TREM2_MODEL (env-only, no default — set it to a
// Bedrock model id, e.g. anthropic.claude-3-5-sonnet-20241022-v2:0, with AWS
// creds). The deterministic PLUMBING is proven offline (recursion_test.go,
// settlement_test.go); this verifies the STRUCTURE holds with a real model in the
// loop — the architecture structures the inquiry so a capable model preserves
// contested; it cannot force a weak one.
//
// On failure it fails on ONE identifiable §14 check (composite / scope /
// contested / bank), so the failure is diagnostic.
const trem2Question = "does microglial TREM2 signaling modulate tau propagation in the entorhinal cortex, and what's the current evidence?"

func TestSmoke_TREM2_Section14(t *testing.T) {
	model := os.Getenv("TELOS_SMOKE_TREM2_MODEL")
	if model == "" {
		t.Skip("set TELOS_SMOKE_TREM2_MODEL (a Bedrock model id, with AWS creds) to run the §14 gate")
	}

	seed := seedSpec(t)
	deps, err := NewDeps(context.Background(), DepsConfig{
		Envelope: seed.Budget,
		Bedrock:  &BedrockConfig{ModelID: model, Region: os.Getenv("AWS_REGION")},
	}, nil)
	if err != nil {
		t.Fatalf("deps: %v", err)
	}
	srv, err := NewServer(seed, deps, nil)
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	body := strings.NewReader(`{"prompt":` + jsonString(trem2Question) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/invocations", body)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invocation failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp InvocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// §14 #1 — two-phase composite graph.
	if resp.Archetype != "composite" {
		t.Fatalf("§14 #1 FAILED (composite): archetype = %q, want composite", resp.Archetype)
	}

	// §14 #2 — scoping bounded, inspectable, neither flat nor exploded.
	scoping, ok := resp.Metadata["telos.scoping"].(map[string]any)
	if !ok {
		t.Fatal("§14 #2 FAILED (scope): scoping not surfaced for inspection")
	}
	ents, _ := scoping["Entities"].([]any)
	minE := toInt(scoping["MinEntities"])
	maxE := toInt(scoping["MaxEntities"])
	if len(ents) < minE || len(ents) > maxE {
		t.Fatalf("§14 #2 FAILED (scope): %d entities outside flatten/explode bounds [%d,%d]", len(ents), minE, maxE)
	}
	t.Logf("§14 #2 scope: %d entities within [%d,%d] — inspectable", len(ents), minE, maxE)

	// §14 #3 — earned, provenanced Contested (built from both directions).
	fa, _ := resp.Metadata["telos.forAgainst"].(string)
	if !strings.Contains(fa, "evidence_for") || !strings.Contains(fa, "evidence_against") {
		t.Fatalf("§14 #3 FAILED (contested): for/against record not assembled from both sides: %q", fa)
	}
	if resp.Basis != "contested" {
		t.Fatalf("§14 #3 FAILED (contested): basis = %q, want contested (an earned contested verdict)", resp.Basis)
	}

	// §14 #4 — accepted-contested banks surplus; lexicographic guard holds live.
	if !resp.Accepted {
		t.Fatalf("§14 #4 FAILED (bank): a provenanced contested result must be ACCEPTED, got accepted=false")
	}
	t.Logf("§14 PASSED: composite → scoped(%d) → earned-contested → accepted; output:\n%s", len(ents), resp.Output)
}

func toInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
