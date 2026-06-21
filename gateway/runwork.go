// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/governor"
)

// RunWork runs one synthesized computation through the chokepoint (§8), mirroring
// the model-call metered loop:
//
//	estimate (frontier price × est. duration)
//	  → governor.Reserve(escrow)            // fails closed on conservation breach
//	  → launcher.Launch (cohort-reconciled, via substrate/sporehost)
//	  → observe to a terminal disposition   // poll-and-infer, off-node self-governed
//	  → meter ACTUAL (elapsed/compute-seconds × rate), SYNTHESIZED (modeled, not billed)
//	  → governor.Settle (surplus banks only on acceptance, via the WorkResult.Record)
//
// No agent gets raw launch access: this is the only path to a launch (invariant 5,
// same seal as Invoke). Compute cost is ESTIMATED — there is no live billing handle
// — and tagged synthesized so burn-rate can tell modeled compute from measured
// model spend (issue #23). The acceptance verdict over WorkResult.Record is the
// caller's responsibility (an unattested result fails acceptance, §8/M3); RunWork
// settles the SPEND grant-rate and reports the disposition.
func (c *Chokepoint) RunWork(ctx context.Context, spec WorkloadSpec) (WorkResult, acs.Cost, error) {
	if err := ctx.Err(); err != nil {
		return WorkResult{}, acs.Cost{}, err
	}
	if c.launcher == nil || c.pricer == nil {
		return WorkResult{}, acs.Cost{}, ErrComputePathNotImplemented
	}

	// 1. Estimate worst-case: hourly rate (truffle frontier) × estimated duration.
	hourly, err := c.pricer.EstimateHourly(ctx, spec.Rung)
	if err != nil {
		return WorkResult{}, acs.Cost{}, fmt.Errorf("gateway: compute estimate: %w", err)
	}
	worstCase := hourly * spec.EstimatedDuration.Hours()

	// 2. Reserve the worst-case against the parent grant. FAILS CLOSED.
	parent := ParentGrant(ctx)
	grant, err := c.gov.Reserve(ctx, parent, acs.BudgetRequest{
		Amount: worstCase, Period: reservePeriodFor(spec.EstimatedDuration, c.reservePeriod),
	})
	if err != nil {
		if errors.Is(err, governor.ErrConservation) {
			return WorkResult{}, acs.Cost{}, fmt.Errorf("%w: %v", ErrReservationDenied, err)
		}
		return WorkResult{}, acs.Cost{}, fmt.Errorf("gateway: compute reserve: %w", err)
	}
	gid := governor.GrantID(grant.GrantID)

	// 3. Launch through the substrate (cohort-reconciled). The escrow is journaled
	//    by Reserve BEFORE this, so a crash here leaves an open escrow the orphan
	//    reconciler matches to the live instance (the M6 trap guard).
	if _, err := c.launcher.Launch(ctx, specToCompute(spec)); err != nil {
		// Launch faulted before incurring metered compute: release the escrow.
		_ = c.gov.Release(context.Background(), gid)
		return WorkResult{}, acs.Cost{}, fmt.Errorf("gateway: compute launch: %w", err)
	}

	// 4. Observe to a terminal disposition. The Observer is poll-and-infer; the
	//    on-node daemon self-governs, so we wait for a terminal state and read WHY
	//    from the disposition. (A bounded poll loop; ctx cancel is the kill-switch.)
	obs, err := c.awaitTerminal(ctx, spec.EntityID, gid, worstCase, hourly)
	if err != nil {
		// Could not observe a terminal disposition (ctx cancelled / observe error).
		// Release the escrow; the orphan reconciler is the backstop for a live unit.
		_ = c.gov.Release(context.Background(), gid)
		return WorkResult{}, acs.Cost{}, fmt.Errorf("gateway: compute observe: %w", err)
	}

	// 5. Meter ACTUAL from accrued compute-seconds × rate — SYNTHESIZED (modeled,
	//    no provider bill for the wall-clock; the gateway owns the estimate).
	actual := c.meterCompute(obs, hourly)

	// 6. Settle grant-rate. The four-exit mapping flows from the disposition; the
	//    acceptance gate (surplus banks iff accepted) is enforced by the governor
	//    from the Outcome the caller supplies after judging WorkResult.Record. Here
	//    we settle the SPEND with the disposition-derived exit; Accepted defaults
	//    false (compute spend is conserved; surplus on acceptance is settled when
	//    the caller renders the verdict — same atomic point as M2).
	outcome := governor.Outcome{Exit: exitForDisposition(obs.Disposition), Cause: obs.Reason}
	if err := c.gov.Settle(ctx, gid, actual, outcome); err != nil {
		return WorkResult{}, acs.Cost{}, fmt.Errorf("gateway: compute settle: %w", err)
	}

	// FSx is its OWN ledger line (§8/M6 #67) — the perishable rectangle's hourly
	// cost, attributed apart from instance cost. Billed for the run's wall-clock
	// (ephemeral) or at least its TTL (durable). Reported on the result; NOT folded
	// into `actual` (the compute meter), so a durable filesystem's accrual is visible.
	billed := time.Duration(obs.ComputeSeconds) * time.Second
	fsxLine := fsxLineFor(spec.Lifecycle.FSx, billed, 0, c.computeCurrency)

	return WorkResult{
		EntityID:       spec.EntityID,
		ResultRef:      spec.ResultRef,
		Disposition:    obs.Disposition,
		ComputeSeconds: obs.ComputeSeconds,
		Record:         acceptanceRecordFor(spec, obs),
		FSx:            fsxLine,
	}, actual, nil
}

