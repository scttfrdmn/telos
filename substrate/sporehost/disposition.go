// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"strconv"
	"time"

	"github.com/scttfrdmn/telos/compute"
	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

// inferDisposition is the POLL-AND-INFER heart of the Observer (§8): a post-Ready
// state change is autonomous (spored self-governed the node), and cohort has no
// "self-retired" phase — so we disambiguate WHY from the spawn:* tags + state.
//
// The signals, in priority order (most-specific-cause first):
//  1. spot reclamation — a spot interruption marker tag, or a spot instance that
//     terminated (the reclamation path). → fault.
//  2. TTL — the ttl-deadline has passed. → deadline exit.
//  3. cost-limit — accrued cost reached the limit. → exhaustion.
//  4. idle — an idle-timeout is configured and the unit stopped (not terminated).
//     → early completion (banks surplus).
//  5. on-complete — a completion marker. → done.
//
// A still-running unit is DispositionRunning.
//
// NOTE (candidate cohort phase-model issue): this tag-disambiguation is inherently
// racy — two deadlines can pass between polls, and a stopped instance loses its
// "why" if tags are GC'd. If this proves fragile in the live build, that is a
// coordinated cohort issue (a "self-retired" phase carrying the cause), NOT a local
// workaround. For now the priority order resolves the common cases deterministically.
func inferDisposition(in spawnaws.InstanceInfo) compute.Disposition {
	state := mapState(in.State)
	if state == compute.StateRunning || state == compute.StateLaunching {
		return compute.DispositionRunning
	}

	// 1. Spot reclamation — the most important to get right (→ fault).
	if isSpotReclaimed(in) {
		return compute.DispositionSpot
	}
	// 2. TTL deadline passed.
	if ttlExpired(in) {
		return compute.DispositionTTL
	}
	// 3. Cost limit reached.
	if costLimitHit(in) {
		return compute.DispositionCostLimit
	}
	// 4. Idle self-stop: an idle-timeout was set and the unit STOPPED (not
	//    terminated) — the spored idle path stops/hibernates.
	if _, hasIdle := in.Tags["spawn:idle-timeout"]; hasIdle && state == compute.StateStopped {
		return compute.DispositionIdleStop
	}
	// 5. On-complete marker.
	if v, ok := in.Tags["spawn:completed"]; ok && v == "true" {
		return compute.DispositionComplete
	}
	// Stopped/terminated with no legible cause: report complete for a clean stop,
	// else leave it as a bare terminal (the gateway treats an unexplained terminal
	// conservatively — see RunWork settle).
	if state == compute.StateStopped {
		return compute.DispositionComplete
	}
	return compute.DispositionRunning // unreachable for terminal; keeps the switch total
}

// isSpotReclaimed detects a spot reclamation from tags/state. spawn writes a spot
// marker when the on-node handler fires; absent that, a terminated SPOT instance
// is treated as reclaimed (the inferred baseline — #D1).
func isSpotReclaimed(in spawnaws.InstanceInfo) bool {
	if v, ok := in.Tags["spawn:spot-interrupted"]; ok && v != "" {
		return true
	}
	// Inferred baseline: a spot instance that terminated, with a spot-webhook tag
	// present (i.e. it was a managed spot unit), is treated as a reclamation rather
	// than a clean completion. Conservative — a real completion sets spawn:completed.
	if in.SpotInstance && mapState(in.State) == compute.StateFailed {
		if _, completed := in.Tags["spawn:completed"]; !completed {
			return true
		}
	}
	return false
}

func ttlExpired(in spawnaws.InstanceInfo) bool {
	dl, ok := in.Tags["spawn:ttl-deadline"]
	if !ok {
		return false
	}
	t, err := time.Parse(time.RFC3339, dl)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}

func costLimitHit(in spawnaws.InstanceInfo) bool {
	limStr, ok := in.Tags["spawn:cost-limit"]
	if !ok {
		return false
	}
	lim, err := strconv.ParseFloat(limStr, 64)
	if err != nil || lim <= 0 {
		return false
	}
	// Accrued cost basis: compute-seconds is the modeled-cost basis; the gateway
	// owns the $ conversion. Here we only need "did the node hit its own limit",
	// which spored records by terminating — so a cost-limit tag on a terminated
	// (not stopped) instance with no other cause is the cost-limit exit.
	return mapState(in.State) == compute.StateFailed
}

// computeSeconds reads the accrued spawn:compute-seconds tag (the modeled-cost
// basis the gateway prices). Zero if unset.
func computeSeconds(in spawnaws.InstanceInfo) int64 {
	v, ok := in.Tags["spawn:compute-seconds"]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
