// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
)

// FSx Lustre is a PERISHABLE RECTANGLE (§8, M6 #67): a durable filesystem bills
// hourly INDEPENDENT of any instance. Its cost must be attributed and torn down
// EXPLICITLY — never folded into instance cost — so the governor (and burn-rate)
// can see a filesystem that outlives the compute it served. This is modeled as its
// OWN synthesized cost line, separate from the compute meter.
//
// Per-run EPHEMERAL is the default (created + torn down with the job, so its
// rectangle is bounded by the run). DURABLE carries an explicit FSxTTL and bills
// until torn down or the TTL reaper fires — that is the line a consumer must see
// distinctly, because it can silently accrue after the compute is long gone.

// FSxRatePerGBHour is a nominal modeled $/GB-hour for FSx Lustre. Like all M6
// compute cost it is an ESTIMATE (modeled, not billed); the gateway owns it.
var FSxRatePerGBHour = 0.000194 // ~ $0.14/GB-month / 730h, nominal

// fsxCost models a filesystem's hourly accrual over a billed duration as its own
// SYNTHESIZED line (issue #23: modeled, distinguishable from measured). capacityGB
// is the provisioned size; billed is how long it lived (for ephemeral, the run
// duration; for durable, until teardown/TTL).
func fsxCost(capacityGB int, billed time.Duration, currency string) acs.Cost {
	if currency == "" {
		currency = "USD"
	}
	amount := float64(capacityGB) * FSxRatePerGBHour * billed.Hours()
	return acs.SynthesizedCost(amount, currency, acs.TokenUsage{})
}

// FSxLine is the separate ledger line for a filesystem's perishable rectangle.
// It is reported alongside (not inside) a compute WorkResult's cost, so a
// durable filesystem's ongoing accrual is never hidden in instance cost.
type FSxLine struct {
	Mode       string        // "ephemeral" | "durable"
	CapacityGB int           // provisioned size
	Billed     time.Duration // how long it has billed
	Cost       acs.Cost      // synthesized, its own line
	// Durable filesystems bill until teardown; Telos must attribute that lifetime.
	TTL time.Duration
}

// fsxLineFor builds the FSx ledger line for a spec's filesystem, or nil if the
// spec provisions none. capacityGB defaults to a minimum FSx size when unset.
func fsxLineFor(fsx compute.FSxLifecycle, billed time.Duration, capacityGB int, currency string) *FSxLine {
	if fsx.Mode == "" {
		return nil
	}
	if capacityGB <= 0 {
		capacityGB = 1200 // FSx Lustre minimum
	}
	// A durable filesystem bills for at least its TTL (the rectangle outlives the
	// run); an ephemeral one bills only for the run's wall-clock.
	billedFor := billed
	if fsx.Mode == "durable" && fsx.TTL > billedFor {
		billedFor = fsx.TTL
	}
	return &FSxLine{
		Mode:       fsx.Mode,
		CapacityGB: capacityGB,
		Billed:     billedFor,
		Cost:       fsxCost(capacityGB, billedFor, currency),
		TTL:        fsx.TTL,
	}
}