// meterCompute turns accrued compute-seconds (or, if zero, the estimate) into a
// SYNTHESIZED cost — modeled, not billed (issue #23). burn-rate must be able to
// tell this from measured model spend, so the whole amount is Synthesized.
func (c *Chokepoint) meterCompute(obs compute.Observation, hourly float64) acs.Cost {
	hours := float64(obs.ComputeSeconds) / 3600.0
	amount := hours * hourly
	return acs.SynthesizedCost(amount, c.computeCurrency, acs.TokenUsage{})
}

// awaitTerminal polls the launcher until the unit reaches a terminal disposition
// or ctx is cancelled. Bounded by ctx; the on-node daemon owns the real lifetime.
//
// THE ESCROW GAP (§3, M6 #63): we reserved an ESTIMATE but settle observed
// wall-clock. A long job can overrun its estimate MID-RUN. On each poll we check
// accrued (compute-seconds × rate) against the reservation; when it approaches the
// reservation, we re-authorize the delta against the parent grant — the
// change-order path firing mid-run, not silent overspend. If the grant can't fund
// the delta, the re-authorization fails closed; the on-node CostLimit tag is the
// backstop that terminates the instance (→ exhaustion exit). The re-authorization
// POLICY (auto vs escalate-to-human, threshold) is the ReAuthorizer seam — surfaced,
// not hardcoded (touches the §15 admission forks).
func (c *Chokepoint) awaitTerminal(ctx context.Context, id string, gid governor.GrantID, reserved, hourly float64) (compute.Observation, error) {
	ticker := time.NewTicker(computePollInterval)
	defer ticker.Stop()
	authorized := reserved // how much is currently escrowed for this unit
	for {
		obs, err := c.launcher.Observe(ctx, []string{id})
		if err != nil {
			return compute.Observation{}, err
		}
		if len(obs) == 1 {
			o := obs[0]
			if isTerminalDisposition(o.Disposition) {
				return o, nil
			}
			// Escrow-gap check: has accrued spend approached the authorization?
			accrued := float64(o.ComputeSeconds) / 3600.0 * hourly
			if accrued >= authorized*reauthorizeThreshold {
				more, err := c.reauthorize(ctx, gid, authorized, hourly)
				if err != nil {
					// Re-authorization failed closed (grant can't fund the overrun).
					// Do not kill here — the on-node CostLimit terminates the instance;
					// we keep observing and will settle the resulting exhaustion exit.
					_ = err
				} else {
					authorized += more
				}
			}
		}
		select {
		case <-ctx.Done():
			return compute.Observation{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// reauthorizeThreshold is the fraction of the current authorization at which a
// mid-run change-order is triggered (re-reserve before the estimate is fully
// consumed, so the work isn't starved at the boundary).
const reauthorizeThreshold = 0.9

// reauthorize is the mid-run change-order (§3): extend the escrow for a running
// unit by one more reservation-period of runway. In M6 the policy is "auto-extend
// by the original estimate if the grant can fund it" via a Reserve against the
// parent that adds to this unit's grant; it FAILS CLOSED when the grant can't.
// The auto-vs-escalate policy is deferred (§15 admission fork) — this is the
// mechanism that makes overrun a change-order, not a silent overspend.
func (c *Chokepoint) reauthorize(ctx context.Context, gid governor.GrantID, current, hourly float64) (float64, error) {
	// Extend by one reservePeriod of runway at the current rate.
	delta := hourly * c.reservePeriod.Hours()
	if delta <= 0 {
		delta = current // fall back to doubling the runway
	}
	parent := ParentGrant(ctx)
	if _, err := c.gov.Reserve(ctx, parent, acs.BudgetRequest{Amount: delta, Period: c.reservePeriod}); err != nil {
		return 0, fmt.Errorf("gateway: mid-run re-authorization failed closed: %w", err)
	}
	return delta, nil
}

// computePollInterval is the Observer poll cadence for awaiting a terminal compute
// disposition. Small for tests; a real run tunes it up.
var computePollInterval = 50 * time.Millisecond

func isTerminalDisposition(d compute.Disposition) bool {
	switch d {
	case compute.DispositionIdleStop, compute.DispositionTTL, compute.DispositionCostLimit,
		compute.DispositionSpot, compute.DispositionComplete:
		return true
	}
	return false
}

// exitForDisposition maps a compute lifecycle disposition to a four-exit kind (§8):
// idle-stop → done (early completion banks surplus on acceptance); ttl → exhausted
// (deadline hit the wall); cost-limit → exhausted; spot → negative-as-fault handled
// via the fault path, but a settle here records exhausted; complete → done.
func exitForDisposition(d compute.Disposition) governor.ExitKind {
	switch d {
	case compute.DispositionIdleStop, compute.DispositionComplete:
		return governor.ExitDone
	case compute.DispositionTTL, compute.DispositionCostLimit, compute.DispositionSpot:
		return governor.ExitExhausted
	default:
		return governor.ExitDone
	}
}

// reservePeriodFor bounds the reservation horizon to at least the estimated
// duration (a long job reserves over its own clock, not a 1-minute call slot),
// keeping the reservation grant-rate-shaped (invariant 4).
func reservePeriodFor(est, floor time.Duration) time.Duration {
	if est > floor {
		return est
	}
	return floor
}

// acceptanceRecordFor builds the acceptance Record the verdict judges. A compute
// result threads PROVENANCE (claim → computation → staged input → transform →
// source); an UNATTESTED result (no Sources) fails acceptance exactly as an
// unprovenanced literature claim does (§8/M3 — acceptance rejects empty Sources).
// The result direction is positive (a produced result) unless the run faulted.
func acceptanceRecordFor(spec WorkloadSpec, obs compute.Observation) acceptance.Record {
	dir := acceptance.DirectionPositive
	if obs.Disposition == compute.DispositionSpot {
		dir = acceptance.DirectionInconclusive // a reclaimed run produced no settled result
	}
	return acceptance.Record{
		NodeID:    spec.EntityID,
		Content:   string(spec.ResultRef),
		Direction: dir,
		Sources:   spec.Provenance, // empty → unattested → fails acceptance
		// Reproduced: a computation that reproduces is oracle-grade; left false
		// here (the caller/attestation layer sets it when a re-run is verified).
	}
}

// specToCompute projects a gateway WorkloadSpec onto the core compute.Spec the
// launcher consumes (no AWS vocabulary crosses this boundary).
func specToCompute(s WorkloadSpec) compute.Spec {
	return compute.Spec{
		EntityID:          s.EntityID,
		IdempotencyToken:  s.IdempotencyToken,
		Rung:              s.Rung,
		EstimatedDuration: s.EstimatedDuration,
		Lifecycle:         s.Lifecycle,
		DataRef:           s.DataRef,
		ResultRef:         s.ResultRef,
		SpotWebhookURL:    s.SpotWebhookURL,
		Correlation:       s.Correlation,
	}
}
