// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

// Surplus signal (M5 #D1, architecture §9: "surplus is a signal"). A settlement's
// disposition flows up to the planner and burnrate so they recalibrate:
//   - planner: over-allocated branches (large surplus, accepted) get leaner next
//     time; the Cause says WHY the surplus arose.
//   - burnrate: reads REALIZED burn (what actually settled), not just the
//     estimate, so the thermostat lands the grant on truth.
//
// Within a grant, accepted surplus banks to the reservoir for the next question.
// All of it is reconstructed FROM THE WAL on replay (the dispositions are stored
// on each grant and rebuilt by applyRecord), so a crash never loses the signal or
// the banked surplus.

// Signal is one settled grant's disposition, surfaced for recalibration.
type Signal struct {
	GrantID  GrantID
	Exit     ExitKind
	Accepted bool
	// Surplus is the surplus this grant BANKED (0 unless accepted — the
	// lexicographic gate's result, reconstructed on replay, never re-evaluated).
	Surplus float64
	// Spent / Synthesized are the realized cost and its modeled portion (issue
	// #23) — burnrate reads realized burn, planner reads where it actually went.
	Spent       float64
	Synthesized float64
	// Cause is why the surplus/exit arose (feeds planner/burnrate). Empty if unset.
	Cause string
	// Fault, if set, is the legible fault disposition (#C1) for a faulted grant.
	Fault *FaultDisposition
}

// Signals returns the disposition of every CLOSED grant — the surplus-signal
// surface planner/burnrate consume. It reflects the live ledger AND a ledger
// rebuilt from the WAL identically (the dispositions are reconstructed on replay).
func (m *Mem) Signals() []Signal {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Signal
	for id, g := range m.grants {
		if id == RootGrant || !g.closed {
			continue
		}
		out = append(out, Signal{
			GrantID:     id,
			Exit:        g.exit,
			Accepted:    g.accepted,
			Surplus:     g.bankedSurplus,
			Spent:       g.spent,
			Synthesized: g.synthesized,
			Cause:       g.cause,
			Fault:       g.fault,
		})
	}
	return out
}

// TotalBankedSurplus is the sum of surplus banked across all settled grants —
// the margin available to fund the next question within the grant (§9). Survives
// replay (reconstructed from the WAL).
func (m *Mem) TotalBankedSurplus() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total float64
	for _, g := range m.grants {
		total += g.bankedSurplus
	}
	return total
}

// SurplusSink receives surplus signals (planner / burnrate implement it). Kept
// minimal: the governor emits Signals; consumers pull or are pushed to. M5 wires
// the pull side; the push wiring into the live planner/burnrate loop is exercised
// by the done-bar test.
type SurplusSink interface {
	// OnSettled is called with a grant's disposition as it settles, so a planner
	// can recalibrate allocation and burnrate can read realized burn.
	OnSettled(Signal)
}
