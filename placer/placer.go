// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package placer maps a bound ACS node to a transport rung. Transport is a
// PLACEMENT DECISION, not a code change (invariant 6): the same agenkit object
// becomes a goroutine or an A2A session depending on the node's Trust and
// Gravity — never its cost alone.
//
// The decision is first-trigger-wins along the transport ladder (architecture
// §7): goroutine is the default; the first trigger that fires (isolation,
// untrusted input, resource gravity) promotes the node to the A2A-session rung.
// The justification bar rises per rung. The spore.host INSTANCE rung is M6/M7 —
// it appears in the ladder but the placer does not yet select it.
//
// The decision is INSPECTABLE: Place returns a Decision recording which rung was
// chosen and which trigger fired, so a human can audit placement the way M2's
// burn-rate curve and M3's scoping expansion are auditable.
package placer

import (
	"context"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/transport"
)

// Placer maps a node to a transport rung (architecture §5).
type Placer interface {
	Place(ctx context.Context, n *acs.Node) (Decision, error)
}

// Decision is the auditable result of placing a node: the selected rung, the
// transport.Placement (the cohort.Placement carrying the fallback ladder from
// that rung), the substrate that fills it, and WHY the rung was chosen.
type Decision struct {
	Rung      transport.Rung
	Placement transport.Placement
	Substrate string // "inproc" (goroutine) | "agentcore" (a2a-session)
	Trigger   string // the first trigger that fired, or "default"
	// acs() renders this into an acs.Placement annotation on the node.
}

// AsACS renders the decision into the node-annotation form (acs.Placement).
func (d Decision) AsACS() acs.Placement {
	t := acs.TransportGoroutine
	switch d.Rung {
	case transport.RungA2ASession:
		t = acs.TransportA2A
	case transport.RungInstance:
		t = acs.TransportInstance
	}
	return acs.Placement{Transport: t, Substrate: d.Substrate}
}

// FirstTrigger is the §7 placer: goroutine by default, promoted to the A2A
// session rung by the first trigger that fires. The triggers, in evaluation
// order (first wins):
//
//  1. Resource gravity — data/model/compute gravity (GPU-in-process, huge memory,
//     a local model, sovereign data, heavy synthesized compute) demands the
//     INSTANCE rung (spore.host), the third transport (§7, M6). This is checked
//     first because it is the strongest pull: a GPU job can't run in a goroutine
//     or a shared session.
//  2. Trust isolation/untrusted — an isolated or hostile-input node with no
//     resource gravity needs its own session envelope (the A2A rung), not the
//     parent's goroutine tree.
//
// A node with neither trigger stays on the goroutine rung (the default).
// First-trigger-wins, gravity before trust: a GPU+isolated node lands on an
// instance (which is also isolated), not merely an A2A session.
type FirstTrigger struct{}

// New returns the first-trigger-wins placer.
func New() *FirstTrigger { return &FirstTrigger{} }

// Place chooses a rung for the node (first-trigger-wins, §7).
func (p *FirstTrigger) Place(ctx context.Context, n *acs.Node) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	rung, trigger := transport.RungGoroutine, "default"

	// Trigger 1 (strongest): resource gravity → the INSTANCE rung (spore.host).
	// GPU-in-process / huge memory / local model / sovereign data / heavy compute
	// can't run in a goroutine or a shared session — it needs its own instance (§7,
	// the third transport, M6).
	if n.Gravity != "" && n.Gravity != acs.GravityNone {
		rung, trigger = transport.RungInstance, "gravity:"+string(n.Gravity)
	}

	// Trigger 2: trust boundary (only if gravity didn't already promote —
	// first-trigger-wins). An isolated/untrusted node with no resource gravity
	// needs its own session envelope (the A2A rung), not an instance.
	if rung == transport.RungGoroutine {
		switch n.Trust {
		case acs.TrustIsolated:
			rung, trigger = transport.RungA2ASession, "trust:isolated"
		case acs.TrustUntrusted:
			rung, trigger = transport.RungA2ASession, "trust:untrusted"
		}
	}

	return Decision{
		Rung:      rung,
		Placement: transport.NewPlacement(rung, transport.DefaultLadder),
		Substrate: substrateFor(rung),
		Trigger:   trigger,
	}, nil
}

// substrateFor maps a rung to the substrate adapter name that fills it.
func substrateFor(r transport.Rung) string {
	switch r {
	case transport.RungA2ASession:
		return "agentcore"
	case transport.RungInstance:
		return "sporehost" // the compute substrate (M6): GPU / huge-mem / sovereign / heavy compute
	default:
		return "inproc"
	}
}

var _ Placer = (*FirstTrigger)(nil)
