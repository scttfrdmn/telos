// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package sporehost is the spore.host compute substrate: the concrete
// compute.Launcher (the core's §8 seam) backed by cohort + spawn's in-process Go
// libraries. It GENERALIZES spawn/pkg/mpicohort — the existing, production cohort
// provider over spawn/pkg/aws — to the 1-cohort (one-shot job) and collective
// (sweep) cases. Not MCP, not the CLI, not nf-spawn (mpicohort proves Go-in-process).
//
// This package lives in a SEPARATE Go module (substrate/sporehost/go.mod) so the
// AWS-SDK + spore.host dependency tree never enters the core telos module graph.
// The core references only telos/compute interfaces; this package implements them.
package sporehost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/scttfrdmn/telos/compute"
	"github.com/spore-host/cohort"
	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

// LaunchAPI is the spawn surface this substrate needs — identical in shape to
// spawn/pkg/mpicohort.LaunchAPI, re-declared here so the substrate can be driven
// by a FAKE in tests (no AWS, no creds, deterministic) and by the real
// spawn/pkg/aws.Client in a creds-gated live run. The real *aws.Client satisfies it.
type LaunchAPI interface {
	Launch(ctx context.Context, cfg spawnaws.LaunchConfig) (*spawnaws.LaunchResult, error)
	Terminate(ctx context.Context, region, instanceID string) error
	StopInstance(ctx context.Context, region, instanceID string, hibernate bool) error
	StartInstance(ctx context.Context, region, instanceID string) error
	ListInstances(ctx context.Context, region, stateFilter string) ([]spawnaws.InstanceInfo, error)
}

// telosTag marks instances launched by this Telos substrate, so Reconcile can
// list "ours" and match against the ledger to find orphans.
const telosTag = "telos:entity"

// Launcher is the compute.Launcher implementation over spawn. It is the
// generalization of mpicohort's Actuator+Observer+Classifier behind the core seam.
type Launcher struct {
	client LaunchAPI
	region string
	// baseConfig carries account-wide launch defaults (AMI, IAM profile, SG, key).
	baseConfig spawnaws.LaunchConfig
}

// New builds a Launcher over a LaunchAPI (real *spawnaws.Client or a fake).
func New(client LaunchAPI, region string, base spawnaws.LaunchConfig) *Launcher {
	return &Launcher{client: client, region: region, baseConfig: base}
}

// Launch maps a compute.Spec → spawn LaunchConfig and launches one unit. Idempotent
// by IdempotencyToken (RunInstances ClientToken) — a relaunch after a crash returns
// the existing instance, the orphan guard. (Generalizes mpicohort.Actuator.Launch.)
func (l *Launcher) Launch(ctx context.Context, spec compute.Spec) (compute.Observation, error) {
	cfg := l.baseConfig
	cfg.Region = l.region
	cfg.Name = spec.EntityID
	cfg.ClientToken = spec.IdempotencyToken // deterministic — safe to re-issue
	cfg.InstanceType = instanceTypeFor(spec.Rung)
	cfg.Spot = spec.Rung.Spot
	// Self-destruct contract (Telos sets policy, spored enforces — §8).
	cfg.TTL = durStr(spec.Lifecycle.TTL)
	cfg.IdleTimeout = durStr(spec.Lifecycle.IdleTimeout)
	cfg.CostLimit = spec.Lifecycle.CostLimit
	// Perishable-rectangle storage.
	if spec.Lifecycle.FSx.Mode != "" {
		cfg.FSxLustreCreate = true
		cfg.FSxLifecycle = spec.Lifecycle.FSx.Mode
		cfg.FSxTTL = durStr(spec.Lifecycle.FSx.TTL)
	}
	// In-window spot push (spawn v0.63.0): Telos's own opaque correlation key.
	cfg.SpotInterruptionWebhookURL = spec.SpotWebhookURL
	cfg.WebhookCorrelation = spec.Correlation
	// Tag so Reconcile can find our instances.
	if cfg.Tags == nil {
		cfg.Tags = map[string]string{}
	}
	cfg.Tags[telosTag] = spec.EntityID

	res, err := l.client.Launch(ctx, cfg)
	if err != nil {
		return compute.Observation{}, err // classified by faultClass on the way up
	}
	return compute.Observation{
		EntityID:    spec.EntityID,
		State:       mapState(res.State),
		Disposition: compute.DispositionRunning,
		ProviderID:  res.InstanceID,
	}, nil
}

