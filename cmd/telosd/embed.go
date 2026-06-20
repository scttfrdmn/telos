// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package main

import _ "embed"

// embeddedBootstrap is the seed ACS compiled into the binary so the host is
// self-contained (it can answer the contract with no external files — important
// in the container). The canonical source is bootstrap.acs at the repo root,
// authored by cmd/genbootstrap; `make seed` copies it here. Keep them in sync —
// a TestEmbeddedSeedMatchesRoot guard checks this.
//
//go:embed bootstrap.acs
var embeddedBootstrap []byte
