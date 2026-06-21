// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/compute"
	"github.com/scttfrdmn/telos/governor"
)

// fakeLauncher is an in-mem compute.Launcher for gateway tests — no AWS. It drives
// a unit to a configured terminal disposition after the first Observe, so the
// metered loop runs end to end offline.
type fakeLauncher struct {
	launched    map[string]bool
	disposition compute.Disposition
	computeSecs int64
	launchErr   error
	launches    int
}

func newFakeLauncher(d compute.Disposition, secs int64) *fakeLauncher {
	return &fakeLauncher{launched: map[string]bool{}, disposition: d, computeSecs: secs}
}

func (f *fakeLauncher) Launch(ctx context.Context, s compute.Spec) (compute.Observation, error) {
	if f.launchErr != nil {
		return compute.Observation{}, f.launchErr
	}
	f.launches++
	f.launched[s.EntityID] = true
	return compute.Observation{EntityID: s.EntityID, State: compute.StateRunning, Disposition: compute.DispositionRunning}, nil
}

func (f *fakeLauncher) Observe(ctx context.Context, ids []string) ([]compute.Observation, error) {
	out := make([]compute.Observation, 0, len(ids))
	for _, id := range ids {
		if !f.launched[id] {
			out = append(out, compute.Observation{EntityID: id, State: compute.StateUnknown})
			continue
		}
		// Terminal on first observe (deterministic).
		out = append(out, compute.Observation{
			EntityID: id, State: compute.StateStopped, Disposition: f.disposition,
			ComputeSeconds: f.computeSecs, Reason: string(f.disposition),
		})
	}
	return out, nil
}

func (f *fakeLauncher) Terminate(ctx context.Context, id string) error {
	delete(f.launched, id)
	return nil
}
func (f *fakeLauncher) Reconcile(ctx context.Context, known []string) ([]compute.Observation, error) {
	return nil, nil
}

// fakePricer returns a fixed hourly rate.
type fakePricer struct{ hourly float64 }

func (p fakePricer) EstimateHourly(ctx context.Context, r compute.Rung) (float64, error) {
	return p.hourly, nil
}