// Observe polls ListInstances and maps each unit's state + disposition. Poll-and-
// infer: a post-Ready state change is disambiguated against spawn:* tags into a
// Disposition (idle-stop / TTL / cost-limit / spot). (Generalizes mpicohort.Observer.)
func (l *Launcher) Observe(ctx context.Context, ids []string) ([]compute.Observation, error) {
	insts, err := l.client.ListInstances(ctx, l.region, "")
	if err != nil {
		return nil, err
	}
	byName := make(map[string]spawnaws.InstanceInfo, len(insts))
	for _, in := range insts {
		byName[in.Name] = in
	}
	out := make([]compute.Observation, 0, len(ids))
	for _, id := range ids {
		in, ok := byName[id]
		if !ok {
			// Lag, not absence (cohort's rule; the idempotency token is ground truth).
			out = append(out, compute.Observation{EntityID: id, State: compute.StateUnknown})
			continue
		}
		out = append(out, observationOf(id, in))
	}
	return out, nil
}

// Terminate destroys a unit by entity id. Idempotent.
func (l *Launcher) Terminate(ctx context.Context, id string) error {
	pid, err := l.providerID(ctx, id)
	if err != nil {
		return nil // already gone
	}
	return l.client.Terminate(ctx, l.region, pid)
}

// Reconcile lists Telos-tagged instances and returns those NOT in `known` — the
// ORPHANS a crash left billing with no ledger record. The caller terminates or
// adopts them. (This is the M6 trap guard; pairs with the idempotency token.)
func (l *Launcher) Reconcile(ctx context.Context, known []string) ([]compute.Observation, error) {
	insts, err := l.client.ListInstances(ctx, l.region, "running")
	if err != nil {
		return nil, err
	}
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}
	var orphans []compute.Observation
	for _, in := range insts {
		entity, tagged := in.Tags[telosTag]
		if !tagged {
			continue // not ours
		}
		if !knownSet[entity] {
			orphans = append(orphans, observationOf(entity, in))
		}
	}
	return orphans, nil
}

func (l *Launcher) providerID(ctx context.Context, id string) (string, error) {
	insts, err := l.client.ListInstances(ctx, l.region, "")
	if err != nil {
		return "", err
	}
	for _, in := range insts {
		if in.Name == id {
			return in.InstanceID, nil
		}
	}
	return "", fmt.Errorf("sporehost: no instance named %q", id)
}

var _ compute.Launcher = (*Launcher)(nil)

// faultClass maps a spawn launch error to a cohort fault class — the Classifier
// half (generalizes mpicohort.Classifier), exposed for the gateway's settle path.
func FaultClass(err error) cohort.FaultClass {
	if err == nil {
		return cohort.FaultRetryableConsistency
	}
	var le *spawnaws.LaunchError
	code := ""
	if errors.As(err, &le) {
		code = le.Code
	}
	switch code {
	case "InsufficientInstanceCapacity", "InsufficientHostCapacity",
		"MaxSpotInstanceCountExceeded", "SpotMaxPriceTooLow":
		return cohort.FaultCapacityExhausted
	case "RequestLimitExceeded", "Throttling":
		return cohort.FaultThrottle
	default:
		return cohort.FaultTerminal
	}
}

// observationOf builds a core Observation from a spawn InstanceInfo, inferring the
// disposition from tags (poll-and-infer; §8).
func observationOf(id string, in spawnaws.InstanceInfo) compute.Observation {
	return compute.Observation{
		EntityID:       id,
		State:          mapState(in.State),
		Disposition:    inferDisposition(in),
		ProviderID:     in.InstanceID,
		ComputeSeconds: computeSeconds(in),
		Reason:         in.State,
	}
}

func mapState(s string) compute.State {
	switch s {
	case "pending":
		return compute.StateLaunching
	case "running":
		return compute.StateRunning
	case "stopping", "stopped":
		return compute.StateStopped
	case "shutting-down", "terminated":
		return compute.StateFailed
	default:
		return compute.StateUnknown
	}
}

func durStr(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func instanceTypeFor(r compute.Rung) string {
	// Minimal mapping; a real frontier search (truffle) refines this. Class → family.
	switch {
	case r.GPU:
		return "g5.xlarge"
	case r.Class == "highmem":
		return "r7g.xlarge"
	default:
		return "c7g.xlarge"
	}
}
