// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package main

import (
	"os"
	"testing"

	"github.com/scttfrdmn/telos/acs"
)

// TestEmbeddedSeedMatchesRoot guards against the embedded copy of bootstrap.acs
// (cmd/telosd/bootstrap.acs) drifting from the canonical root artifact. They are
// the same file by `make seed`; this fails loudly if someone regenerates the
// root without copying.
func TestEmbeddedSeedMatchesRoot(t *testing.T) {
	root, err := os.ReadFile("../../bootstrap.acs")
	if err != nil {
		t.Fatalf("read root seed: %v", err)
	}
	if string(root) != string(embeddedBootstrap) {
		t.Fatal("embedded bootstrap.acs differs from root bootstrap.acs; run `make seed`")
	}
}

// TestEmbeddedSeedLoads confirms the embedded seed is a valid, self-hashing ACS.
func TestEmbeddedSeedLoads(t *testing.T) {
	if _, err := acs.Load(embeddedBootstrap); err != nil {
		t.Fatalf("embedded seed does not load: %v", err)
	}
}
