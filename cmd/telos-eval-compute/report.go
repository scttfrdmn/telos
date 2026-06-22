// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package main

import (
	"encoding/json"
	"os"
)

// Phase2Report is the captured result of the real-instance orphan-reconciler test.
type Phase2Report struct {
	Entity       string  `json:"entity"`
	ProviderID   string  `json:"provider_id"`
	InstanceType string  `json:"instance_type"`
	TTLMinutes   int     `json:"ttl_minutes"`
	CostLimit    float64 `json:"cost_limit_usd"`
	LaunchedAt   string  `json:"launched_at"`

	Stranded bool `json:"stranded"` // ledger discarded (crash simulated)

	OrphansDetected     int      `json:"orphans_detected"`
	OurInstanceDetected bool     `json:"our_instance_detected"` // did Reconcile find THE instance we stranded?
	Terminated          []string `json:"terminated"`

	ResidualRunning int    `json:"residual_running"` // telos-tagged instances still up after terminate
	ResidualClean   bool   `json:"residual_clean"`   // zero residual = the safety net holds
	CheckedAt       string `json:"checked_at"`
}

func emit(r Phase2Report) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}
