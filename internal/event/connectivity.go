package event

import (
	"bytes"
	"encoding/json"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// MaxConnectivityPayloadBytes bounds a journalled connectivity record. It is
// larger than the ordinary payload bound because the record carries the whole
// canonical fact, which is what makes the journal replayable on its own.
const MaxConnectivityPayloadBytes = 6 * 1024

// ConnectivityFact is one accepted connectivity fact as it is journalled.
//
// The canonical fact travels whole so replay never has to reconstruct it, and
// the identity fields are mirrored beside it so a record can be selected,
// ordered and retained without decoding the fact first. The mirrors are
// checked against the fact, so they cannot drift from it.
type ConnectivityFact struct {
	Domain         policy.Domain          `json:"domain"`
	Component      connectivity.Component `json:"component"`
	SourceID       connectivity.SourceID  `json:"source_id"`
	BootID         string                 `json:"boot_id"`
	SourceSequence uint64                 `json:"source_sequence"`
	// HostSequence is the durable acceptance order. It is set only on a fact
	// that entered that order; an event the acceptor refused a place in it
	// carries zero.
	HostSequence uint64 `json:"host_sequence"`
	// FoldPosition orders every event a reduction was given, whether or not
	// it was accepted.
	//
	// The two numbers do different jobs and neither can do the other's. The
	// host sequence is the order of accepted facts, and a duplicate, a
	// conflict or a late arrival has no place in it. But the reduction reads
	// those events — a conflict is recorded in the aggregate state, a
	// restatement is owed after one — so a journal that held only the
	// accepted facts could not reproduce what the reduction concluded, and
	// the lineage said so: replaying a cycle that folded one produced a
	// different snapshot and reported it as the conclusion contradicting its
	// own evidence.
	FoldPosition uint64 `json:"fold_position"`
	// Outcome is what the acceptor decided about this event.
	Outcome  string `json:"outcome"`
	Role     string `json:"role"`
	Digest   string `json:"digest"`
	Baseline bool   `json:"baseline"`
	// Fact is the canonical encoding the digest addresses.
	Fact json.RawMessage `json:"fact"`
}

func asConnectivityFact(payload any) (ConnectivityFact, bool) {
	switch value := payload.(type) {
	case ConnectivityFact:
		return value, true
	case *ConnectivityFact:
		if value == nil {
			return ConnectivityFact{}, false
		}
		return *value, true
	default:
		return ConnectivityFact{}, false
	}
}

// OutcomeAccepted is the acceptance outcome that takes a place in the host
// order. It is spelled here because this package may not depend on the
// acceptor, and a test holds the two spellings together.
const OutcomeAccepted = "accepted"

// validateConnectivityFact refuses a record whose mirrors disagree with the
// fact they claim to describe.
func validateConnectivityFact(payload any, baseline bool) error {
	value, ok := asConnectivityFact(payload)
	if !ok {
		return ErrPayloadType
	}
	// A host sequence of zero is how an event says it never entered the
	// accepted order — a duplicate, a conflict, a late arrival. The fold
	// position is the one every event has, because every event was folded.
	if value.FoldPosition == 0 || value.Outcome == "" ||
		len(value.Digest) != 64 || len(value.Fact) == 0 {
		return ErrInvalidField
	}
	if value.Role != "authoritative" && value.Role != "corroborating" {
		return ErrInvalidField
	}
	// The two orders have to agree about what this event was. A record that
	// entered the accepted order and names no place in it, or one that never
	// entered it and claims a place anyway, is a record whose own fields
	// disagree — and the accepted order is what a replay counts on.
	if (value.Outcome == OutcomeAccepted) != (value.HostSequence != 0) {
		return ErrInvalidField
	}
	fact, err := connectivity.Decode([]byte(value.Fact))
	if err != nil {
		return ErrInvalidField
	}
	digest, err := connectivity.Digest(fact)
	if err != nil || digest != value.Digest {
		return ErrInvalidField
	}
	if fact.Domain != value.Domain || fact.Component != value.Component ||
		fact.SourceID != value.SourceID || fact.BootID != value.BootID ||
		fact.SourceSequence != value.SourceSequence || fact.Baseline != value.Baseline {
		return ErrInvalidField
	}
	// The schema carries the retention priority, so a record filed under the
	// wrong one would outlive or predecease what it describes.
	if fact.Baseline != baseline {
		return ErrInvalidField
	}
	return nil
}

// CanonicalConnectivityRecord builds a journal record from one folded event.
func CanonicalConnectivityRecord(
	fact connectivity.Fact,
	hostSequence uint64,
	foldPosition uint64,
	outcome string,
	role string,
) (Schema, ConnectivityFact, error) {
	encoded, err := connectivity.Encode(fact)
	if err != nil {
		return "", ConnectivityFact{}, err
	}
	schema := SchemaConnectivityObservation
	if fact.Baseline {
		schema = SchemaConnectivityBaseline
	}
	record := ConnectivityFact{
		Domain:         fact.Domain,
		Component:      fact.Component,
		SourceID:       fact.SourceID,
		BootID:         fact.BootID,
		SourceSequence: fact.SourceSequence,
		HostSequence:   hostSequence,
		FoldPosition:   foldPosition,
		Outcome:        outcome,
		Role:           role,
		Digest:         policy.SHA256Hex(encoded),
		Baseline:       fact.Baseline,
		Fact:           json.RawMessage(append([]byte(nil), encoded...)),
	}
	return schema, record, nil
}

// DecodeConnectivityFact returns the fact a journalled record carries.
func DecodeConnectivityFact(record ConnectivityFact) (connectivity.Fact, error) {
	return connectivity.Decode([]byte(record.Fact))
}

// sameCanonicalFact reports whether two records carry byte-identical facts.
func sameCanonicalFact(left, right ConnectivityFact) bool {
	return bytes.Equal([]byte(left.Fact), []byte(right.Fact))
}
