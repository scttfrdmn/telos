// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/scttfrdmn/telos/compute"
)

// Spot reclamation settles correctly via TWO paths, converging on the SAME fault
// settlement (§8, M6 #D1/#D2):
//
//   - POLL-AND-INFER (always-correct baseline, #D1): the Observer sees a spot unit
//     terminated and infers DispositionSpot from tags/state. Works with NO webhook.
//   - IN-WINDOW WEBHOOK (upgrade, #D2): spawn v0.63.0 POSTs the reclamation fact
//     inside the ~2-min window, carrying the opaque WebhookCorrelation (Telos's
//     entity/grant key). Lets Telos settle and re-place BEFORE the instance is gone.
//
// Both call ReclamationHandler.OnReclaimed with a Reclamation; the handler settles
// spent compute as a fault-with-reason (the M5 fault-Record-through-the-WAL path,
// in the core governor) and re-places on lagotto's patient rung. A reclamation must
// settle correctly whether or not the webhook fired — so the inferred path is the
// baseline and the webhook is a latency upgrade over it, never a dependency.

// Reclamation is the normalized spot-reclamation fact, identical whether it came
// from the webhook payload or was inferred from polled state+tags.
type Reclamation struct {
	EntityID       string // correlated back to the cohort entity / grant
	Correlation    string // the opaque key Telos set at launch (entity/grant); webhook path
	Action         string // "terminate" | "stop" (AWS spot action); empty on the inferred path
	ComputeSeconds int64  // accrued modeled-cost basis at reclamation
	Reason         string // legible detail
	ViaWebhook     bool   // true: in-window push; false: inferred from terminated
}

// ReclamationHandler settles a reclamation as a fault and re-places the work. The
// core (gateway/governor) supplies it; the substrate detects + normalizes the
// reclamation and calls it. Keeping it an interface keeps the substrate free of
// the governor's WAL details — it just reports the fault fact.
type ReclamationHandler interface {
	OnReclaimed(ctx context.Context, r Reclamation) error
}

// CheckInferred scans observations for spot reclamations (DispositionSpot) and
// fires the handler for each — the ALWAYS-CORRECT baseline (#D1). Call this from
// the Observe loop; it works with the webhook disabled.
func CheckInferred(ctx context.Context, handler ReclamationHandler, obs []compute.Observation) error {
	for _, o := range obs {
		if o.Disposition != compute.DispositionSpot {
			continue
		}
		err := handler.OnReclaimed(ctx, Reclamation{
			EntityID:       o.EntityID,
			ComputeSeconds: o.ComputeSeconds,
			Reason:         o.Reason,
			ViaWebhook:     false,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// spotWebhookPayload mirrors the fixed fact-struct spawn v0.63.0 POSTs (spawn#228).
// Telos parses only what it needs; Correlation is its own opaque key echoed back.
type spotWebhookPayload struct {
	Event          string `json:"event"`
	InstanceID     string `json:"instance_id"`
	Action         string `json:"action"` // terminate | stop
	NameTag        string `json:"name_tag"`
	ComputeSeconds int64  `json:"compute_seconds"`
	Correlation    string `json:"correlation"` // Telos's entity/grant key, verbatim
	Reason         string `json:"reason"`
}

// WebhookReceiver is the HTTP handler for the in-window spot push (#D2). Mount it
// at the URL Telos sets as LaunchConfig.SpotInterruptionWebhookURL. On a POST it
// normalizes the payload and fires the handler — in window, before the instance is
// gone. It is best-effort on spawn's side (fire-once, may be missed), so the
// inferred path (CheckInferred) remains the durable baseline.
func WebhookReceiver(handler ReclamationHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var p spotWebhookPayload
		if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		// Correlation is Telos's own key (set at launch); prefer it for the entity,
		// fall back to the Name tag.
		entity := p.Correlation
		if entity == "" {
			entity = p.NameTag
		}
		err := handler.OnReclaimed(req.Context(), Reclamation{
			EntityID:       entity,
			Correlation:    p.Correlation,
			Action:         p.Action,
			ComputeSeconds: p.ComputeSeconds,
			Reason:         orDefault(p.Reason, "spot-interruption"),
			ViaWebhook:     true,
		})
		if err != nil {
			// Settle failed — but the inferred path will catch it later; ack so
			// spawn's fire-once best-effort POST isn't retried (it doesn't retry).
			http.Error(w, fmt.Sprintf("settle deferred to inferred path: %v", err), http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
