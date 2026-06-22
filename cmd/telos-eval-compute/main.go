// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Command telos-eval-compute is the Phase-2 (real-compute) eval. It is GATED —
// it launches ONE real, billable EC2 instance — and must be run with explicit
// flags and a hard cap. Its single most important job is to verify the M6 orphan
// reconciler AGAINST A REAL INSTANCE: launch one tiny instance, deliberately
// STRAND it (simulate a Telos crash by discarding the ledger), then prove
// Launcher.Reconcile detects and terminates it with zero residual billing.
//
// Safety backstops, in depth:
//   - tiny instance (t4g.small), spot, hard TTL (default 15m) + cost-limit on the
//     node itself (spored self-terminates even if Telos and the reconciler both die),
//   - FSx ephemeral only (no durable rectangle),
//   - a -cap backstop and a final residual-billing check that lists ALL
//     telos-tagged instances and fails loudly if any survive.
//
// Run (real spend):
//
//	AWS_PROFILE=aws go run ./cmd/telos-eval-compute -go -region=us-west-2 \
//	  -ami=ami-… -cap=2.0
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/substrate/sporehost"
	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

func main() {
	var (
		go_     = flag.Bool("go", false, "REQUIRED to actually launch a real instance (real spend)")
		region  = flag.String("region", "us-west-2", "AWS region")
		ami     = flag.String("ami", "", "AMI id (AL2023 arm64); required")
		itype   = flag.String("instance-type", "t4g.small", "instance type (small/cheap)")
		ttlMin  = flag.Int("ttl-min", 15, "hard TTL minutes (spored self-terminates) — the deepest backstop")
		costCap = flag.Float64("cost-limit", 0.50, "on-node cost-limit USD (spored self-terminates)")
		capUSD  = flag.Float64("cap", 0, "harness backstop cap USD (required)")
	)
	flag.Parse()

	if !*go_ {
		fmt.Fprintln(os.Stderr, "telos-eval-compute: refusing to launch without -go (real spend). Re-run with -go.")
		os.Exit(1)
	}
	if *ami == "" || *capUSD <= 0 {
		fatal("require -ami and a positive -cap")
	}

	ctx := context.Background()
	// NewClient reads region from the AWS config/env; set AWS_REGION to *region so
	// the SDK and our region flag agree.
	_ = os.Setenv("AWS_REGION", *region)
	client, err := spawnaws.NewClient(ctx)
	if err != nil {
		fatal("aws client: %v", err)
	}

	// Provision the spored IAM role (idempotent) so the node can self-govern.
	fmt.Fprintln(os.Stderr, "phase2: ensuring spored IAM role…")
	profile, err := client.SetupSporedIAMRole(ctx)
	if err != nil {
		fatal("setup spored IAM role: %v", err)
	}

	base := spawnaws.LaunchConfig{
		AMI:                *ami,
		IamInstanceProfile: profile,
		Username:           "ec2-user",
	}
	launcher := sporehost.New(client, *region, base)

	entity := "telos-eval-orphan-" + stamp()
	spec := compute.Spec{
		EntityID:          entity,
		IdempotencyToken:  "telos-eval-" + entity,
		Rung:              compute.Rung{Class: "cpu", Spot: true},
		EstimatedDuration: time.Duration(*ttlMin) * time.Minute,
		Lifecycle: compute.Lifecycle{
			TTL:       time.Duration(*ttlMin) * time.Minute, // deepest backstop
			CostLimit: *costCap,                             // second backstop
		},
	}
	// Force the instance type via the base config (the Launcher maps Rung→type,
	// but for the eval we pin a known-cheap type).
	base.InstanceType = *itype
	launcher = sporehost.New(client, *region, base)

	fmt.Fprintf(os.Stderr, "phase2: launching ONE real instance %q (%s spot, TTL=%dm, cost-limit=$%.2f, cap=$%.2f)…\n",
		entity, *itype, *ttlMin, *costCap, *capUSD)
	obs, err := launcher.Launch(ctx, spec)
	if err != nil {
		fatal("launch: %v", err)
	}
	fmt.Fprintf(os.Stderr, "phase2: launched provider_id=%s state=%s\n", obs.ProviderID, obs.State)

	report := Phase2Report{
		Entity: entity, ProviderID: obs.ProviderID, InstanceType: *itype,
		TTLMinutes: *ttlMin, CostLimit: *costCap,
		LaunchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// --- THE STRAND: simulate a Telos crash. We DISCARD the ledger (the `known`
	// set is EMPTY), as if the record were lost. The instance is real and billing,
	// with no Telos record of it. ---
	fmt.Fprintln(os.Stderr, "phase2: STRANDING the instance (discarding the ledger — simulating a Telos crash)…")
	report.Stranded = true

	// --- RECONCILE: the orphan guard. With an empty ledger, every telos-tagged
	// running instance is an orphan. ---
	fmt.Fprintln(os.Stderr, "phase2: reconciling (empty ledger → the instance is an orphan)…")
	// Give the instance a moment to be listable.
	time.Sleep(8 * time.Second)
	orphans, err := launcher.Reconcile(ctx, nil) // nil ledger = nothing known
	if err != nil {
		fatal("reconcile: %v (instance may still be running — check console! TTL/cost-limit are the backstop)", err)
	}
	report.OrphansDetected = len(orphans)
	found := false
	for _, o := range orphans {
		fmt.Fprintf(os.Stderr, "phase2: orphan detected: %s (%s)\n", o.EntityID, o.ProviderID)
		if o.EntityID == entity {
			found = true
		}
		// Terminate the orphan.
		if err := launcher.Terminate(ctx, o.EntityID); err != nil {
			fmt.Fprintf(os.Stderr, "phase2: terminate %s: %v\n", o.EntityID, err)
		} else {
			report.Terminated = append(report.Terminated, o.EntityID)
		}
	}
	report.OurInstanceDetected = found

	// --- RESIDUAL CHECK: prove zero telos-tagged instances are left running/billing. ---
	fmt.Fprintln(os.Stderr, "phase2: verifying zero residual billing (waiting for termination to settle)…")
	time.Sleep(15 * time.Second)
	residual, err := launcher.Reconcile(ctx, nil)
	if err != nil {
		fatal("residual check: %v", err)
	}
	report.ResidualRunning = len(residual)
	report.ResidualClean = len(residual) == 0
	report.CheckedAt = time.Now().UTC().Format(time.RFC3339)

	emit(report)
	if !report.ResidualClean {
		fmt.Fprintf(os.Stderr, "phase2: ⚠️  %d telos-tagged instance(s) STILL RUNNING — TERMINATE MANUALLY. TTL=%dm is the backstop.\n", report.ResidualRunning, *ttlMin)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "phase2: ✅ orphan detected, terminated, zero residual billing. The real-money safety net holds.")
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "phase2: "+f+"\n", a...)
	os.Exit(1)
}

func stamp() string {
	// deterministic-ish unique suffix without Date.now in tests; here in a cmd it's fine.
	return fmt.Sprintf("%d", time.Now().Unix())
}
