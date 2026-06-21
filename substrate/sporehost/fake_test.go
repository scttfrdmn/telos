// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package sporehost

import (
	"context"
	"sync"

	spawnaws "github.com/spore-host/spawn/pkg/aws"
)

// fakeLaunch is an in-mem LaunchAPI for offline tests — no AWS, no creds,
// deterministic. It is the fake-seam mpicohort's LaunchAPI interface enables, so
// the substrate's logic (mapping, disposition inference, orphan detection) is
// fully testable without spending money. The real *spawnaws.Client satisfies the
// same interface for the creds-gated live path.
type fakeLaunch struct {
	mu        sync.Mutex
	instances map[string]*spawnaws.InstanceInfo // by instanceID
	byToken   map[string]string                 // ClientToken → instanceID (idempotency)
	nextID    int
	// launchErr, if set, is returned from Launch (to exercise the Classifier).
	launchErr error
	launches  int // count of ACTUAL RunInstances-equivalent calls (idempotency check)
}

func newFakeLaunch() *fakeLaunch {
	return &fakeLaunch{instances: map[string]*spawnaws.InstanceInfo{}, byToken: map[string]string{}}
}

func (f *fakeLaunch) Launch(ctx context.Context, cfg spawnaws.LaunchConfig) (*spawnaws.LaunchResult, error) {
	if f.launchErr != nil {
		return nil, f.launchErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// IDEMPOTENCY (the orphan guard): a relaunch with the same ClientToken returns
	// the existing instance, never a duplicate — exactly RunInstances' behavior.
	if cfg.ClientToken != "" {
		if id, ok := f.byToken[cfg.ClientToken]; ok {
			in := f.instances[id]
			return &spawnaws.LaunchResult{InstanceID: id, Name: in.Name, State: in.State}, nil
		}
	}
	f.nextID++
	f.launches++
	id := "i-" + itoa(f.nextID)
	in := &spawnaws.InstanceInfo{
		InstanceID: id, Name: cfg.Name, InstanceType: cfg.InstanceType,
		State: "running", Region: cfg.Region, SpotInstance: cfg.Spot,
		Tags: map[string]string{},
	}
	for k, v := range cfg.Tags {
		in.Tags[k] = v
	}
	if cfg.TTL != "" {
		in.Tags["spawn:idle-timeout"] = cfg.IdleTimeout
	}
	f.instances[id] = in
	if cfg.ClientToken != "" {
		f.byToken[cfg.ClientToken] = id
	}
	return &spawnaws.LaunchResult{InstanceID: id, Name: cfg.Name, State: "running"}, nil
}

func (f *fakeLaunch) Terminate(ctx context.Context, region, instanceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[instanceID]; ok {
		in.State = "terminated"
	}
	return nil
}

func (f *fakeLaunch) StopInstance(ctx context.Context, region, instanceID string, hibernate bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[instanceID]; ok {
		in.State = "stopped"
	}
	return nil
}

func (f *fakeLaunch) StartInstance(ctx context.Context, region, instanceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[instanceID]; ok {
		in.State = "running"
	}
	return nil
}

func (f *fakeLaunch) ListInstances(ctx context.Context, region, stateFilter string) ([]spawnaws.InstanceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []spawnaws.InstanceInfo
	for _, in := range f.instances {
		if stateFilter != "" && in.State != stateFilter {
			continue
		}
		out = append(out, *in)
	}
	return out, nil
}

// setState/setTag let tests drive a unit into a disposition for the Observer.
func (f *fakeLaunch) setState(id, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[id]; ok {
		in.State = state
	}
}

func (f *fakeLaunch) setTag(id, k, v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[id]; ok {
		in.Tags[k] = v
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
