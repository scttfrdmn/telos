// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"context"
	"fmt"
	"sync"

	"github.com/scttfrdmn/telos/acs"
)

// Mem is the M1 conservation-only, in-memory Governor. It enforces the grant
// invariant — Σ(child reservations) ≤ parent remaining, recursively, fails
// closed — and nothing more. No WAL, no admission policy, no burnrate, no
// surplus banking (see the package doc).
//
// It is safe for concurrent use: the gateway's metered loop runs under the
// goroutine fan-out of the agenkit patterns, so Reserve/Settle race.
type Mem struct {
	mu        sync.Mutex
	grants    map[GrantID]*grantState
	nextID    uint64
	wal       *wal        // nil for an in-memory-only governor (no durability)
	sink      SurplusSink // nil if no live surplus consumer; notified on settle/fault (#D1)
	replaying bool        // true during WAL replay — suppresses live sink pushes
}

// WithSink registers a live surplus-signal consumer (planner/burnrate). It is
// notified as grants settle. Pushes are SUPPRESSED during WAL replay — replay
// reconstructs the ledger state, and re-pushing historical signals would
// double-count realized burn at the consumer. Returns m for chaining.
func (m *Mem) WithSink(sink SurplusSink) *Mem {
	m.mu.Lock()
	m.sink = sink
	m.mu.Unlock()
	return m
}

type grantState struct {
	id     GrantID
	parent GrantID
	// reservoir is the grant's allocation (amount over period). The clock is
	// kept on the grant (invariant 4) so Remaining can be reported as a rate.
	reservoir acs.Budget
	escrowed  float64 // sum of child reservations currently outstanding
	spent     float64 // sum settled by this grant's children + its own settle
	// synthesized is the MODELED portion of spent (issue #23): cost the gateway
	// assigned to local work that no provider billed. Tracked separately so a
	// later real-cash reconciliation (M5) can tell true spend from modeled spend;
	// conservation in M2 paces on total spent.
	synthesized float64
	// bankedSurplus is the reward-ledger surplus this grant banked at settlement:
	// (reserved − actual) IF the settling outcome was Accepted, else 0. Distinct
	// from the conservation flow (unspent escrow is always released); this is the
	// rewarded margin that funds the next question (architecture §9).
	bankedSurplus float64
	// cause/exit/accepted record the settle disposition for the surplus signal
	// (#D1) and legible reconstruction on replay. Set once, at settlement.
	cause    string
	exit     ExitKind
	accepted bool
	// fault, if set, is this grant's fault disposition (#C1) — durable + replayable.
	fault  *FaultDisposition
	closed bool // settled or released
}

// New returns a conservation-only governor seeded with a root grant equal to the
// run envelope. Reserve against RootGrant draws from this envelope.
func New(envelope acs.Budget) *Mem {
	m := &Mem{grants: make(map[GrantID]*grantState)}
	m.grants[RootGrant] = &grantState{
		id:        RootGrant,
		parent:    RootGrant, // self; the turtles stop here
		reservoir: envelope,
	}
	return m
}

// remainingLocked computes a grant's unescrowed, unspent reservoir amount.
// Caller holds m.mu.
func (g *grantState) remainingAmount() float64 {
	return g.reservoir.Amount - g.escrowed - g.spent
}

// applyReserve performs the reserve mutation (escrow on parent, create child).
// Shared by Reserve and WAL replay so both reach identical state. Caller holds
// m.mu and has already validated conservation. Bumps nextID past id on replay.
func (m *Mem) applyReserve(id, parent GrantID, req acs.BudgetRequest) *grantState {
	p := m.grants[parent]
	child := &grantState{
		id:        id,
		parent:    parent,
		reservoir: acs.Budget{Amount: req.Amount, Period: req.Period, Currency: p.reservoir.Denomination()},
	}
	m.grants[id] = child
	p.escrowed += req.Amount
	return child
}

