// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

// Package acceptance is the disinterested verdict, rendered in a SEPARATE trust
// and budget envelope from production (invariant 10 / §12 — the keystone seam).
//
// "Get it wrong and no later policy recovers it." This package therefore holds
// ONLY the verdict contract and acceptance-node construction. It contains NO
// result-producing code: nothing here reasons about, retrieves, or synthesizes a
// claim. A producer never settles its own acceptance, and — enforced by keeping
// acceptance construction in this package and forbidding the host's producer
// builders from touching acceptance nodes — production code cannot reach in and
// self-render a verdict.
//
// M0 builds the SEAM, not the policy. The acceptance node here is INERT: it
// renders no verdict (NewInertNode). The courtroom (advocates, tiers, bonds —
// §12 direction) is deferred; the separation is pinned now because it is the part
// that is unrecoverable later.
package acceptance

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
)

// Basis is the labeled grounding of a verdict — HOW acceptance was established,
// never a bare "true" (invariant 10 / §12 committed seam). It is orthogonal to
// which way the result went.
type Basis string

const (
	// OracleVerified: checked against an oracle that exists (e.g. a computation
	// reproduces, a fact has a ground truth).
	OracleVerified Basis = "oracle-verified"
	// ConcordantUnderTest: independent methods/sources concur under test, where
	// no single oracle exists.
	ConcordantUnderTest Basis = "concordant-under-test"
	// Contested: an earned verdict of due process — the record is genuinely
	// contested, not a failure to decide. A first-class result.
	Contested Basis = "contested"
	// NotAdjudicated: no verdict was rendered (the M0 inert state).
	NotAdjudicated Basis = "not-adjudicated"
)

// Verdict is what an acceptance node renders. Accepted is never self-asserted by
// a producer; it comes only from here (architecture §5).
type Verdict struct {
	Accepted bool   `json:"accepted"`
	Basis    Basis  `json:"basis"`
	Note     string `json:"note,omitempty"`
}

// Record is the production output an acceptance node judges. It is opaque to this
// package by design — acceptance grades direction-neutral facts about a record,
// it does not re-produce or re-reason the content.
type Record struct {
	// NodeID is the producing node whose record this is.
	NodeID string
	// Content is the rendered result text (by value here in M0; by reference in
	// later milestones).
	Content string
}

// StandardOfProof is the bar a record must clear. It is a string alias rather
// than an import of acs to keep this package free of any production dependency
// (the acs package models production graphs); acs.StandardOfProof values are
// passed through as plain strings.
type StandardOfProof string

// Acceptance renders a disinterested verdict on a record at a given standard of
// proof (architecture §5). Implementations must have no stake in which way the
// result goes.
type Acceptance interface {
	Render(ctx context.Context, record Record, standard StandardOfProof) (Verdict, error)
}

// inertNode is the M0 acceptance node: a disinterested party that renders NO
// verdict yet. It implements both Acceptance and agenkit.Agent so the host can
// instantiate it as a graph node in its own envelope, while the verdict logic
// (M2) is still absent. Crucially it produces nothing about the claim — its
// Process echoes back a "not adjudicated" marker, never an evaluation.
type inertNode struct {
	name string
}

// NewInertNode constructs the M0 acceptance node. This is the ONLY constructor
// the host uses for a KindAcceptance node; the host's producer/stub builders
// must never be used for acceptance (asserted by a guard test). That routing is
// the package-level half of the invariant-10 seam.
func NewInertNode(name string) agenkit.Agent {
	if name == "" {
		name = "acceptance"
	}
	return &inertNode{name: name}
}

// Render is the Acceptance contract. In M0 it always returns NotAdjudicated: the
// seam exists, the policy does not. It renders a verdict, never a result.
func (n *inertNode) Render(ctx context.Context, record Record, standard StandardOfProof) (Verdict, error) {
	return Verdict{
		Accepted: false,
		Basis:    NotAdjudicated,
		Note:     fmt.Sprintf("acceptance is inert in M0; record from %q held to standard %q was not adjudicated", record.NodeID, standard),
	}, nil
}

// Process lets the inert node sit in an agent graph. It returns a marker message
// and renders no judgement on the content — acceptance does not produce.
func (n *inertNode) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := agenkit.NewMessage("agent", "[acceptance: not adjudicated in M0 — separate-envelope seam present, verdict policy deferred to M2]")
	out.WithMetadata("telos.kind", "acceptance")
	out.WithMetadata("telos.basis", string(NotAdjudicated))
	return out, nil
}

// Name implements agenkit.Agent.
func (n *inertNode) Name() string { return n.name }

// Capabilities implements agenkit.Agent.
func (n *inertNode) Capabilities() []string { return []string{"acceptance", "verdict"} }

// Introspect implements agenkit.Agent.
func (n *inertNode) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(n)
}
