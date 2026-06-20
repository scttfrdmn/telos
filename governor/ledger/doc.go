// Package ledger is distributed conservation: WAL, surplus accounting, and the
// neutral pool.
//
// In-process the conservation invariant and cancellation are free; across A2A
// the reservation/settlement rides the wire ({grant_id, reservation, deadline,
// cancel_token} out; {result, cost_settled, outcome, child_settlements[]} back)
// and is reconciled via a write-ahead log.
//
// Savings classes are distinct: slack banks only on acceptance; caching banks
// unconditionally. Forfeitures and penalties route to a NEUTRAL pool — never to
// a judge or a winning advocate (architecture §9, §12).
//
// Status: stub. In-mem + WAL lands in M2; distributed reconciliation in M5.
package ledger