// Reserve escrows req against parent, failing closed on conservation breach.
func (m *Mem) Reserve(ctx context.Context, parent GrantID, req acs.BudgetRequest) (*acs.BudgetGrant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("governor: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.grants[parent]
	if !ok {
		return nil, fmt.Errorf("governor: parent grant %q does not exist", parent)
	}
	if p.closed {
		return nil, fmt.Errorf("governor: parent grant %q is closed", parent)
	}

	// Conservation, RATE-aware: the child's period must fit within the parent's
	// clock (a child cannot spend over a longer horizon than its parent has),
	// and the child's amount must fit the parent's remaining reservoir.
	if req.Period > p.reservoir.Period {
		return nil, fmt.Errorf("governor: child period %v exceeds parent grant period %v (rate conservation)", req.Period, p.reservoir.Period)
	}
	avail := p.remainingAmount()
	if req.Amount > avail {
		// Fails closed: reserve nothing on breach.
		return nil, fmt.Errorf("%w: requested %.6f but only %.6f remains in parent %q", errConservation, req.Amount, avail, parent)
	}

	m.nextID++
	id := GrantID(fmt.Sprintf("g%d", m.nextID))

	// Journal BEFORE mutating, so a crash leaves a log that replays to the same
	// state (write-ahead). The record carries the assigned ID so replay is exact.
	if err := m.journal(walRecord{Op: opReserve, ID: id, Parent: parent,
		Amount: req.Amount, Period: req.Period}); err != nil {
		m.nextID-- // un-allocate the id we didn't use
		return nil, err
	}

	child := m.applyReserve(id, parent, req)
	return &acs.BudgetGrant{GrantID: string(id), Budget: child.reservoir}, nil
}

// Settle records actual cost for a grant and closes it, atomically computing and
// banking surplus per the lexicographic objective. The grant's full reservation
// is released from the parent's escrow; the actual cost is charged as spend; and
// surplus = reserved − actual is BANKED to the grant IFF the outcome is Accepted
// (otherwise it banks zero — abandonment, not thrift). "Settle consults the
// verdict and banks atomically" — one conservation point, so the acceptance gate
// cannot be bypassed.
func (m *Mem) Settle(ctx context.Context, id GrantID, actual acs.Cost, outcome Outcome) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settleLocked(id, actual, outcome, true)
}

// settleLocked performs the settlement state transition. wal controls whether the
// action is journaled (false during WAL replay, to avoid re-logging). Caller
// holds m.mu.
func (m *Mem) settleLocked(id GrantID, actual acs.Cost, outcome Outcome, wal bool) error {
	g, ok := m.grants[id]
	if !ok {
		return fmt.Errorf("governor: grant %q does not exist", id)
	}
	if id == RootGrant {
		return fmt.Errorf("governor: cannot settle the root grant")
	}
	// IDEMPOTENT BY GRANT ID (M5 #I1): a grant settles exactly once. Settling an
	// already-closed grant — a live double-settle, a late settlement, or a WAL
	// replay of an already-applied settle — is a NO-OP, not an error and not a
	// re-apply. The first disposition wins; the ledger is left unchanged. The
	// closed-check is BEFORE journaling, so a no-op never appends to the WAL
	// (replay stays bounded and a re-settle can't grow the log). This is the trap
	// that silently corrupts a crash-recovery budget system if gotten wrong:
	// without it, replay double-debits the reservoir and double-banks surplus.
	if g.closed {
		return nil
	}
	if cur := g.reservoir.Denomination(); actual.Denomination() != cur {
		return fmt.Errorf("governor: settle currency %s != grant currency %s", actual.Denomination(), cur)
	}

	if wal {
		if err := m.journal(walRecord{Op: opSettle, ID: id, Amount: actual.Amount,
			Synthesized: actual.Synthesized, Accepted: outcome.Accepted, Cause: outcome.Cause}); err != nil {
			return err
		}
	}

	p := m.grants[g.parent]
	// Release this grant's reservation from the parent's escrow...
	p.escrowed -= g.reservoir.Amount
	// ...and charge the actual cost as spend against the parent reservoir.
	// Over-spend is recorded honestly (not clamped); admission is what prevents it.
	p.spent += actual.Amount
	p.synthesized += actual.Synthesized

	g.spent = actual.Amount
	g.synthesized = actual.Synthesized

	// Surplus = reserved − actual, BANKED only on acceptance. This is the single
	// place the acceptance gate authorizes a reward; it is gated on outcome.Accepted
	// alone and never blended with the amount (lexicographic — see CompareOutcomes).
	surplus := g.reservoir.Amount - actual.Amount
	if surplus > 0 && outcome.Accepted {
		g.bankedSurplus = surplus
	} else {
		g.bankedSurplus = 0
	}

	// Record the settle disposition for the surplus signal (#D1) and for legible
	// reconstruction on replay. cause/exit/accepted are reconstructed identically
	// by replay (the gate result, never re-evaluated unconditionally).
	g.cause = outcome.Cause
	g.exit = outcome.Exit
	g.accepted = outcome.Accepted

	g.closed = true
	m.notify(g)
	return nil
}

