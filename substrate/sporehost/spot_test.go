// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/governor"
	"github.com/spore-host/cohort"
)

// env builds a run-envelope budget for the governor in these tests.
func env(amount float64) acs.Budget {
	return acs.Budget{Amount: amount, Period: 30 * 24 * time.Hour, Currency: "USD"}
}

// faultHandler settles a reclamation as a fault via the M5 fault-Record-through-
// the-WAL path (governor.RecordFault) — the real core settlement. It records which
// entities were settled and whether each came via the webhook.
type faultHandler struct {
	gov     *governor.Mem
	grantOf map[string]governor.GrantID // entity → its reserved grant
	settled map[string]bool
	viaHook map[string]bool
}

func newFaultHandler(gov *governor.Mem) *faultHandler {
	return &faultHandler{gov: gov, grantOf: map[string]governor.GrantID{}, settled: map[string]bool{}, viaHook: map[string]bool{}}
}

func (h *faultHandler) OnReclaimed(ctx context.Context, r Reclamation) error {
	gid, ok := h.grantOf[r.EntityID]
	if !ok {
		return nil // unknown entity (already settled / not ours) — no-op, safe
	}
	// Settle spent compute as a FAULT-WITH-REASON (M5 #C1): no surplus, escrow
	// released, legible disposition recorded.
	err := h.gov.RecordFault(ctx, gid, cohort.Fault{
		Class: cohort.FaultTerminal, Code: "SpotReclamation",
		Message: r.Reason,
	})
	if err != nil {
		return err
	}
	h.settled[r.EntityID] = true
	h.viaHook[r.EntityID] = r.ViaWebhook
	return nil
}

// #D1 — POLL-AND-INFER baseline: a reclamation settles as a fault with NO webhook.
func TestSpot_InferredBaseline(t *testing.T) {
	gov := governor.New(env(100))
	h := newFaultHandler(gov)
	reserveForReal(t, gov, h, "job-1")

	// The Observer inferred DispositionSpot from terminated+tags.
	obs := []compute.Observation{
		{EntityID: "job-1", State: compute.StateFailed, Disposition: compute.DispositionSpot,
			ComputeSeconds: 120, Reason: "spot-reclaim (inferred from terminated spot instance)"},
	}
	if err := CheckInferred(context.Background(), h, obs); err != nil {
		t.Fatal(err)
	}
	if !h.settled["job-1"] {
		t.Fatal("inferred reclamation must settle as a fault (the always-correct baseline)")
	}
	if h.viaHook["job-1"] {
		t.Fatal("this path is webhook-MISSED (inferred), not via webhook")
	}
	// Settled as a fault: no surplus banked, legible disposition recorded.
	if gov.BankedSurplus(h.grantOf["job-1"]) != 0 {
		t.Fatal("a reclamation fault must bank no surplus")
	}
	if d := gov.Fault(h.grantOf["job-1"]); d == nil || d.Code != "SpotReclamation" {
		t.Fatalf("fault disposition not recorded legibly: %+v", d)
	}
}

// #D2 — IN-WINDOW WEBHOOK: the same reclamation settles via the POST, before the
// instance is gone, correlated by the opaque key Telos set at launch.
func TestSpot_WebhookPath(t *testing.T) {
	gov := governor.New(env(100))
	h := newFaultHandler(gov)
	reserveForReal(t, gov, h, "entity-key-42")

	srv := httptest.NewServer(WebhookReceiver(h))
	defer srv.Close()

	// spawn v0.63.0 POSTs the fixed fact-struct; Correlation = Telos's entity key.
	payload := spotWebhookPayload{
		Event: "spot_interruption", InstanceID: "i-123", Action: "terminate",
		NameTag: "entity-key-42", ComputeSeconds: 90,
		Correlation: "entity-key-42", Reason: "spot-interruption",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook POST status = %d", resp.StatusCode)
	}
	if !h.settled["entity-key-42"] {
		t.Fatal("webhook reclamation must settle as a fault, in window")
	}
	if !h.viaHook["entity-key-42"] {
		t.Fatal("this path is via the webhook")
	}
}

// CRITICAL: both paths converge — a reclamation settles correctly whether the
// webhook fired OR was missed. And settling BOTH (webhook then late inferred) is
// idempotent (M5 fault is idempotent by GrantID) — no double-settle.
func TestSpot_BothPathsConverge_Idempotent(t *testing.T) {
	gov := governor.New(env(100))
	h := newFaultHandler(gov)
	reserveForReal(t, gov, h, "job-x")
	gid := h.grantOf["job-x"]

	// Webhook fires first (in window).
	_ = h.OnReclaimed(context.Background(), Reclamation{EntityID: "job-x", ComputeSeconds: 60, Reason: "via webhook", ViaWebhook: true})
	remAfterWebhook := gov.Remaining(governor.RootGrant).Amount

	// Then the inferred path also fires (the instance is later observed terminated).
	// M5 idempotency: settling the already-faulted grant is a no-op.
	obs := []compute.Observation{{EntityID: "job-x", State: compute.StateFailed, Disposition: compute.DispositionSpot, Reason: "inferred"}}
	_ = CheckInferred(context.Background(), h, obs)

	if gov.Remaining(governor.RootGrant).Amount != remAfterWebhook {
		t.Fatal("double-settle (webhook + inferred) changed the ledger — must be idempotent")
	}
	if gov.Fault(gid) == nil {
		t.Fatal("fault disposition lost")
	}
}

// reserveForReal reserves a real grant for an entity.
func reserveForReal(t *testing.T, gov *governor.Mem, h *faultHandler, entity string) {
	t.Helper()
	g, err := gov.Reserve(context.Background(), governor.RootGrant,
		acs.BudgetRequest{Amount: 20, Period: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h.grantOf[entity] = governor.GrantID(g.GrantID)
}
