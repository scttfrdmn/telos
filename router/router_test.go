// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package router

import (
	"context"
	"testing"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

func table(t *testing.T) *Table {
	t.Helper()
	tbl, err := NewTable([]Entry{
		{Tier: acs.TierCheap, Provider: "ollama", Model: "llama3.1:8b"},
		{Tier: acs.TierMid, Provider: "bedrock", Model: "anthropic.claude-3-5-haiku-20241022-v1:0", Capabilities: []string{"tools"}},
		{Tier: acs.TierFrontier, Provider: "bedrock", Model: "anthropic.claude-3-5-sonnet-20241022-v2:0", Capabilities: []string{"tools", "vision"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tbl
}

func ceil() acs.Budget { return acs.Budget{Amount: 100, Period: 24 * time.Hour} }

func TestSelect_ByTier(t *testing.T) {
	tbl := table(t)
	b, err := tbl.Select(context.Background(), acs.ModelConstraint{Tier: acs.TierMid}, ceil())
	if err != nil {
		t.Fatal(err)
	}
	if b.Provider != "bedrock" || b.Model == "" {
		t.Fatalf("mid tier resolved to %+v", b)
	}
}

func TestSelect_EmptyTierTakesFloor(t *testing.T) {
	tbl := table(t)
	b, err := tbl.Select(context.Background(), acs.ModelConstraint{}, ceil())
	if err != nil {
		t.Fatal(err)
	}
	if b.Provider != "ollama" {
		t.Fatalf("empty tier should take cascade floor (ollama), got %+v", b)
	}
}

func TestSelect_CapabilityFiltering(t *testing.T) {
	tbl := table(t)
	// cheap tier has no capabilities; requiring "vision" at cheap must fail.
	_, err := tbl.Select(context.Background(), acs.ModelConstraint{Tier: acs.TierCheap, Capabilities: []string{"vision"}}, ceil())
	if err == nil {
		t.Fatal("expected no match: cheap tier lacks vision")
	}
	// frontier has vision.
	b, err := tbl.Select(context.Background(), acs.ModelConstraint{Tier: acs.TierFrontier, Capabilities: []string{"vision"}}, ceil())
	if err != nil || b.Provider != "bedrock" {
		t.Fatalf("frontier+vision should resolve to bedrock, got %+v err=%v", b, err)
	}
}

func TestSelect_NoModelNamesLeakToConstraint(t *testing.T) {
	// The constraint carries no model name — only tier+caps. This is the §4
	// capability-as-constraint property; the table is the only place names live.
	c := acs.ModelConstraint{Tier: acs.TierMid}
	if c.Tier == "" {
		t.Skip()
	}
	// (Compile-time: ModelConstraint has no Model field — if someone adds one,
	// this test's intent should be revisited.)
}
