// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acceptance

import (
	"context"
	"fmt"

	"github.com/scttfrdmn/agenkit-go/agenkit"
)

// summaryJudge renders SUMMARY-JUDGMENT verdicts (M2): it grades direction-neutral
// facts about a record — does it carry provenance, do independent sources concur,
// does it reproduce, is it genuinely contested — against a standard of proof. No
// advocates, tiers, or bonds (that is §12 "direction", deferred).
//
// It is disinterested by construction: Render takes only the Record and the
// standard. It has no access to the producer's grant or budget (invariant 10,
// runtime envelope separation), so it cannot be swayed by how much the producer
// has riding on the answer.
type summaryJudge struct {
	name string
}

// NewSummaryJudge constructs a live acceptance node that renders labeled verdicts.
// Like NewInertNode it is the ONLY constructor for this judge — the host routes
// KindAcceptance nodes here, never through a producer builder (invariant 10).
func NewSummaryJudge(name string) agenkit.Agent {
	if name == "" {
		name = "acceptance"
	}
	return &summaryJudge{name: name}
}

// requiredIndependentSources is the concordance bar per standard: how many
// independent, supporting sources a record needs to be accepted as concordant.
// (Provisional thresholds; the policy is summary-judgment-level, not the §12
// court.) Oracle additionally requires Reproduced.
func requiredIndependentSources(standard StandardOfProof) int {
	switch standard {
	case StandardOfProof("scoping"):
		return 0 // scoping bar: provenance presence is enough
	case StandardOfProof("plausible"):
		return 1
	case StandardOfProof("concordant"):
		return 2
	case StandardOfProof("oracle"):
		return 2 // plus Reproduced (checked separately)
	default:
		return 2 // default to the concordant bar
	}
}

// Render renders a verdict for a record at a standard of proof (architecture §5).
// The decision is direction-NEUTRAL: it counts support for whatever direction the
// record states, never preferring positive over negative.
func (j *summaryJudge) Render(ctx context.Context, record Record, standard StandardOfProof) (Verdict, error) {
	if err := ctx.Err(); err != nil {
		return Verdict{}, err
	}

	// Rule 1 — unprovenanced claims fail acceptance (architecture §4). A claim
	// with no sources cannot be graded on direction-neutral verifiable facts.
	if len(record.Sources) == 0 {
		return Verdict{
			Accepted: false,
			Basis:    NotAdjudicated,
			Note:     "no provenance: an unprovenanced claim fails acceptance",
		}, nil
	}

	// Tally support vs dispute among (independent) sources. Direction-neutral:
	// "support" means supports the record's OWN stated direction, whichever it is.
	var indepSupport, dispute int
	for _, s := range record.Sources {
		switch {
		case s.Supports && s.Independent:
			indepSupport++
		case !s.Supports:
			dispute++
		}
	}

	// Rule 2 — genuine contestation is a first-class ACCEPTED outcome (it earned
	// a verdict of due process). A record the producer flagged contested, or one
	// with real support on both sides, is accepted as Contested.
	if record.SelfContested || (dispute > 0 && indepSupport > 0) {
		return Verdict{
			Accepted: true,
			Basis:    Contested,
			Note:     fmt.Sprintf("contested record accepted: %d independent-support vs %d disputing sources", indepSupport, dispute),
		}, nil
	}

	need := requiredIndependentSources(standard)

	// Rule 3 — oracle standard: requires reproduction AND the concordance bar.
	if standard == StandardOfProof("oracle") {
		if record.Reproduced && indepSupport >= need {
			return Verdict{Accepted: true, Basis: OracleVerified,
				Note: fmt.Sprintf("reproduced under test; %d independent supporting sources", indepSupport)}, nil
		}
		return Verdict{Accepted: false, Basis: NotAdjudicated,
			Note: fmt.Sprintf("oracle standard not met: reproduced=%v, independent support=%d (need %d + reproduction)", record.Reproduced, indepSupport, need)}, nil
	}

	// Rule 4 — concordance: enough independent supporting sources for the standard.
	if indepSupport >= need {
		basis := ConcordantUnderTest
		// A reproduced computation that also concurs is oracle-grade even below
		// the oracle standard — the stronger basis is honest to report.
		if record.Reproduced {
			basis = OracleVerified
		}
		return Verdict{Accepted: true, Basis: basis,
			Note: fmt.Sprintf("%d independent supporting sources meet the %q bar (need %d)", indepSupport, standard, need)}, nil
	}

	// Otherwise: provenance exists but does not clear the bar — not accepted.
	return Verdict{Accepted: false, Basis: NotAdjudicated,
		Note: fmt.Sprintf("insufficient concordance for %q: %d independent supporting sources (need %d)", standard, indepSupport, need)}, nil
}

// Process lets the judge sit in an agent graph. It renders a verdict over the
// record carried in the inbound message metadata (key "telos.record") if present,
// else over the message content as a bare, unprovenanced claim (which fails).
// The verdict and its labeled basis are surfaced in the outbound metadata.
func (j *summaryJudge) Process(ctx context.Context, message *agenkit.Message) (*agenkit.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rec := recordFromMessage(message)
	standard := standardFromMessage(message)

	v, err := j.Render(ctx, rec, standard)
	if err != nil {
		return nil, err
	}
	out := agenkit.NewMessage("agent", fmt.Sprintf("[acceptance: %s — %s] %s", verdictWord(v), v.Basis, v.Note))
	out.WithMetadata("telos.kind", "acceptance")
	out.WithMetadata("telos.accepted", v.Accepted)
	out.WithMetadata("telos.basis", string(v.Basis))
	out.WithMetadata("telos.note", v.Note)
	return out, nil
}

func verdictWord(v Verdict) string {
	if v.Accepted {
		return "accepted"
	}
	return "not accepted"
}

func (j *summaryJudge) Name() string { return j.name }
func (j *summaryJudge) Capabilities() []string {
	return []string{"acceptance", "verdict", "summary-judgment"}
}
func (j *summaryJudge) Introspect() *agenkit.IntrospectionResult {
	return agenkit.DefaultIntrospectionResult(j)
}

// recordFromMessage extracts a Record from a message's metadata if a producer
// attached one (key "telos.record"); otherwise it treats the message content as
// a bare, unprovenanced claim (no sources → fails acceptance). This keeps the
// judge's Process usable in a plain agent graph without coupling to producers.
func recordFromMessage(m *agenkit.Message) Record {
	if m == nil {
		return Record{}
	}
	if r, ok := m.Metadata["telos.record"].(Record); ok {
		return r
	}
	return Record{Content: m.ContentString()}
}

// standardFromMessage reads the standard of proof a producer/host attached (key
// "telos.standard"); defaults to concordant when absent.
func standardFromMessage(m *agenkit.Message) StandardOfProof {
	if m != nil {
		if s, ok := m.Metadata["telos.standard"].(string); ok && s != "" {
			return StandardOfProof(s)
		}
	}
	return StandardOfProof("concordant")
}
