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
	mu     sync.Mutex
	grants map[GrantID]*grantState
	nextID uint64
}

type grantState struct {
	id     GrantID
	parent GrantID
	// reservoir is the grant's allocation (amount over period). The clock is
	// kept on the grant (invariant 4) so Remaining can be reported as a rate.
	reservoir acs.Budget
	escrowed  float64 // sum of child reservations currently outstanding
	spent     float64 // sum settled by this grant's children + its own settle
	closed    bool    // settled or released
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
	child := &grantState{
		id:        id,
		parent:    parent,
		reservoir: acs.Budget{Amount: req.Amount, Period: req.Period, Currency: p.reservoir.Denomination()},
	}
	m.grants[id] = child
	p.escrowed += req.Amount

	return &acs.BudgetGrant{GrantID: string(id), Budget: child.reservoir}, nil
}

// Settle records actual cost for a grant and closes it. The grant's full
// reservation is released from the parent's escrow, and the actual cost is
// debited from the parent's reservoir as spend.
func (m *Mem) Settle(ctx context.Context, id GrantID, actual acs.Cost, outcome Outcome) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	g, ok := m.grants[id]
	if !ok {
		return fmt.Errorf("governor: grant %q does not exist", id)
	}
	if g.closed {
		return fmt.Errorf("governor: grant %q already closed", id)
	}
	if id == RootGrant {
		return fmt.Errorf("governor: cannot settle the root grant")
	}
	if cur := g.reservoir.Denomination(); actual.Denomination() != cur {
		return fmt.Errorf("governor: settle currency %s != grant currency %s", actual.Denomination(), cur)
	}

	p := m.grants[g.parent]
	// Release this grant's reservation from the parent's escrow...
	p.escrowed -= g.reservoir.Amount
	// ...and charge the actual cost as spend against the parent reservoir.
	// Note: M1 does NOT clamp actual to the reservation. Over-spend is recorded
	// honestly here; the admission policy that would have prevented it is M2.
	p.spent += actual.Amount

	g.spent = actual.Amount
	g.closed = true
	return nil
}

// Release closes a grant with no charge, returning its full reservation to the
// parent's available reservoir.
func (m *Mem) Release(ctx context.Context, id GrantID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	g, ok := m.grants[id]
	if !ok {
		return fmt.Errorf("governor: grant %q does not exist", id)
	}
	if g.closed {
		return fmt.Errorf("governor: grant %q already closed", id)
	}
	if id == RootGrant {
		return fmt.Errorf("governor: cannot release the root grant")
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

var _ Governor = (*Mem)(nil)
