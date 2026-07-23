package telemetry

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

const (
	testBatchID = metadata.UUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	testNodeID  = metadata.UUID("11111111-1111-4111-8111-111111111111")
)

func TestBatchEncodingIsCanonicalAndRoundTrips(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	first, err := EncodeBatch(testBatchID, entries)
	if err != nil {
		t.Fatalf("EncodeBatch(first) error = %v", err)
	}
	second, err := EncodeBatch(testBatchID, entries)
	if err != nil {
		t.Fatalf("EncodeBatch(second) error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical gzip encoding changed for identical input")
	}

	decoded, err := DecodeBatch(first)
	if err != nil {
		t.Fatalf("DecodeBatch() error = %v", err)
	}
	if decoded.BatchID != testBatchID || decoded.NodeID != testNodeID ||
		decoded.FirstSequence != 1 || decoded.LastSequence != 2 ||
		len(decoded.Events) != 2 {
		t.Fatalf("DecodeBatch() = %+v", decoded)
	}
}

func TestAcknowledgementDeletesOnlyExplicitEventIDs(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}

	acknowledgement := Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          testBatchID,
		NodeID:           testNodeID,
		AcceptedEventIDs: []metadata.UUID{entries[0].Metadata.EventID},
	}
	encoded, err := EncodeAcknowledgement(acknowledgement)
	if err != nil {
		t.Fatalf("EncodeAcknowledgement() error = %v", err)
	}
	decoded, err := DecodeAcknowledgement(encoded)
	if err != nil {
		t.Fatalf("DecodeAcknowledgement() error = %v", err)
	}
	removed, err := ApplyAcknowledgement(journal, testBatchID, testNodeID, decoded)
	if err != nil || removed != 1 {
		t.Fatalf("ApplyAcknowledgement() = %d, %v; want 1, nil", removed, err)
	}

	remaining, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].Metadata.EventID != entries[1].Metadata.EventID {
		t.Fatalf("remaining entries = %+v", remaining)
	}

	removed, err = ApplyAcknowledgement(journal, testBatchID, testNodeID, decoded)
	if err != nil || removed != 0 {
		t.Fatalf("duplicate acknowledgement = %d, %v; want 0, nil", removed, err)
	}
}

func TestMismatchedAcknowledgementCannotDeleteSpoolRecords(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	acknowledgement := Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          metadata.UUID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		NodeID:           testNodeID,
		AcceptedEventIDs: []metadata.UUID{entries[0].Metadata.EventID},
	}
	if _, err := ApplyAcknowledgement(
		journal,
		testBatchID,
		testNodeID,
		acknowledgement,
	); !errors.Is(err, ErrAcknowledgementMismatch) {
		t.Fatalf("ApplyAcknowledgement() error = %v, want %v", err, ErrAcknowledgementMismatch)
	}
	remaining, err := journal.Entries()
	if err != nil || len(remaining) != 1 {
		t.Fatalf("remaining entries = %d, %v; want 1, nil", len(remaining), err)
	}
}

func TestBatchRejectsInvalidOrderingAndOversizedCompressedInput(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	entries[0], entries[1] = entries[1], entries[0]
	if _, err := EncodeBatch(testBatchID, entries); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("EncodeBatch(out of order) error = %v, want %v", err, ErrInvalidBatch)
	}
	if _, err := DecodeBatch(make([]byte, MaxBatchCompressedBytes+1)); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("DecodeBatch(oversized) error = %v, want %v", err, ErrBatchTooLarge)
	}
}

func TestSequenceGapsRemainVisible(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	encoded, err := EncodeBatch(testBatchID, entries)
	if err != nil {
		t.Fatalf("EncodeBatch() error = %v", err)
	}
	batch, err := DecodeBatch(encoded)
	if err != nil {
		t.Fatalf("DecodeBatch() error = %v", err)
	}
	batch.Events[1].Metadata.Sequence = 4
	batch.LastSequence = 4

	gaps, err := SequenceGaps(batch, 1)
	if err != nil {
		t.Fatalf("SequenceGaps() error = %v", err)
	}
	if len(gaps) != 1 || gaps[0] != (SequenceRange{First: 2, Last: 3}) {
		t.Fatalf("SequenceGaps() = %+v, want [{2 3}]", gaps)
	}
}

func newJournal(t *testing.T) *spool.Spool {
	t.Helper()
	journal, err := spool.Open(
		filepath.Join(t.TempDir(), "spool"),
		spool.OwnerRoot,
		spool.Options{NodeID: testNodeID},
	)
	if err != nil {
		t.Fatalf("spool.Open() error = %v", err)
	}
	return journal
}

func appendObservation(t *testing.T, journal *spool.Spool) {
	t.Helper()
	encoded, err := event.Encode(event.SchemaObservation, event.Observation{
		Component: control.ComponentNetwork,
		Health:    control.HealthReady,
		Reason:    control.ReasonProbeSucceeded,
	})
	if err != nil {
		t.Fatalf("encode observation: %v", err)
	}
	if _, err := journal.Append(encoded); err != nil {
		t.Fatalf("append observation: %v", err)
	}
}

func appendTransition(t *testing.T, journal *spool.Spool) {
	t.Helper()
	encoded, err := event.Encode(event.SchemaTransition, event.Transition{
		Component:  control.ComponentTunnel,
		From:       control.StateRecovering,
		To:         control.StateHealthy,
		Reason:     control.ReasonVerificationPassed,
		Generation: 2,
	})
	if err != nil {
		t.Fatalf("encode transition: %v", err)
	}
	if _, err := journal.Append(encoded); err != nil {
		t.Fatalf("append transition: %v", err)
	}
}
