// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"context"

	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

// The creds-gated LIVE path uses the real spawn client behind the same LaunchAPI
// seam the fake tests drive — so live and offline exercise identical substrate
// logic. This compile-time assertion proves *spawnaws.Client satisfies LaunchAPI;
// actually launching real EC2 is a gated operation (real spend), not run in CI.
var _ LaunchAPI = (*spawnaws.Client)(nil)

// NewLive builds a Launcher over a real spawn aws.Client for a creds-gated live
// run. The caller supplies an authenticated *spawnaws.Client (AWS creds in the
// environment). Offline/tests use New() with a fake LaunchAPI instead.
func NewLive(ctx context.Context, client *spawnaws.Client, region string, base spawnaws.LaunchConfig) *Launcher {
	return New(client, region, base)
}
