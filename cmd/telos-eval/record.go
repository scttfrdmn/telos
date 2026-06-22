// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package main

import (
	"encoding/json"
	"os"

	"github.com/scttfrdmn/telos/governor"
)

// Question is one eval input plus WHY it was chosen (which archetype + weak point
// it probes), so the eval is legible.
type Question struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Why    string `json:"why"`
	// Domain is a coarse tag (biomed | physics | cs | econ | …) used to confirm the
	// set spans domains and the TREM2 tuning can't help.
	Domain string `json:"domain"`
	// ExpectArchetype is the archetype WE expect (for measuring detection accuracy
	// — NOT fed to the system; the system infers blind).
	ExpectArchetype string `json:"expect_archetype"`
}

// RunRecord is the captured signal for one question — the JSONL row. Every field
// is MEASURED from the run, not assumed.
type RunRecord struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Why    string `json:"why"`
	Error  string `json:"error,omitempty"`

	// --- inference shape ---
	Archetype         string `json:"archetype"`          // detected: composite|mechanistic|evidence-synthesis
	CompositeDetected bool   `json:"composite_detected"` // archetype == composite

	// --- scope (Q2: does calibration generalize) ---
	ScopeEntities []ScopeEntity `json:"scope_entities"` // the EMITTED expansion (inspectable)
	ScopeMin      int           `json:"scope_min"`      // flatten bound
	ScopeMax      int           `json:"scope_max"`      // explode bound
	ScopeWithin   bool          `json:"scope_within"`   // landed in [min,max]?
	ScopeNote     string        `json:"scope_note"`     // the bounding note (incl. flat/explode WARNING)
	ScopeDropped  []string      `json:"scope_dropped,omitempty"`

	// --- path (Q1: cheap vs escalation) ---
	Tier    string `json:"tier,omitempty"`    // model tier the leaves used (cheap/mid/frontier)
	Backend string `json:"backend,omitempty"` // which backend served

	// --- exit + verdict ---
	Accepted            bool   `json:"accepted"`
	Basis               string `json:"basis"` // oracle-verified|concordant-under-test|contested|not-adjudicated
	Contested           bool   `json:"contested"`
	ReconciledDirection string `json:"reconciled_direction,omitempty"`
	ForAgainst          string `json:"for_against,omitempty"` // the assembled both-sides record (legibility)

	// --- standard of proof (Q3) ---
	Standard string `json:"standard"` // the default standard the run hit

	// --- cost + surplus (Q1 leak, burst-pool sizing) ---
	CostTotal      float64 `json:"cost_total"`      // total $ this question spent
	CostModeled    float64 `json:"cost_modeled"`    // synthesized portion (compute / local)
	CostMetered    float64 `json:"cost_metered"`    // real provider-billed portion
	ReservoirAfter float64 `json:"reservoir_after"` // grant remaining after the run
	BankedSurplus  float64 `json:"banked_surplus"`  // surplus banked (only on acceptance)
	SurplusCause   string  `json:"surplus_cause,omitempty"`
	SpentSoFar     float64 `json:"spent_so_far"` // cumulative across the run (cap tracking)

	// --- faults / orphans / races (Q4) ---
	Faults []string `json:"faults,omitempty"` // fault dispositions observed
	Output string   `json:"output_excerpt"`   // first chars of the model's answer

	// --- transport ---
	HTTPStatus int `json:"http_status"` // the /invocations status (200 = ran; 500 = errored)
}

type ScopeEntity struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// invocationResponse mirrors host.InvocationResponse for decoding (the harness
// reads the contract JSON, not the Go type, so it stays decoupled from host).
type invocationResponse struct {
	Output    string         `json:"output"`
	Archetype string         `json:"archetype"`
	Accepted  bool           `json:"accepted"`
	Basis     string         `json:"basis"`
	Metadata  map[string]any `json:"metadata"`
}

// fromInvocation extracts the measured shape from the run response.
func (r *RunRecord) fromInvocation(inv invocationResponse) {
	r.Archetype = inv.Archetype
	r.CompositeDetected = inv.Archetype == "composite"
	r.Accepted = inv.Accepted
	r.Basis = inv.Basis
	r.Output = excerpt(inv.Output, 240)

	md := inv.Metadata
	if md == nil {
		return
	}
	r.Standard, _ = md["telos.standard"].(string)
	r.Tier, _ = md["telos.tier"].(string)
	r.Backend, _ = md["telos.backend"].(string)
	r.ReconciledDirection, _ = md["telos.reconciled_direction"].(string)
	r.ForAgainst, _ = md["telos.forAgainst"].(string)
	if c, ok := md["telos.contested"].(bool); ok {
		r.Contested = c
	}
	// scope: telos.scoping is a nested object.
	if sc, ok := md["telos.scoping"].(map[string]any); ok {
		r.ScopeMin = toInt(sc["MinEntities"])
		r.ScopeMax = toInt(sc["MaxEntities"])
		r.ScopeNote, _ = sc["Note"].(string)
		if ents, ok := sc["Entities"].([]any); ok {
			for _, e := range ents {
				if em, ok := e.(map[string]any); ok {
					name, _ := em["Name"].(string)
					reason, _ := em["Reason"].(string)
					r.ScopeEntities = append(r.ScopeEntities, ScopeEntity{Name: name, Reason: reason})
				}
			}
		}
		n := len(r.ScopeEntities)
		r.ScopeWithin = r.ScopeMin <= n && n <= r.ScopeMax
		if dropped, ok := sc["Dropped"].([]any); ok {
			for _, d := range dropped {
				if s, ok := d.(string); ok {
					r.ScopeDropped = append(r.ScopeDropped, s)
				}
			}
		}
	}
}

// fromGovernor extracts the measured cost/surplus/fault signal from the run's
// governor — the ground truth for money, not the response's self-report.
func (r *RunRecord) fromGovernor(gov *governor.Mem) {
	if gov == nil {
		return
	}
	total, synth := gov.Spent(governor.RootGrant)
	r.CostTotal = total
	r.CostModeled = synth
	r.CostMetered = total - synth
	r.ReservoirAfter = gov.Remaining(governor.RootGrant).Amount
	r.BankedSurplus = gov.TotalBankedSurplus()
	for _, sig := range gov.Signals() {
		if sig.Cause != "" && r.SurplusCause == "" {
			r.SurplusCause = sig.Cause
		}
		if sig.Fault != nil {
			r.Faults = append(r.Faults, sig.Fault.FaultSummary())
		}
	}
}

func toInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func excerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func loadQuestions(path string) ([]Question, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qs []Question
	if err := json.Unmarshal(data, &qs); err != nil {
		return nil, err
	}
	return qs, nil
}
