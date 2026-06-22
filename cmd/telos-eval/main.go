// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Command telos-eval is the reusable evaluation harness. It runs a set of research
// questions through the M0–M6 spine and captures, per question, the full run
// signal as JSONL — archetype, composite detection, the scope node's emitted
// entity expansion + bounds, which path ran, exit, acceptance verdict + basis,
// surplus banked + cause, and the cost split (model vs compute, modeled vs
// metered). It is PERMANENT infrastructure: every M7 piece is validated against it.
//
// It is honest by construction — it MEASURES the spine as-is and never tunes the
// system to pass. A question that under-fans or returns a bare verdict is a finding
// recorded faithfully, not a bug to hot-fix.
//
// Backends:
//
//	-backend=echo     (default) offline plumbing check, NO spend, NO creds.
//	-backend=bedrock  real Bedrock model (creds-gated). SPENDS REAL MONEY — gated
//	                  by an explicit -cap (hard dollar grant) per the eval contract.
//
// The grant cap is a HARD ceiling: the governor's run envelope is set from -cap,
// and -cap-per-question bounds each question's grant. The harness refuses to run
// the real backend without an explicit, positive cap.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
	"github.com/scttfrdmn/telos/host"
)

func main() {
	var (
		backend    = flag.String("backend", "echo", "echo | bedrock (bedrock spends real money)")
		region     = flag.String("region", "us-west-2", "AWS region for bedrock")
		model      = flag.String("model", "", "Bedrock model id for the escalation tier (mid/frontier)")
		cheapModel = flag.String("cheap-model", "", "Bedrock model id for the cheap tier (optional; defaults to -model)")
		capUSD     = flag.Float64("cap", 0, "HARD total REAL-SPEND cap in USD across the run (metered Bedrock $); the harness stops when cumulative metered spend reaches it (required for bedrock)")
		envelope   = flag.Float64("envelope", 200, "per-question grant ENVELOPE in USD — the reservation ceiling the governor conserves against. Must exceed the seed-emitted graph budget (the finding: that's ~$100) or every run fails closed. Real spend is METERED separately and bounded by -cap.")
		perQ       = flag.Float64("cap-per-question", 0, "unused legacy; see -envelope and -cap")
		questions  = flag.String("questions", "", "path to the question-set JSON (see questions.example.json)")
		out        = flag.String("out", "", "path to write the JSONL run log (default stdout)")
	)
	flag.Parse()

	if *questions == "" {
		fatal("missing -questions <path>")
	}
	qs, err := loadQuestions(*questions)
	if err != nil {
		fatal("load questions: %v", err)
	}

	// HARD CAP GATE: the real backend never runs without an explicit positive cap.
	if *backend == "bedrock" {
		if *capUSD <= 0 {
			fatal("bedrock requires an explicit positive -cap (hard dollar grant); refusing to spend uncapped")
		}
		if *model == "" {
			fatal("bedrock requires -model")
		}
	}
	_ = *perQ // legacy flag retained for compat; envelope + cap supersede it
	perQuestion := *envelope

	writer := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatal("open out: %v", err)
		}
		defer f.Close()
		writer = f
	}
	enc := json.NewEncoder(writer)

	ctx := context.Background()
	runner, err := newRunner(ctx, *backend, *region, *model, *cheapModel, perQuestion)
	if err != nil {
		fatal("init runner: %v", err)
	}

	fmt.Fprintf(os.Stderr, "telos-eval: backend=%s questions=%d cap=$%.2f per-question=$%.4f\n",
		*backend, len(qs), *capUSD, perQuestion)

	var meteredTotal float64 // cumulative REAL (provider-billed) spend — the cap axis
	for i, q := range qs {
		// HARD CAP on REAL metered spend: stop before exceeding the dollar grant.
		if *backend == "bedrock" && meteredTotal >= *capUSD {
			fmt.Fprintf(os.Stderr, "telos-eval: HARD CAP $%.2f real spend reached after %d questions; stopping (the grant clock hitting the wall)\n", *capUSD, i)
			break
		}
		rec := runner.run(ctx, q)
		rec.Index = i
		meteredTotal += rec.CostMetered
		rec.SpentSoFar = meteredTotal
		if err := enc.Encode(rec); err != nil {
			fatal("encode record: %v", err)
		}
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s → http=%d archetype=%q accepted=%v basis=%s metered=$%.4f modeled=$%.4f (real $%.4f/$%.2f)\n",
			i+1, len(qs), short(q.ID), rec.HTTPStatus, rec.Archetype, rec.Accepted, rec.Basis, rec.CostMetered, rec.CostModeled, meteredTotal, *capUSD)
	}
	fmt.Fprintf(os.Stderr, "telos-eval: done. total REAL metered spend $%.4f of $%.2f cap.\n", meteredTotal, *capUSD)
}

