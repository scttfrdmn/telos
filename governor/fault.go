// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"fmt"

	"github.com/spore-host/cohort"
)

// Fault-Record through the WAL (M5 #C1). In M4 a dead session returned a
// structured cohort Record in the same synchronous breath as the settle. M5
// makes a fault disposition a FIRST-CLASS, WAL-persisted, replayable thing: a
// session can die, its fault is journaled, and after a crash-and-replay the
// legible disposition is reproduced AND its grant is settled exactly once
// against the parent rebuilt from the log.
//
// This composes M4's legibility (a real disposition, never a bare error) with
// M2's durability (the WAL): replaying the log must reproduce the legible Record.

// FaultDisposition is the durable, replayable form of a fault on one grant's
// entity. It mirrors the legible parts of a cohort.Fault so Summary/Explain can
// be reconstructed after a crash without the original error value.
type FaultDisposition struct {
	GrantID GrantID
	Class   string // cohort.FaultClass string (e.g. "terminal")
	Code    string // verbatim provider code, preserved for legibility
	Message string
}

// RecordFault journals a fault for a grant and settles that grant as a fault
// outcome: no surplus banks (it did not succeed), the escrow is released back to
// the parent (the work did not consume budget it must pay for — a launch that
// never ran is not a charge), and the disposition is stored for legible replay.
// Idempotent by GrantID, like Settle/Release: a fault recorded twice (or replayed)
// is a no-op.
func (m *Mem) RecordFault(ctx context.Context, id GrantID, f cohort.Fault) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.faultLocked(FaultDisposition{
		GrantID: id, Class: f.Class.String(), Code: f.Code, Message: f.Message,
	}, true)
}

// faultLocked applies a fault disposition. wal controls journaling (false on
// replay). Caller holds m.mu.
func (m *Mem) faultLocked(d FaultDisposition, wal bool) error {
	g, ok := m.grants[d.GrantID]
	if !ok {
		return fmt.Errorf("governor: fault for absent grant %q", d.GrantID)
	}
	if d.GrantID == RootGrant {
		return fmt.Errorf("governor: cannot fault the root grant")
	}
	// Idempotent (#I1): a fault on an already-closed grant is a no-op. The first
	// disposition wins — a grant that already settled successfully is not later
	// "faulted," and a fault is not re-applied on replay.
	if g.closed {
		return nil
	}
	if wal {
		if err := m.journal(walRecord{Op: opFault, ID: d.GrantID,
			FaultClass: d.Class, FaultCode: d.Code, FaultMsg: d.Message}); err != nil {
			return err
		}
	}
	// Release the escrow back to the parent (no charge for work that faulted
	// before incurring metered cost) and record the disposition.
	if p := m.grants[g.parent]; p != nil {
		p.escrowed -= g.reservoir.Amount
	}
	g.fault = &d
	g.exit = ExitExhausted // a fault is the lowest-reward exit; no surplus
	g.accepted = false
	g.bankedSurplus = 0
	g.cause = d.FaultSummary()
	g.closed = true
	m.notify(g)
	return nil
}

// applyFaultLocked replays a journaled fault (wal=false).
func (m *Mem) applyFaultLocked(rec walRecord, wal bool) error {
	return m.faultLocked(FaultDisposition{
		GrantID: rec.ID, Class: rec.FaultClass, Code: rec.FaultCode, Message: rec.FaultMsg,
	}, wal)
}

// Fault returns a grant's fault disposition, or nil if it did not fault. Survives
// replay — reconstructed from the WAL — so the legible disposition is available
// after a crash (#C1).
func (m *Mem) Fault(id GrantID) *FaultDisposition {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.grants[id]; ok {
		return g.fault
	}
	return nil
}

// FaultSummary renders a one-line legible disposition for a faulted grant —
// reproduced identically after replay (cohort's legibility rule survives recovery).
func (d FaultDisposition) FaultSummary() string {
	return fmt.Sprintf("%s/%s: %s", d.Class, d.Code, d.Message)
}
