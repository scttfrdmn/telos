// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package compute is the CORE-side seam for synthesized computation (§8). It is
// the interface the gateway's RunWork path drives; the concrete AWS implementation
// lives in the separate module telos/substrate/sporehost (which carries the
// spawn/truffle/AWS-SDK dependency tree). The core module references compute ONLY
// through these interfaces, so the ~200-module AWS tree never enters the core
// module graph (invariant: core stays light; the substrate module is heavy).
//
// This mirrors cohort's provider seam in spirit — launch / observe / settle a
// named unit — but in Telos's own vocabulary, because the core cannot import
// cohort's EC2-flavored types either. The sporehost module adapts between these
// and cohort/spawn.
package compute

import (
	"context"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

// Spec is a synthesized-compute request — the core-visible projection of a
// gateway.WorkloadSpec, carrying only what the launcher needs and nothing
// AWS-specific. The launcher (sporehost) translates it into an aws.LaunchConfig.
type Spec struct {
	// EntityID is the stable name of this unit of compute (becomes the instance's
	// Name tag and the cohort EntityID). The orphan-detection guard keys on it.
	EntityID string

	// IdempotencyToken is deterministic in (run, entity, generation) — passed as
	// the RunInstances ClientToken so a relaunch after a crash returns the existing
	// instance rather than duplicating it (the orphan guard). Empty = launcher
	// derives one.
	IdempotencyToken string

	// Rung describes the placement (instance class / GPU / spot) abstractly — the
	// launcher maps it to a concrete instance type. No AWS vocabulary here.
	Rung Rung

	// EstimatedDuration is the worst-case wall-clock the gateway escrows against.
	EstimatedDuration time.Duration

	// Lifecycle is the self-destruct contract Telos sets at launch and then only
	// observes (spore.host's grain): TTL, idle, cost-limit, FSx lifetime.
	Lifecycle Lifecycle

	// DataRef / ResultRef are state-by-reference (S3/FSx paths), never inlined.
	DataRef   acs.StateRef
	ResultRef acs.StateRef

	// SpotWebhookURL / Correlation wire the in-window spot-reclamation push
	// (spawn v0.63.0): the launcher sets these at launch; Correlation is Telos's
	// own opaque key (the cohort entity/grant), echoed verbatim by spored.
	SpotWebhookURL string
	Correlation    string
}

// Rung is the abstract placement of a compute unit — agent-runtime vocabulary,
// not cloud. The launcher resolves it to an instance type + spot/on-demand.
type Rung struct {
	Class string // e.g. "cpu" | "gpu" | "highmem" — launcher maps to instance family
	GPU   bool
	Spot  bool
}

// Lifecycle is the launch-time self-destruct policy. The on-node daemon enforces
// it; Telos observes the result. These map to spawn LaunchConfig tag fields.
type Lifecycle struct {
	TTL         time.Duration // hard deadline → deadline exit
	IdleTimeout time.Duration // self-stop when idle → early completion (banks surplus)
	CostLimit   float64       // self-terminate on $ ceiling → exhaustion
	FSx         FSxLifecycle  // perishable-rectangle storage (its own ledger line)
}

// FSxLifecycle is the perishable-rectangle storage contract (§8): a durable
// filesystem bills hourly independent of any instance, so its lifetime is explicit
// and governed separately. Zero value = no FSx.
type FSxLifecycle struct {
	Mode string        // "" (none) | "ephemeral" (per-run) | "durable" (reused)
	TTL  time.Duration // required when Mode == "durable"
}

// State is the core-visible lifecycle state of a compute unit (the projection of
// cohort.LifecycleState / EC2 state the Observer reports).
type State string

const (
	StateUnknown   State = "unknown"   // not yet observed / lag (never "absent" on a fresh launch)
	StateLaunching State = "launching" // launch acked, not yet running
	StateRunning   State = "running"   // provider reports running
	StateStopped   State = "stopped"   // self-stopped (idle) or deliberately stopped
	StateDone      State = "done"      // completed
	StateFailed    State = "failed"    // terminal
)

// Disposition is WHY a post-Ready unit changed state — the poll-and-infer result
// the Observer derives from tags (§8): cohort has no "self-retired" phase, so the
// launcher disambiguates an autonomous stop/terminate into one of these.
type Disposition string

const (
	DispositionRunning   Disposition = "running"      // still going
	DispositionIdleStop  Disposition = "idle-stop"    // → early completion, banks surplus
	DispositionTTL       Disposition = "ttl-fire"     // → deadline exit
	DispositionCostLimit Disposition = "cost-limit"   // → exhaustion
	DispositionSpot      Disposition = "spot-reclaim" // → fault
	DispositionComplete  Disposition = "complete"     // → done (on-complete fired)
)

// Observation is one observed compute unit, with its disposition reason and the
// accrued cost basis (compute-seconds the launcher read from a tag, etc.).
type Observation struct {
	EntityID       string
	State          State
	Disposition    Disposition
	ProviderID     string // e.g. instance id
	ComputeSeconds int64  // accrued, from the spawn:compute-seconds tag (modeled-cost basis)
	Reason         string // verbatim provider/disposition detail, for legibility
}

// Launcher is the core-side compute provider seam (§8). The sporehost module's
// concrete implementation generalizes spawn/pkg/mpicohort over cohort; the core
// only ever sees this interface, so it never imports the AWS-SDK tree.
type Launcher interface {
	// Launch starts one compute unit. Idempotent by Spec.IdempotencyToken: a
	// relaunch after a crash returns the existing unit, never a duplicate (the
	// orphan guard). Returns the initial observation.
	Launch(ctx context.Context, spec Spec) (Observation, error)

	// Observe polls the current state + disposition of the named units. It is
	// poll-and-infer: a post-Ready state change is interpreted against tags into a
	// Disposition. Unseen ids report StateUnknown (lag, not absence).
	Observe(ctx context.Context, ids []string) ([]Observation, error)

	// Terminate destroys a unit. Idempotent.
	Terminate(ctx context.Context, id string) error

	// Reconcile detects ORPHANS: units running under Telos's tag that the caller's
	// ledger no longer accounts for (a crash left them billing). It returns the
	// unmatched live units so the caller can terminate or adopt them. `known` is
	// the set of entity ids the ledger believes are live.
	Reconcile(ctx context.Context, known []string) ([]Observation, error)
}

// Pricer is the frontier/discovery seam (truffle): forward $/hr for a rung, so the
// gateway can estimate before launch. Read-only; no mutation.
type Pricer interface {
	// EstimateHourly returns the estimated $/hr for a rung (spot or on-demand),
	// for the gateway's worst-case escrow. It is an ESTIMATE, never billed truth.
	EstimateHourly(ctx context.Context, rung Rung) (float64, error)
}
