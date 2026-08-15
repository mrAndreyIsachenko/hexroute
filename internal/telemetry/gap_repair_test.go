package telemetry

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

func TestPrepareGapReplayBuildsExactRetainedSignedBatch(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	appendObservation(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	key := gapSigningKey(t)
	budget := NewGapReplayBudget(1)
	batchID := metadata.UUID("33333333-3333-4333-8333-333333333333")
	requestID := metadata.UUID("44444444-4444-4444-8444-444444444444")
	request, err := PrepareGapReplay(
		journal,
		key,
		[]SequenceRange{{First: 2, Last: 3}},
		batchID,
		requestID,
		time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
		&budget,
	)
	if err != nil {
		t.Fatalf("PrepareGapReplay() error = %v", err)
	}
	if request.Envelope.Envelope.RequestID != requestID ||
		request.Envelope.Envelope.NodeID != testNodeID ||
		request.FirstSequence != 2 ||
		request.LastSequence != 3 ||
		request.ReplayedEvents != 2 {
		t.Fatalf("replay request = %+v", request)
	}
	batch, err := DecodeBatch(request.Body)
	if err != nil {
		t.Fatalf("DecodeBatch(replay) error = %v", err)
	}
	if batch.BatchID != batchID ||
		batch.Events[0].Metadata.Sequence != 2 ||
		batch.Events[1].Metadata.Sequence != 3 ||
		!bytes.Equal(batch.Events[0].Record, entries[1].Event) ||
		!bytes.Equal(batch.Events[1].Record, entries[2].Event) {
		t.Fatalf("replay batch = %+v", batch)
	}
	if _, err := PrepareGapReplay(
		journal,
		key,
		[]SequenceRange{{First: 1, Last: 1}},
		metadata.UUID("55555555-5555-4555-8555-555555555555"),
		metadata.UUID("66666666-6666-4666-8666-666666666666"),
		time.Date(2026, time.August, 15, 12, 0, 1, 0, time.UTC),
		&budget,
	); !errors.Is(err, ErrGapReplayRateLimited) {
		t.Fatalf("second replay error = %v, want %v", err, ErrGapReplayRateLimited)
	}
}

func TestGapRepairDetectsExpiredEvidenceAndEmitsOneRedactedRecord(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	entries, complete, err := RetainedEntriesForRanges(
		journal,
		[]SequenceRange{{First: 1, Last: 2}},
	)
	if err != nil || complete || len(entries) != 1 {
		t.Fatalf("RetainedEntriesForRanges() = %d complete=%t err=%v", len(entries), complete, err)
	}

	reporter := &GapUnrecoverableReporter{}
	first, err := reporter.EmitOnce(journal, []SequenceRange{{First: 1, Last: 2}})
	if err != nil || !first {
		t.Fatalf("EmitOnce(first) = %t, %v", first, err)
	}
	second, err := reporter.EmitOnce(journal, []SequenceRange{{First: 1, Last: 2}})
	if err != nil || second {
		t.Fatalf("EmitOnce(second) = %t, %v", second, err)
	}
	entries, err = journal.Entries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries after unrecoverable gap = %d, %v", len(entries), err)
	}
	record, err := event.Decode(entries[1].Event)
	if err != nil || record.Schema != event.SchemaDiagnostic {
		t.Fatalf("diagnostic event = %+v, %v", record, err)
	}
	diagnostic := record.Payload.(*event.Diagnostic)
	if diagnostic.Code != event.DiagnosticTelemetryGapUnrecoverable ||
		diagnostic.Count != 1 {
		t.Fatalf("diagnostic payload = %+v", diagnostic)
	}
}

func TestGapRepairRejectsUnboundedOrReorderedRanges(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	tests := [][]SequenceRange{
		{},
		{{First: 0, Last: 1}},
		{{First: 3, Last: 2}},
		{{First: 1, Last: MaxMissingRangeWidth + 1}},
		{{First: 2, Last: 3}, {First: 3, Last: 4}},
	}
	for _, ranges := range tests {
		if _, _, err := RetainedEntriesForRanges(journal, ranges); !errors.Is(err, ErrInvalidAcknowledgement) {
			t.Fatalf("RetainedEntriesForRanges(%+v) error = %v", ranges, err)
		}
	}
}

func gapSigningKey(t *testing.T) signing.Key {
	t.Helper()
	randomBytes := make([]byte, ed25519.SeedSize+16)
	randomBytes[0] = 11
	key, err := signing.GenerateFile(
		filepath.Join(t.TempDir(), "gap-node.json"),
		testNodeID,
		bytes.NewReader(randomBytes),
	)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	return key
}
