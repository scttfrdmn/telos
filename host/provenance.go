// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package host

import (
	"github.com/scttfrdmn/agenkit-go/agenkit"
	"github.com/scttfrdmn/telos/acceptance"
)

// Provenance threading (M3, critical path). A producing node attaches an
// acceptance.Record — its claim plus the CITED SOURCES backing it — to its output
// message under recordKey. Records accumulate UP the graph: a reconciliation node
// merges the records of its inputs into the final record the separate-envelope
// acceptance node judges. Without this, every claim is unprovenanced and the
// verdict can never accept (architecture §4 / M2), so §14 cannot reach an
// accepted-contested result. This is the carrier, not the policy.
//
// The Record lives in message Metadata (not Content) so it survives the agenkit
// patterns' message passing and never pollutes the human-facing text. recordsKey
// carries a SLICE of records across a parallel fan-in (the parallel aggregator
// would otherwise drop child metadata — see concatAggregator).
const (
	recordKey  = "telos.record"
	recordsKey = "telos.records"
)

// attachRecord puts a producer's record on a message and returns the message for
// chaining. The record's sources are the provenance the verdict will grade.
func attachRecord(m *agenkit.Message, rec acceptance.Record) *agenkit.Message {
	if m == nil {
		return m
	}
	return m.WithMetadata(recordKey, rec)
}

// readRecord extracts a producer's record from a message, reporting whether one
// was present. A message with no record carries no provenance (an unprovenanced
// claim — which fails acceptance).
func readRecord(m *agenkit.Message) (acceptance.Record, bool) {
	if m == nil || m.Metadata == nil {
		return acceptance.Record{}, false
	}
	rec, ok := m.Metadata[recordKey].(acceptance.Record)
	return rec, ok
}

// mergeRecords assembles several producer records into one — the reconciliation
// node's job. Sources are unioned (so the head sees ALL evidence gathered, both
// directions); SelfContested is set if any input was contested OR the merged
// sources support BOTH directions; Direction is the reconciled direction (see
// reconcileDirection). The result is the record the acceptance node judges.
func mergeRecords(nodeID string, content string, in []acceptance.Record) acceptance.Record {
	merged := acceptance.Record{NodeID: nodeID, Content: content}
	support, dispute := 0, 0
	for _, r := range in {
		merged.Sources = append(merged.Sources, r.Sources...)
		if r.SelfContested {
			merged.SelfContested = true
		}
		if r.Reproduced {
			merged.Reproduced = true
		}
		for _, s := range r.Sources {
			if s.Supports {
				support++
			} else {
				dispute++
			}
		}
	}
	// Evidence assembled on BOTH sides is the structural signature of a genuinely
	// contested record (the head gathered for AND against). This is what makes a
	// "contested" EARNED rather than a hedge: it rests on the assembled record.
	if support > 0 && dispute > 0 {
		merged.SelfContested = true
		merged.Direction = acceptance.DirectionInconclusive
	} else {
		merged.Direction = reconcileDirection(in)
	}
	return merged
}

// reconcileDirection picks the direction supported by the assembled records when
// they do not conflict. Defaults to inconclusive when there is no clear signal.
func reconcileDirection(in []acceptance.Record) acceptance.Direction {
	for _, r := range in {
		if r.Direction == acceptance.DirectionPositive || r.Direction == acceptance.DirectionNegative {
			return r.Direction
		}
	}
	return acceptance.DirectionInconclusive
}
