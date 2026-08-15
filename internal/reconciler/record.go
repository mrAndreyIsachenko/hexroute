package reconciler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type ActionRecord struct {
	Schema       string           `json:"schema"`
	Provenance   ActionProvenance `json:"provenance"`
	Payload      any              `json:"payload"`
	RecordSHA256 string           `json:"record_sha256"`
}

type wireActionRecord struct {
	Schema       string           `json:"schema"`
	Provenance   ActionProvenance `json:"provenance"`
	Payload      json.RawMessage  `json:"payload"`
	RecordSHA256 string           `json:"record_sha256"`
}

type digestActionRecord struct {
	Schema     string           `json:"schema"`
	Provenance ActionProvenance `json:"provenance"`
	Payload    any              `json:"payload"`
}

var (
	ErrMalformedActionRecord = errors.New("malformed reconciliation action record")
	ErrPayloadKindMismatch   = errors.New("action record payload kind mismatch")
)

func NewActionRecord(provenance ActionProvenance, payload any) (ActionRecord, error) {
	if provenance.Validate(provenance.Kind) != nil || validatePayload(provenance.Kind, payload) != nil {
		return ActionRecord{}, ErrInvalidContract
	}
	digest, err := recordDigest(provenance, payload)
	if err != nil {
		return ActionRecord{}, ErrInvalidContract
	}
	return ActionRecord{
		Schema:       ActionRecordSchema,
		Provenance:   provenance,
		Payload:      payload,
		RecordSHA256: digest,
	}, nil
}

func EncodeActionRecord(record ActionRecord) ([]byte, string, error) {
	if record.Schema != ActionRecordSchema ||
		record.Provenance.Validate(record.Provenance.Kind) != nil ||
		validatePayload(record.Provenance.Kind, record.Payload) != nil {
		return nil, "", ErrInvalidContract
	}
	digest, err := recordDigest(record.Provenance, record.Payload)
	if err != nil || digest != record.RecordSHA256 {
		return nil, "", ErrInvalidContract
	}
	encoded, err := policy.MarshalCanonical(record)
	if err != nil || len(encoded) > MaxRecordBytes {
		return nil, "", ErrInvalidContract
	}
	return encoded, digest, nil
}

func DecodeActionRecord(encoded []byte, expected RecordKind) (ActionRecord, error) {
	if !expected.Valid() || len(encoded) == 0 || len(encoded) > MaxRecordBytes {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	canonical, err := policy.Canonicalize(encoded)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	var wire wireActionRecord
	if err := decodeStrict(encoded, &wire); err != nil {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	if wire.Schema != ActionRecordSchema || wire.Provenance.Kind != expected ||
		wire.Provenance.Validate(expected) != nil || !validDigest(wire.RecordSHA256) {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	payload, err := decodePayloadStrict(expected, wire.Payload)
	if err != nil {
		if errors.Is(err, ErrPayloadKindMismatch) {
			return ActionRecord{}, err
		}
		return ActionRecord{}, ErrMalformedActionRecord
	}
	digest, err := recordDigest(wire.Provenance, payload)
	if err != nil || digest != wire.RecordSHA256 {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	record := ActionRecord{
		Schema:       wire.Schema,
		Provenance:   wire.Provenance,
		Payload:      payload,
		RecordSHA256: wire.RecordSHA256,
	}
	rebuilt, _, err := EncodeActionRecord(record)
	if err != nil || !bytes.Equal(rebuilt, encoded) {
		return ActionRecord{}, ErrMalformedActionRecord
	}
	return record, nil
}

func recordDigest(provenance ActionProvenance, payload any) (string, error) {
	if provenance.Validate(provenance.Kind) != nil || validatePayload(provenance.Kind, payload) != nil {
		return "", ErrInvalidContract
	}
	encoded, err := policy.MarshalCanonical(digestActionRecord{
		Schema:     ActionRecordSchema,
		Provenance: provenance,
		Payload:    payload,
	})
	if err != nil || len(encoded) > MaxRecordBytes {
		return "", ErrInvalidContract
	}
	return policy.SHA256Hex(encoded), nil
}

func validatePayload(kind RecordKind, payload any) error {
	switch typed := payload.(type) {
	case ReadinessRecord:
		if kind == RecordReadiness {
			return typed.Validate()
		}
	case AcknowledgementRecord:
		if kind == RecordAcknowledgement {
			return typed.Validate()
		}
	case ActionPlanRecord:
		if kind == RecordActionPlan {
			return typed.Validate()
		}
	case OperationSessionRecord:
		if kind == RecordOperationSession {
			return typed.Validate()
		}
	case CheckpointRecord:
		if kind == RecordCheckpoint {
			return typed.Validate()
		}
	case AttemptRecord:
		if kind == RecordAttempt {
			return typed.Validate()
		}
	case StepRecord:
		if kind == RecordStep {
			return typed.Validate()
		}
	case ResourceRecord:
		if kind == RecordResource {
			return typed.Validate()
		}
	case OutcomeRecord:
		if kind == RecordOutcome {
			return typed.Validate()
		}
	case IncidentRecord:
		if kind == RecordIncident {
			return typed.Validate()
		}
	}
	return ErrPayloadKindMismatch
}

func decodePayloadStrict(kind RecordKind, encoded json.RawMessage) (any, error) {
	switch kind {
	case RecordReadiness:
		var payload ReadinessRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordAcknowledgement:
		var payload AcknowledgementRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordActionPlan:
		var payload ActionPlanRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordOperationSession:
		var payload OperationSessionRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordCheckpoint:
		var payload CheckpointRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordAttempt:
		var payload AttemptRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordStep:
		var payload StepRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordResource:
		var payload ResourceRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordOutcome:
		var payload OutcomeRecord
		return decodeIntoPayload(encoded, &payload)
	case RecordIncident:
		var payload IncidentRecord
		return decodeIntoPayload(encoded, &payload)
	default:
		return nil, ErrPayloadKindMismatch
	}
}

func decodeIntoPayload[T any](encoded json.RawMessage, destination *T) (T, error) {
	var zero T
	if len(encoded) == 0 || len(encoded) > MaxRecordBytes {
		return zero, ErrMalformedActionRecord
	}
	if err := decodeStrict(encoded, destination); err != nil {
		return zero, ErrMalformedActionRecord
	}
	if validatePayloadKind(destination) != nil {
		return zero, ErrMalformedActionRecord
	}
	return *destination, nil
}

func validatePayloadKind(payload any) error {
	switch typed := payload.(type) {
	case *ReadinessRecord:
		return typed.Validate()
	case *AcknowledgementRecord:
		return typed.Validate()
	case *ActionPlanRecord:
		return typed.Validate()
	case *OperationSessionRecord:
		return typed.Validate()
	case *CheckpointRecord:
		return typed.Validate()
	case *AttemptRecord:
		return typed.Validate()
	case *StepRecord:
		return typed.Validate()
	case *ResourceRecord:
		return typed.Validate()
	case *OutcomeRecord:
		return typed.Validate()
	case *IncidentRecord:
		return typed.Validate()
	default:
		return ErrPayloadKindMismatch
	}
}

func decodeStrict(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrMalformedActionRecord
	}
	return nil
}

func canonicalTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return false
	}
	return parsed.UTC().Format(time.RFC3339Nano) == value
}
