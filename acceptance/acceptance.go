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
// M2 builds SUMMARY JUDGMENT: the node renders real, labeled verdicts (see
// judge.go / NewSummaryJudge). The courtroom (advocates, tiers, bonds — §12
// "direction") is still deferred; M2 only grades direction-neutral facts about a
// record (its provenance exists and is consistent), never a bare "true." The M0
// inert node (NewInertNode) is retained for composition-only / offline paths.
//
// RUNTIME ENVELOPE SEPARATION (invariant 10, extended from M0's package boundary):
// the verdict path takes only a Record (the result to judge) and a standard — it
// has NO handle to the producer's grant, reservoir, or budget context, so it
// cannot be swayed by how much budget the producer has. The producing node never
// settles its own acceptance; the package contains no production code (guarded by
// the import test in seam_test.go).
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
// package by design — acceptance grades direction-neutral FACTS about a record
// (does it carry provenance; do its sources concur; does it reproduce), it does
// not re-produce or re-reason the content. Crucially it carries NO budget/grant
// context: the verdict path cannot see the producer's reservoir (invariant 10).
type Record struct {
	// NodeID is the producing node whose record this is.
	NodeID string

	// Content is the rendered result text (by value here; by reference later).
	Content string

	// Direction is which way the result points — Positive, Negative, or
	// Inconclusive. It is recorded ONLY so the judge can be shown to treat
	// directions neutrally; it must NOT affect whether the record is accepted.
	Direction Direction

	// Sources are the citations/evidence backing the claim. A record with NO
	// sources is unprovenanced and FAILS acceptance (architecture §4).
	Sources []Source

	// Reproduced reports that a computation in the record reproduced under test
	// (an oracle check). When true the verdict can reach OracleVerified.
	Reproduced bool

	// SelfContested marks a record the producer itself flagged as genuinely
	// contested (conflicting evidence). An accepted contested record is a
	// first-class result, not a failure (architecture §10/§14).
	SelfContested bool
}

// Source is one piece of provenance backing a record. Concordance is judged on
// how many INDEPENDENT, on-point sources support the claim.
type Source struct {
	// ID is a citation / dataset / attestation identifier.
	ID string
	// Independent marks a source methodologically independent of the others
	// (different method/lab/model) — concordance among independent sources is
	// worth more than agreement among dependent ones (architecture §12).
	Independent bool
	// Supports reports whether this source supports (true) or disputes (false)
	// the record's direction. Disputing sources push toward Contested.
	Supports bool
}

// Direction is which way a result points. Acceptance is direction-NEUTRAL: a
// well-supported Negative is accepted exactly like a well-supported Positive.
type Direction string

const (
	DirectionPositive     Direction = "positive"
	DirectionNegative     Direction = "negative"
	DirectionInconclusive Direction = "inconclusive"
)

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
