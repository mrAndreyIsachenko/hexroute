package reconciler

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestCanonicalEncodeDecodeRoundTripAndHash(t *testing.T) {
	record, err := NewActionRecord(testProvenance(RecordAcknowledgement), AcknowledgementRecord{
		RequestID: testRequestID, Class: AckAccepted, Reason: ReasonNoAction, RetryClass: RetryNone, NoAction: true,
	})
	if err != nil {
		t.Fatalf("NewActionRecord() error = %v", err)
	}
	encoded, digest, err := EncodeActionRecord(record)
	if err != nil {
		t.Fatalf("EncodeActionRecord() error = %v", err)
	}
	if digest == "" || digest != record.RecordSHA256 {
		t.Fatalf("digest = %q record = %q", digest, record.RecordSHA256)
	}
	decoded, err := DecodeActionRecord(encoded, RecordAcknowledgement)
	if err != nil {
		t.Fatalf("DecodeActionRecord() error = %v", err)
	}
	if decoded.RecordSHA256 != record.RecordSHA256 {
		t.Fatalf("decoded digest = %q, want %q", decoded.RecordSHA256, record.RecordSHA256)
	}
	if _, ok := decoded.Payload.(AcknowledgementRecord); !ok {
		t.Fatalf("decoded payload type = %T", decoded.Payload)
	}
}

func TestDecodeRejectsUnknownTrailingNonCanonicalAndPayloadSubstitution(t *testing.T) {
	record, err := NewActionRecord(testProvenance(RecordReadiness), ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	})
	if err != nil {
		t.Fatalf("NewActionRecord() error = %v", err)
	}
	encoded, _, err := EncodeActionRecord(record)
	if err != nil {
		t.Fatalf("EncodeActionRecord() error = %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	wire["unknown"] = true
	unknown, _ := json.Marshal(wire)
	if _, err := DecodeActionRecord(unknown, RecordReadiness); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("unknown field error = %v", err)
	}
	if _, err := DecodeActionRecord(append(encoded, '\n'), RecordReadiness); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("trailing data/noncanonical error = %v", err)
	}
	pretty := bytes.ReplaceAll(encoded, []byte{','}, []byte{',', '\n'})
	if _, err := DecodeActionRecord(pretty, RecordReadiness); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("pretty noncanonical error = %v", err)
	}
	if _, err := DecodeActionRecord(encoded, RecordOutcome); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("payload substitution error = %v", err)
	}
}

func TestDecodeRejectsPayloadUnknownFieldsAndDigestTamper(t *testing.T) {
	record, err := NewActionRecord(testProvenance(RecordReadiness), ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	})
	if err != nil {
		t.Fatalf("NewActionRecord() error = %v", err)
	}
	encoded, _, err := EncodeActionRecord(record)
	if err != nil {
		t.Fatalf("EncodeActionRecord() error = %v", err)
	}
	mutated := strings.Replace(string(encoded), `"target":"synthetic.target"`, `"target":"synthetic.target","credential_reference":"HEXROUTE_CANARY_TOTP_SEED"`, 1)
	canonicalMutated, canonicalErr := policy.Canonicalize([]byte(mutated))
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if _, err := DecodeActionRecord(canonicalMutated, RecordReadiness); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("payload unknown field error = %v", err)
	}
	tampered := strings.Replace(string(encoded), record.RecordSHA256[:16], strings.Repeat("0", 16), 1)
	canonicalTampered, canonicalErr := policy.Canonicalize([]byte(tampered))
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if _, err := DecodeActionRecord(canonicalTampered, RecordReadiness); !errors.Is(err, ErrMalformedActionRecord) {
		t.Fatalf("digest tamper error = %v", err)
	}
}