// runner builds a FRESH deps+governor+host per question — each question is its own
// Telos grant (the eval contract), so cost/surplus are measured per-question, not
// accumulated across the set.
type runner struct {
	backend, region, model, cheapModel string
	perQuestionCap                     float64
	seed                               *acs.Spec
}

func newRunner(ctx context.Context, backend, region, model, cheapModel string, perQuestionCap float64) (*runner, error) {
	seed, err := acs.LoadFile("bootstrap.acs")
	if err != nil {
		seed, err = acs.LoadFile("../../bootstrap.acs")
		if err != nil {
			return nil, fmt.Errorf("load seed: %w", err)
		}
	}
	return &runner{backend: backend, region: region, model: model, cheapModel: cheapModel,
		perQuestionCap: perQuestionCap, seed: seed}, nil
}

func (r *runner) run(ctx context.Context, q Question) RunRecord {
	rec := RunRecord{ID: q.ID, Prompt: q.Prompt, Why: q.Why}

	// Each question is its own grant: fresh governor with the per-question cap as
	// the run envelope (a real reservoir+clock the burn-rate thermostat meets).
	env := acs.Budget{Amount: r.perQuestionCap, Period: 24 * time.Hour, Currency: "USD"}
	if r.perQuestionCap <= 0 {
		env.Amount = 1e9 // echo: unmetered, large envelope avoids fail-closed
	}
	cfg := host.DepsConfig{Envelope: env}
	if r.backend == "bedrock" {
		cfg.Bedrock = &host.BedrockConfig{ModelID: r.model, Region: r.region}
		cfg.CheapModel = r.cheapModel
	}
	deps, err := host.NewDeps(ctx, cfg, nil)
	if err != nil {
		rec.Error = "deps: " + err.Error()
		return rec
	}
	srv, err := host.NewServer(r.seed, deps, nil)
	if err != nil {
		rec.Error = "server: " + err.Error()
		return rec
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"prompt":` + jsonStr(q.Prompt) + `}`
	resp, err := http.Post(ts.URL+"/invocations", "application/json", strings.NewReader(body))
	if err != nil {
		rec.Error = err.Error()
		return rec
	}
	defer resp.Body.Close()
	rec.HTTPStatus = resp.StatusCode
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Capture the server error verbatim — a 500 is a measured finding, not a
		// silent zero. (This is how the budget-propagation gap surfaced.)
		rec.Error = "http " + resp.Status + ": " + excerpt(string(raw), 400)
		if gov, ok := deps.Governor.(*governor.Mem); ok {
			rec.fromGovernor(gov) // shows what the run reserved/spent before erroring
		}
		return rec
	}
	var inv invocationResponse
	if err := json.Unmarshal(raw, &inv); err != nil {
		rec.Error = "decode: " + err.Error()
		return rec
	}
	rec.fromInvocation(inv)
	if gov, ok := deps.Governor.(*governor.Mem); ok {
		rec.fromGovernor(gov)
	}
	return rec
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "telos-eval: "+f+"\n", a...)
	os.Exit(1)
}

func short(s string) string {
	if len(s) <= 28 {
		return s
	}
	return s[:28]
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
