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
	// HostSequence is the durable acceptance order. Only accepted facts are
	// journalled, so it is always set.
	HostSequence uint64 `json:"host_sequence"`
	Role         string `json:"role"`
	Digest       string `json:"digest"`
	Baseline     bool   `json:"baseline"`
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

// validateConnectivityFact refuses a record whose mirrors disagree with the
// fact they claim to describe.
func validateConnectivityFact(payload any, baseline bool) error {
	value, ok := asConnectivityFact(payload)
	if !ok {
		return ErrPayloadType
	}
	if value.HostSequence == 0 || len(value.Digest) != 64 || len(value.Fact) == 0 {
		return ErrInvalidField
	}
	if value.Role != "authoritative" && value.Role != "corroborating" {
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

// CanonicalConnectivityRecord builds a journal record from an accepted fact.
func CanonicalConnectivityRecord(
	fact connectivity.Fact,
	hostSequence uint64,
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