func computeGateway(t *testing.T, env float64, l compute.Launcher, p compute.Pricer) (*Chokepoint, governor.Governor) {
	t.Helper()
	gov := governor.New(acs.Budget{Amount: env, Period: 30 * 24 * time.Hour, Currency: "USD"})
	costs := NewCostModel(CostModelConfig{})
	gw, err := New(Config{
		Backends: map[string]Backend{"echo": NewEchoBackend("echo")},
		Governor: gov, Costs: costs, Launcher: l, Pricer: p, ComputeCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	return gw, gov
}

func workspec(id string, dur time.Duration, prov []acceptance.Source) WorkloadSpec {
	return WorkloadSpec{
		EntityID: id, IdempotencyToken: "tok-" + id,
		Rung: compute.Rung{Class: "cpu"}, EstimatedDuration: dur,
		ResultRef: acs.StateRef("s3://bucket/results/" + id), Provenance: prov,
	}
}

// Compute disabled (no launcher) → ErrComputePathNotImplemented (the M1→M6 boundary).
func TestRunWork_DisabledWithoutLauncher(t *testing.T) {
	gov := governor.New(acs.Budget{Amount: 100, Period: time.Hour, Currency: "USD"})
	gw, _ := New(Config{Backends: map[string]Backend{"echo": NewEchoBackend("echo")}, Governor: gov, Costs: NewCostModel(CostModelConfig{})})
	_, _, err := gw.RunWork(context.Background(), workspec("j", time.Hour, nil))
	if !errors.Is(err, ErrComputePathNotImplemented) {
		t.Fatalf("no launcher should disable compute, got %v", err)
	}
}

// The metered loop: estimate → reserve → launch → meter → settle, grant-rate, with
// SYNTHESIZED cost (modeled compute, not billed).
func TestRunWork_MeteredLoopSynthesizedCost(t *testing.T) {
	l := newFakeLauncher(compute.DispositionComplete, 3600) // 1 compute-hour
	gw, gov := computeGateway(t, 1000, l, fakePricer{hourly: 2.0})

	res, cost, err := gw.RunWork(context.Background(), workspec("job-1", time.Hour, oneSource()))
	if err != nil {
		t.Fatalf("runwork: %v", err)
	}
	// 1 compute-hour × $2/hr = $2, ALL synthesized (modeled, not billed).
	if cost.Amount != 2.0 {
		t.Fatalf("metered compute cost = %v, want 2.0", cost.Amount)
	}
	if !cost.FullySynthesized() {
		t.Fatalf("compute cost must be fully synthesized (modeled), got synthesized=%v of %v", cost.Synthesized, cost.Amount)
	}
	// Settled grant-rate: root reservoir down by ~2.
	if rem := gov.Remaining(governor.RootGrant).Amount; rem != 998 {
		t.Fatalf("root remaining = %v, want 998 (1000-2)", rem)
	}
	if res.Disposition != compute.DispositionComplete {
		t.Fatalf("disposition = %v, want complete", res.Disposition)
	}
}

// Fails closed: an estimate larger than the envelope is refused before launch.
func TestRunWork_FailsClosedBeforeLaunch(t *testing.T) {
	l := newFakeLauncher(compute.DispositionComplete, 3600)
	gw, _ := computeGateway(t, 1.0, l, fakePricer{hourly: 100.0}) // 100h-ish estimate >> $1
	_, _, err := gw.RunWork(context.Background(), workspec("job-1", time.Hour, nil))
	if !errors.Is(err, ErrReservationDenied) {
		t.Fatalf("over-estimate must fail closed, got %v", err)
	}
	if l.launches != 0 {
		t.Fatal("launched despite failed reservation — must fail closed BEFORE launch")
	}
}

// Four-exit mapping: idle-stop → done (banks surplus on acceptance); ttl/cost/spot
// → exhausted/fault. Here we check the disposition→exit projection.
func TestRunWork_FourExitMapping(t *testing.T) {
	cases := []struct {
		d    compute.Disposition
		exit governor.ExitKind
	}{
		{compute.DispositionIdleStop, governor.ExitDone},
		{compute.DispositionComplete, governor.ExitDone},
		{compute.DispositionTTL, governor.ExitExhausted},
		{compute.DispositionCostLimit, governor.ExitExhausted},
		{compute.DispositionSpot, governor.ExitExhausted},
	}
	for _, c := range cases {
		if got := exitForDisposition(c.d); got != c.exit {
			t.Errorf("disposition %v → exit %v, want %v", c.d, got, c.exit)
		}
	}
}

// Attestation: a compute result WITHOUT provenance produces a Record that the
// acceptance judge REJECTS (unattested fails acceptance, §8/M3); WITH provenance
// it can be accepted.
func TestRunWork_UnattestedFailsAcceptance(t *testing.T) {
	judge := acceptance.NewSummaryJudge("judge").(acceptance.Acceptance)

	// Unattested run: no provenance.
	l1 := newFakeLauncher(compute.DispositionComplete, 60)
	gw1, _ := computeGateway(t, 1000, l1, fakePricer{hourly: 1})
	res1, _, _ := gw1.RunWork(context.Background(), workspec("j1", time.Minute, nil))
	v1, _ := judge.Render(context.Background(), res1.Record, "concordant")
	if v1.Accepted {
		t.Fatal("an UNATTESTED compute result must fail acceptance")
	}

	// Attested run: two independent supporting sources.
	l2 := newFakeLauncher(compute.DispositionComplete, 60)
	gw2, _ := computeGateway(t, 1000, l2, fakePricer{hourly: 1})
	res2, _, _ := gw2.RunWork(context.Background(), workspec("j2", time.Minute, twoSources()))
	v2, _ := judge.Render(context.Background(), res2.Record, "concordant")
	if !v2.Accepted {
		t.Fatalf("an ATTESTED compute result should be acceptable, got %+v", v2)
	}
}

func oneSource() []acceptance.Source {
	return []acceptance.Source{{ID: "compute:run-1", Independent: true, Supports: true}}
}
func twoSources() []acceptance.Source {
	return []acceptance.Source{
		{ID: "compute:run-1", Independent: true, Supports: true},
		{ID: "compute:input-sha", Independent: true, Supports: true},
	}
}
