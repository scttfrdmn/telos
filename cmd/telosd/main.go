// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Command telosd is the Telos host: a generic agenkit-go runtime that answers
// the AgentCore contract (GET /ping, POST /invocations on 0.0.0.0:8080) and
// instantiates an agent graph from a seed ACS (bootstrap.acs).
//
// M0: local-first. Deployment to AgentCore is a separate, gated step.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/host"
)

func main() {
	addr := flag.String("addr", envOr("TELOS_ADDR", "0.0.0.0:8080"), "listen address")
	seedPath := flag.String("seed", envOr("TELOS_SEED", ""), "path to a seed ACS (defaults to the embedded bootstrap.acs)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	seed, err := loadSeed(*seedPath)
	if err != nil {
		log.Error("load seed", "err", err)
		os.Exit(1)
	}
	log.Info("seed loaded", "hash", seed.Hash, "root", seed.RootID, "nodes", len(seed.Nodes))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Assemble the gateway/router/governor deps from the environment. With no
	// AWS creds and no local model server configured, NewDeps falls back to an
	// offline echo backend so the host runs end to end (synthesized cost).
	deps, err := host.NewDeps(ctx, depsConfigFromEnv(seed), log)
	if err != nil {
		log.Error("assemble deps", "err", err)
		os.Exit(1)
	}

	srv, err := host.NewServer(seed, deps, log)
	if err != nil {
		log.Error("new server", "err", err)
		os.Exit(1)
	}

	if err := srv.ListenAndServe(ctx, *addr); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

// loadSeed loads the seed from a path, or the embedded bootstrap.acs if no path
// is given.
func loadSeed(path string) (*acs.Spec, error) {
	if path != "" {
		return acs.LoadFile(path)
	}
	return acs.Load(embeddedBootstrap)
}

// depsConfigFromEnv builds the gateway backend config from the environment. The
// run envelope is taken from the seed's grant. Backends are opt-in:
//
//	TELOS_BEDROCK_MODEL  (+ optional AWS_REGION)  → wire Bedrock
//	TELOS_OLLAMA_MODEL   (+ optional OLLAMA_HOST) → wire local Ollama
//
// With neither set, NewDeps falls back to the offline echo backend.
func depsConfigFromEnv(seed *acs.Spec) host.DepsConfig {
	cfg := host.DepsConfig{Envelope: seed.Budget}
	if m := os.Getenv("TELOS_BEDROCK_MODEL"); m != "" {
		cfg.Bedrock = &host.BedrockConfig{ModelID: m, Region: os.Getenv("AWS_REGION")}
	}
	if m := os.Getenv("TELOS_OLLAMA_MODEL"); m != "" {
		cfg.Ollama = &host.OllamaConfig{Model: m, BaseURL: os.Getenv("OLLAMA_HOST")}
	}
	return cfg
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
