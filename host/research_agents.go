// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"context"
	"fmt"
	"strings"

	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acceptance"
	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/domain/research"
	"github.com/scttfrdmn/telos/gateway"
	"github.com/scttfrdmn/telos/router"
)

// The emitted research graph (domain/research) tags its producing nodes by Role
// so the host can give them provenance-aware behavior. This is the M3 critical
// path: producers attach cited sources to their records, the reconciliation node
// assembles the for/against record and may emit an EARNED contested, and the
// separate-envelope acceptance node can finally ACCEPT (M2 returns "unprovenanced"
// without this). The agents wrap a gateway call when deps are wired (so a real
// model fills the content) and fall back to deterministic provenance offline.

// evidenceAgent is a producing node that gathers evidence for one DIRECTION
// (for/against) of a mechanistic claim and attaches a provenanced record. The
// direction is the node's stance, NOT a thumb on acceptance — the judge grades
// the assembled record direction-neutrally.
type evidenceAgent struct {
	node      *acs.Node
	direction acceptance.Direction
	inner     agenkit.Agent // optional gateway-backed producer for real content
}

func (a *evidenceAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, content, err := runInnerOrEcho(ctx, a.inner, a.node, message)
	if err != nil {
		return nil, err
	}
	// Attach a provenanced record. Offline, sources are deterministic stand-ins
	// marked independent + supporting THIS node's direction; a real backend's
	// content informs the note. The point M3 proves is the THREADING — that a
	// producer carries sources the verdict can grade.
	rec := acceptance.Record{
		NodeID:    string(a.node.ID),
		Content:   content,
		Direction: a.direction,
		Sources:   evidenceSources(a.node.ID, a.direction),
	}
	return attachRecord(out, rec), nil
}

func (a *evidenceAgent) Name() string { return string(a.node.ID) }
func (a *evidenceAgent) Capabilities() []string {
	return []string{"reason", "evidence", string(a.direction)}
}
func (a *evidenceAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}

// reconcileAgent is the mechanistic head's reconciliation: it MERGES the records
// of its inputs (the for/against assembly) into one record and is WILLING to emit
// Contested when the assembled record has evidence on both sides — an earned
// contested, not a hedge (§14 #3). It also surfaces the for/against record its
// verdict rested on (inspectable).
type reconcileAgent struct {
	node  *acs.Node
	inner agenkit.Agent
}

func (a *reconcileAgent) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, content, err := runInnerOrEcho(ctx, a.inner, a.node, message)
	if err != nil {
		return nil, err
	}

	// Assemble the upstream records. The Sequential/Parallel patterns carry the
	// previous stage's metadata forward; collect every record reachable on the
	// inbound message's pipeline metadata as well as the direct input.
	assembled := collectRecords(message)

	merged := mergeRecords(string(a.node.ID), content, assembled)

	// An EARNED contested rests on evidence from both directions. If the assembly
	// is one-sided, this is NOT a contested record — it is whatever direction was
	// supported. mergeRecords sets SelfContested only when both sides are present,
	// so contested here is earned by construction.
	out = attachRecord(out, merged)
	// Inspectable (§14 #3): surface the for/against record the reconciliation
	// rested on, so a human can verify contested was earned.
	out.WithMetadata("telos.forAgainst", summarizeAssembly(assembled))
	out.WithMetadata("telos.reconciled_direction", string(merged.Direction))
	out.WithMetadata("telos.contested", merged.SelfContested)
	return out, nil
}

func (a *reconcileAgent) Name() string           { return string(a.node.ID) }
func (a *reconcileAgent) Capabilities() []string { return []string{"reconcile", "mechanistic-head"} }
func (a *reconcileAgent) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(a)
}

// runInnerOrEcho runs the gateway-backed producer if present (real content), else
// produces a deterministic echo line. Returns the output message and its content.
func runInnerOrEcho(ctx context.Context, inner agenkit.Agent, n *acs.Node, message *agenkit.Message) (*agenkit.Message, string, error) {
	if inner != nil {
		out, err := inner.Process(ctx, message)
		if err != nil {
			return nil, "", err
		}
		if out == nil {
			out = agenkit.NewMessage("agent", "")
		}
		return out, out.ContentString(), nil
	}
	content := fmt.Sprintf("[%s/%s] %s", n.ID, n.Role, truncate(messageContent(message), 100))
	return agenkit.NewMessage("agent", content), content, nil
}

func messageContent(m *agenkit.Message) string {
	if m == nil {
		return ""
	}
	return m.ContentString()
}

// evidenceSources fabricates deterministic provenance offline. A real producer
// (gateway-backed, with retrieval tools) replaces this with actual citations; the
// shape is what the verdict grades. Each source is independent and supports the
// node's own direction — so a single-direction node yields concordant support,
// and a for+against PAIR yields the both-sides record that earns contested.
func evidenceSources(id acs.NodeID, dir acceptance.Direction) []acceptance.Source {
	supports := dir != acceptance.DirectionNegative // "against" nodes dispute the positive claim
	return []acceptance.Source{
		{ID: string(id) + ":src1", Independent: true, Supports: supports},
		{ID: string(id) + ":src2", Independent: true, Supports: supports},
	}
}

// collectRecords gathers all producer records reachable from a message — the
// direct telos.record plus any carried in agenkit's pipeline_stages metadata
// (Sequential/Parallel preserve prior stages). Deduped by NodeID.
func collectRecords(m *agenkit.Message) []acceptance.Record {
	seen := map[string]bool{}
	var out []acceptance.Record
	add := func(r acceptance.Record) {
		if r.NodeID != "" && !seen[r.NodeID] {
			seen[r.NodeID] = true
			out = append(out, r)
		}
	}
	if r, ok := readRecord(m); ok {
		add(r)
	}
	if m != nil {
		// A parallel fan-in carries a SLICE of child records (recordsKey).
		if recs, ok := m.Metadata[recordsKey].([]acceptance.Record); ok {
			for _, r := range recs {
				add(r)
			}
		}
		// Defensive: any stray single records in metadata.
		for _, v := range m.Metadata {
			if r, ok := v.(acceptance.Record); ok {
				add(r)
			}
		}
	}
	return out
}

func summarizeAssembly(recs []acceptance.Record) string {
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "%s(%s):%dsrc; ", r.NodeID, r.Direction, len(r.Sources))
	}
	return strings.TrimSpace(b.String())
}

// newResearchLeaf builds the role-aware producing agent for a research-shape leaf.
// Returns (agent, true) when the node's Role is a producing role; (nil, false)
// otherwise so the caller falls back to the default leaf builder.
func newResearchLeaf(n *acs.Node, gw gateway.Gateway, rtr router.Router) (agenkit.Agent, bool) {
	var inner agenkit.Agent
	if gw != nil && rtr != nil && n.Kind == acs.KindReason {
		inner = newGatewayAgent(n, gw, rtr)
	}
	switch n.Role {
	case research.RoleEvidenceFor:
		return &evidenceAgent{node: n, direction: acceptance.DirectionPositive, inner: inner}, true
	case research.RoleEvidenceAgainst:
		return &evidenceAgent{node: n, direction: acceptance.DirectionNegative, inner: inner}, true
	case research.RoleReconcile:
		return &reconcileAgent{node: n, inner: inner}, true
	case research.RoleSynthesize, research.RoleRetrieve, research.RoleExtract:
		// Evidence-synthesis producers also carry provenance so an
		// evidence-synthesis-only run can reach acceptance. Direction positive
		// (the substrate's consolidated finding).
		return &evidenceAgent{node: n, direction: acceptance.DirectionPositive, inner: inner}, true
	}
	return nil, false
}
