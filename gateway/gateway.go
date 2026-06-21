// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package gateway is the one work chokepoint (invariant 5).
//
// No agent gets raw model or compute access. Every metered unit — a model call
// or a synthesized computation — routes, escrows, meters, and settles here. It
// is the only place local models and off-platform compute can be metered: a
// local model returns tokens but no bill, so its cost is SYNTHESIZED here. If
// metering lived at the model, local work would meter as free.
//
// The defining property: a caller hands the gateway a capability-bound
// acs.ModelBinding and a request, and gets back a response plus a metered cost.
// It cannot tell whether the model ran on Bedrock or on a local endpoint. That
// indistinguishability is the gateway's reason to exist (M1 proves it).
package gateway

import (
	"context"

	"time"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
)

// Gateway is the chokepoint for all metered work (architecture §5).
type Gateway interface {
	// Invoke runs one model call: estimate worst-case cost, escrow it with the
	// governor, route to the backend named by the binding, invoke, meter the
	// ACTUAL cost (synthesizing it for local backends), and settle. The caller
	// cannot tell which backend served the call.
	Invoke(ctx context.Context, binding acs.ModelBinding, req ModelRequest) (ModelResponse, acs.Cost, error)

	// RunWork runs one synthesized computation through the chokepoint (§8): estimate
	// (frontier price × duration) → escrow → launch via the compute.Launcher →
	// meter elapsed×rate → settle. Cost is ESTIMATED, not billed, and tagged
	// synthesized (modeled, not metered-truth). No agent gets raw launch access —
	// the only path to a launch is here (invariant 5, same seal as model calls).
	RunWork(ctx context.Context, spec WorkloadSpec) (WorkResult, acs.Cost, error)
}

// ModelRequest is a backend-agnostic model call. It carries no provider or model
// name — the acs.ModelBinding passed to Invoke decides routing — so the request
// shape is identical regardless of where the model runs.
type ModelRequest struct {
	// Messages is the conversation, in agenkit's message form.
	Messages []*agenkit.Message

	// MaxTokens bounds the output. The metered loop uses it for the WORST-CASE
	// estimate (output ← MaxTokens) that is escrowed before the call. Zero means
	// the gateway applies a default ceiling.
	MaxTokens int

	// Temperature is optional sampling temperature; nil leaves the backend default.
	Temperature *float64

	// Cache requests prompt caching when the backend supports it. Cache-aware
	// billing (warm prefix at cache rate) depends on this and on the backend
	// reporting cache tokens (best-effort; see agenkit#665).
	Cache bool
}

// ModelResponse is the backend-agnostic result. Every field is populated the
// same way regardless of backend; nothing here lets a caller distinguish Bedrock
// from local. The Backend tag exists for METERING and tests only and is not a
// behavioral difference (it does not change control flow above the gateway).
type ModelResponse struct {
	// Message is the model's reply.
	Message *agenkit.Message

	// Usage is the normalized token usage the cost was computed from.
	Usage acs.TokenUsage

	// CacheHit reports whether a warm prefix was served at the cache rate.
	CacheHit bool

	// Backend names which backend served the call (e.g. "bedrock", "ollama").
	// Diagnostic/metering only — see the type doc.
	Backend string
}

// WorkloadSpec is a synthesized-compute request (architecture §8): a
// ComputeSynthesis agent FRAMES the requirement (method, data-by-ref, resources,
// lifecycle), it does not write execution code. The gateway estimates+escrows it,
// the compute.Launcher runs it, the gateway meters the result.
type WorkloadSpec struct {
	// EntityID names this unit of compute (instance Name tag / cohort EntityID /
	// orphan-guard key). IdempotencyToken makes a relaunch-after-crash safe.
	EntityID         string
	IdempotencyToken string

	// Rung is the abstract placement (cpu/gpu/highmem, spot?) — the launcher maps
	// it to a concrete instance type. No cloud vocabulary in the spec.
	Rung compute.Rung

	// EstimatedDuration is the worst-case wall-clock the gateway escrows against.
	EstimatedDuration time.Duration

	// Lifecycle is the launch-time self-destruct contract Telos sets and observes
	// (TTL/idle/cost-limit/FSx). The on-node daemon enforces it; Telos doesn't babysit.
	Lifecycle compute.Lifecycle

	// DataRef / ResultRef pass state BY REFERENCE (S3/FSx), never inlined.
	DataRef   acs.StateRef
	ResultRef acs.StateRef

	// SpotWebhookURL / Correlation wire the in-window spot-reclamation push
	// (spawn v0.63.0). Correlation is Telos's opaque entity/grant key.
	SpotWebhookURL string
	Correlation    string

	// Provenance threads up to the result's claim — an UNATTESTED compute result
	// fails acceptance, exactly as an unprovenanced literature claim does (§8/M3).
	Provenance []acceptance.Source
}

// WorkResult is the result of a synthesized computation.
type WorkResult struct {
	EntityID string
	// ResultRef is where the output landed (S3/FSx), by reference.
	ResultRef acs.StateRef
	// Disposition is WHY the unit ended (idle-stop→surplus, ttl→deadline,
	// cost-limit→exhaustion, spot→fault, complete→done) — the four-exit mapping.
	Disposition compute.Disposition
	// ComputeSeconds is the accrued modeled-cost basis.
	ComputeSeconds int64
	// Record is the acceptance Record (with provenance) the verdict judges.
	Record acceptance.Record
	// FSx, if non-nil, is the SEPARATE perishable-rectangle ledger line for a
	// filesystem this work provisioned — its hourly cost is attributed apart from
	// the instance cost (§8/M6), so a durable filesystem's accrual is never hidden.
	FSx *FSxLine
}
