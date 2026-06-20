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

	srv, err := host.NewServer(seed, log)
	if err != nil {
		log.Error("new server", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
