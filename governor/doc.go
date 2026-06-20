// Package governor is admission, conservation, and the surplus objective —
// ASBB retargeted at agents and compute, made grant-aware.
//
// INVARIANT 4 (unrecoverable if wrong): the governor is grant-RATE-aware, not
// total-budget-aware. The conserved quantity is dollars-over-period. ASBB was
// always a spend-RATE admission controller; the grant's amount-over-period is
// the rate it governs. Nothing here may reduce a grant to a bare total. See
// acs.Budget, which carries amount AND period for exactly this reason.
//
// Conservation: Σ(child) ≤ parent remaining; fails closed; recurses; max depth.
// Objective is lexicographic: (1) attested acceptance, then (2) maximize
// surplus — never a weighted sum (that admits an under-deliver exploit). Surplus
// banks only on an accepted result.
//
// The interface (architecture §5):
//
//	Admit(ctx, *Quote, Reservoir, Clock) (Admission, error) // rate-aware
//	Reserve(ctx, parent GrantID, BudgetRequest) (*BudgetGrant, error)
//	Settle(ctx, GrantID, actual Cost, Outcome) error        // surplus iff Accepted
//	Release(ctx, GrantID) error
//	Remaining(GrantID) Budget
//
// Status: stub. Lands in M2 (flat per-run admission, conservation on spawns,
// in-mem + WAL ledger, burnrate default-standard).
package governor
