// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package router selects a concrete model for a capability constraint — the one
// place model NAMES live (capability-as-constraint, architecture §4). Callers
// and the ACS express a Tier and capabilities; the router resolves them to an
// acs.ModelBinding (provider + model). Nothing above the router names a model.
//
// M1 SCOPE: a minimal config-table selector — enough for the gateway to pick a
// backend from a binding. The cascade-under-surplus-pressure behavior and the
// full router table are deferred (M2/M7+); this slice only maps Tier → binding.
package router

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/telos/acs"
)

// Router resolves a capability constraint to a concrete model binding.
type Router interface {
	// Select picks a binding satisfying the constraint within the budget ceiling.
	// In M1 the ceiling is accepted but not yet used to drive a cascade (M2).
	Select(ctx context.Context, c acs.ModelConstraint, ceil acs.Budget) (acs.ModelBinding, error)
}

// Entry maps a tier to a concrete provider+model. The required capabilities, if
// any, are advertised so Select can reject a constraint the entry can't meet.
type Entry struct {
	Tier         acs.Tier
	Provider     string // drives backend choice in the gateway (e.g. "bedrock", "ollama")
	Model        string // the concrete model id — the ONLY place names live
	Capabilities []string
}

// Table is a static, ordered routing table. The first entry whose tier matches
// (and whose capabilities cover the constraint) wins. An empty tier on the
// constraint resolves to the table's cascade floor (first entry).
type Table struct {
	entries []Entry
}

// NewTable builds a router from an ordered entry list (cheapest first by
// convention, so an empty-tier constraint lands on the floor).
func NewTable(entries []Entry) (*Table, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("router: empty table")
	}
	for i, e := range entries {
		if e.Provider == "" || e.Model == "" {
			return nil, fmt.Errorf("router: entry %d missing provider or model", i)
		}
		if !e.Tier.Valid() {
			return nil, fmt.Errorf("router: entry %d invalid tier %q", i, e.Tier)
		}
	}
	return &Table{entries: append([]Entry(nil), entries...)}, nil
}

// Select resolves the constraint to a binding (architecture §5).
func (t *Table) Select(ctx context.Context, c acs.ModelConstraint, ceil acs.Budget) (acs.ModelBinding, error) {
	if err := ctx.Err(); err != nil {
		return acs.ModelBinding{}, err
	}
	want := c.Tier
	for _, e := range t.entries {
		// Empty constraint tier → take the floor (first entry that has caps).
		if want != "" && e.Tier != want {
			continue
		}
		if !covers(e.Capabilities, c.Capabilities) {
			continue
		}
		return acs.ModelBinding{Provider: e.Provider, Model: e.Model}, nil
	}
	return acs.ModelBinding{}, fmt.Errorf("router: no model for tier %q with capabilities %v", c.Tier, c.Capabilities)
}

// covers reports whether have ⊇ need.
func covers(have, need []string) bool {
	if len(need) == 0 {
		return true
	}
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	for _, n := range need {
		if !set[n] {
			return false
		}
	}
	return true
}

var _ Router = (*Table)(nil)