// notify pushes a settled grant's disposition to the live surplus sink (#D1).
// Suppressed during replay: replay reconstructs ledger state, and re-emitting
// historical signals would double-count realized burn at the consumer. Caller
// holds m.mu; the sink must not call back into the governor (would deadlock).
func (m *Mem) notify(g *grantState) {
	if m.sink == nil || m.replaying {
		return
	}
	m.sink.OnSettled(Signal{
		GrantID: g.id, Exit: g.exit, Accepted: g.accepted,
		Surplus: g.bankedSurplus, Spent: g.spent, Synthesized: g.synthesized,
		Cause: g.cause, Fault: g.fault,
	})
}

// Release closes a grant with no charge, returning its full reservation to the
// parent's available reservoir.
func (m *Mem) Release(ctx context.Context, id GrantID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releaseLocked(id, true)
}

// releaseLocked performs the release state transition. wal controls journaling
// (false during replay). Caller holds m.mu.
func (m *Mem) releaseLocked(id GrantID, wal bool) error {
	g, ok := m.grants[id]
	if !ok {
		return fmt.Errorf("governor: grant %q does not exist", id)
	}
	if id == RootGrant {
		return fmt.Errorf("governor: cannot release the root grant")
	}
	// Idempotent by GrantID (#I1): releasing an already-closed grant — including a
	// replay of an applied release — is a no-op. First disposition wins (a grant
	// that was settled is not later "released"; a released grant is not re-released).
	if g.closed {
		return nil
	}
	if wal {
		if err := m.journal(walRecord{Op: opRelease, ID: id}); err != nil {
			return err
		}
	}
	m.grants[g.parent].escrowed -= g.reservoir.Amount
	g.closed = true
	return nil
}

// Remaining reports a grant's unescrowed, unspent reservoir as a Budget (amount
// over the grant's period — a rate, never a bare total). An unknown or closed
// grant reports a zero-amount budget over its period (or a zero budget if
// unknown).
func (m *Mem) Remaining(id GrantID) acs.Budget {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[id]
	if !ok {
		return acs.Budget{}
	}
	amt := g.remainingAmount()
	if amt < 0 {
		amt = 0
	}
	return acs.Budget{Amount: amt, Period: g.reservoir.Period, Currency: g.reservoir.Denomination()}
}

// BankedSurplus reports the surplus a settled grant banked (reserved − actual on
// acceptance; zero otherwise). Unknown/unsettled grants report 0.
func (m *Mem) BankedSurplus(id GrantID) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.grants[id]; ok {
		return g.bankedSurplus
	}
	return 0
}

// Spent reports the total cost charged against a grant by its settled children
// plus its own settlement. SyntheticSpent is the MODELED portion of that (issue
// #23) — kept distinguishable so real-cash reconciliation can recover true spend.
func (m *Mem) Spent(id GrantID) (total, synthesized float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.grants[id]; ok {
		return g.spent, g.synthesized
	}
	return 0, 0
}

var _ Governor = (*Mem)(nil)
