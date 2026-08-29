package connectivity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

var (
	ErrInvalidFact      = errors.New("connectivity fact is invalid")
	ErrInvalidPayload   = errors.New("connectivity payload is invalid")
	ErrInvalidEncoding  = errors.New("connectivity fact encoding is invalid")
	ErrNotCanonical     = errors.New("connectivity fact is not canonically encoded")
	ErrPayloadMismatch  = errors.New("connectivity payload does not match its component")
	ErrForbiddenContent = errors.New("connectivity fact carries forbidden content")
)

// identifierRunes is the only alphabet an opaque identifier may use.
//
// It excludes the separators, quoting and whitespace that a path, an argument
// vector or a shell fragment needs, so a source cannot smuggle one through a
// field that is nominally an identifier.
func identifierRunes(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > MaxIdentifierBytes {
		return false
	}
	// A leading dot, or any run of dots, is how a relative path starts.
	if value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	if bytes.Contains([]byte(value), []byte("..")) {
		return false
	}
	return identifierRunes(value)
}

// validEventUUID accepts only a lowercase canonical UUID.
func validEventUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		switch index {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	return true
}

// Validate reports whether the fact is well-formed on its own terms.
//
// It does not decide ownership: that needs the compiled safety envelope and is
// checked separately, so that a structurally invalid fact and a fact from the
// wrong owner are distinguishable failures.
func Validate(fact Fact) error {
	if fact.Schema != FactSchema || fact.Version != FactSchemaVersion {
		return fmt.Errorf("%w: schema", ErrInvalidFact)
	}
	if !validEventUUID(fact.EventID) {
		return fmt.Errorf("%w: event id", ErrInvalidFact)
	}
	if fact.Domain != policy.DomainRoot && fact.Domain != policy.DomainUser {
		return fmt.Errorf("%w: domain", ErrInvalidFact)
	}
	if !fact.Component.Valid() {
		return fmt.Errorf("%w: component", ErrInvalidFact)
	}
	if !validIdentifier(string(fact.SourceID)) {
		return fmt.Errorf("%w: source id", ErrForbiddenContent)
	}
	if !validIdentifier(fact.BootID) {
		return fmt.Errorf("%w: boot id", ErrForbiddenContent)
	}
	if fact.SourceSequence == 0 {
		return fmt.Errorf("%w: source sequence", ErrInvalidFact)
	}
	if fact.ObservedAt.IsZero() || fact.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("%w: observed at", ErrInvalidFact)
	}
	if fact.MonotonicTick <= 0 {
		return fmt.Errorf("%w: monotonic tick", ErrInvalidFact)
	}
	// A deadline at or before the observation is already expired on arrival,
	// which would make the fact unusable the moment it is accepted.
	if fact.FreshnessDeadline <= fact.MonotonicTick {
		return fmt.Errorf("%w: freshness deadline", ErrInvalidFact)
	}
	if !fact.Lifecycle.Valid() {
		return fmt.Errorf("%w: lifecycle", ErrInvalidFact)
	}
	if !fact.Reason.Valid() {
		return fmt.Errorf("%w: reason", ErrInvalidFact)
	}
	component, single := fact.Payload.component()
	if !single {
		return fmt.Errorf("%w: payload cardinality", ErrInvalidPayload)
	}
	if component != fact.Component {
		return ErrPayloadMismatch
	}
	if err := fact.Payload.validate(); err != nil {
		return err
	}
	return nil
}

// Encode returns the canonical encoding of a validated fact.
func Encode(fact Fact) ([]byte, error) {
	if err := Validate(fact); err != nil {
		return nil, err
	}
	canonical, err := policy.MarshalCanonical(fact)
	if err != nil {
		return nil, ErrInvalidEncoding
	}
	if len(canonical) > MaxEncodedFactBytes {
		return nil, fmt.Errorf("%w: encoded size", ErrInvalidEncoding)
	}
	return canonical, nil
}

// Digest returns the canonical SHA-256 of a validated fact.
func Digest(fact Fact) (string, error) {
	canonical, err := Encode(fact)
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(canonical), nil
}

// Decode parses one encoded fact strictly.
//
// Unknown fields, trailing data, oversized input and any encoding that is not
// already canonical are rejected. Rejecting non-canonical input matters for
// persisted facts: two encodings of the same fact would otherwise carry two
// digests, and the digest is the identity the aggregate deduplicates on.
func Decode(encoded []byte) (Fact, error) {
	if len(encoded) == 0 || len(encoded) > MaxEncodedFactBytes {
		return Fact{}, fmt.Errorf("%w: size", ErrInvalidEncoding)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var fact Fact
	if err := decoder.Decode(&fact); err != nil {
		return Fact{}, fmt.Errorf("%w: %v", ErrInvalidEncoding, err)
	}
	if decoder.More() {
		return Fact{}, fmt.Errorf("%w: trailing data", ErrInvalidEncoding)
	}
	if err := Validate(fact); err != nil {
		return Fact{}, err
	}
	canonical, err := policy.MarshalCanonical(fact)
	if err != nil {
		return Fact{}, ErrInvalidEncoding
	}
	if !bytes.Equal(canonical, encoded) {
		return Fact{}, ErrNotCanonical
	}
	return fact, nil
}
