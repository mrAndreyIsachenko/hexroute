package telemetry

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

// acceptingTransport takes a batch and acknowledges every event in it.
type acceptingTransport struct {
	batches int
	events  int
}

func (transport *acceptingTransport) Upload(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (Acknowledgement, error) {
	batch, err := DecodeBatch(body)
	if err != nil {
		return Acknowledgement{}, err
	}
	transport.batches++
	transport.events += len(batch.Events)
	accepted := make([]metadata.UUID, 0, len(batch.Events))
	highest := uint64(0)
	for _, item := range batch.Events {
		accepted = append(accepted, item.Metadata.EventID)
		if item.Metadata.Sequence > highest {
			highest = item.Metadata.Sequence
		}
	}
	return Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           envelope.Envelope.NodeID,
		RequestID:        envelope.Envelope.RequestID,
		AcceptedEventIDs: accepted,
		HighWatermark:    highest,
	}, nil
}

// A pass costs the records it sends, not the records the spool holds.
//
// The spool this runtime actually has holds eighty-odd thousand records,
// because nothing has ever drained it, and a pass sends two hundred and
// fifty-six. Reading all of them to choose those would have made draining the
// store cost that scan once per batch — the same work squared — and it is the
// defect this repository has already paid for twice on other paths.
//
// The assertion counts records opened rather than seconds, for the reason the
// eviction cost test gives: a stopwatch measures the machine, and a threshold
// tuned until it passes measures the threshold.
func TestAPassOpensOnlyTheRecordsItSends(t *testing.T) {
	journal := newJournal(t)
	const stored = MaxBatchEvents * 4
	for index := 0; index < stored; index++ {
		appendObservation(t, journal)
	}

	settled := journal.Opens()
	transport := &acceptingTransport{}
	uploader := newTestUploader(t, journal, transport)

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if transport.events != MaxBatchEvents {
		t.Fatalf("the pass sent %d events, want %d",
			transport.events, MaxBatchEvents)
	}
	// One open per record sent. Anything above that is a record read and not
	// handed out, which is the whole subject.
	if opened := journal.Opens() - settled; opened != MaxBatchEvents {
		t.Fatalf("the pass opened %d records to send %d, with %d stored",
			opened, MaxBatchEvents, stored)
	}
}

// And draining the store stays linear in the store rather than quadratic: each
// pass pays for its own batch and for nothing it leaves behind.
func TestDrainingTheStoreCostsTheStoreOnce(t *testing.T) {
	journal := newJournal(t)
	const stored = MaxBatchEvents * 3
	for index := 0; index < stored; index++ {
		appendObservation(t, journal)
	}

	settled := journal.Opens()
	transport := &acceptingTransport{}
	uploader := newTestUploader(t, journal, transport)

	for pass := 0; pass < 4; pass++ {
		if err := uploader.RunOnce(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	if transport.events != stored {
		t.Fatalf("four passes sent %d of %d records", transport.events, stored)
	}
	if sequences, err := journal.Sequences(); err != nil {
		t.Fatalf("sequences: %v", err)
	} else if len(sequences) != 0 {
		t.Fatalf("%d records remain after the store was drained", len(sequences))
	}
	if opened := journal.Opens() - settled; opened != stored {
		t.Fatalf("draining %d records opened %d", stored, opened)
	}
}

// A record that has gone since the listing is an ordinary answer. The listing
// is a moment old by the time it is read, and records leave by eviction as well
// as by acknowledgement.
func TestARecordThatWentSinceTheListingIsSkipped(t *testing.T) {
	journal := newJournal(t)
	for index := 0; index < 3; index++ {
		appendObservation(t, journal)
	}
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if _, err := journal.Acknowledge(
		[]uint64{entries[0].Sequence}); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	transport := &acceptingTransport{}
	uploader := newTestUploader(t, journal, transport)
	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if transport.events != 2 {
		t.Fatalf("the pass sent %d events, want the 2 still held", transport.events)
	}
}

// Oldest first, because the sequence is what the server's cursor advances
// along. A later record sent before an earlier one opens a gap the repair path
// then has to close, for nothing.
func TestAPassSendsTheOldestRecordsFirst(t *testing.T) {
	journal := newJournal(t)
	for index := 0; index < MaxBatchEvents+10; index++ {
		appendObservation(t, journal)
	}
	held, err := journal.Sequences()
	if err != nil {
		t.Fatalf("sequences: %v", err)
	}

	sent := &recordingTransport{}
	uploader := newTestUploader(t, journal, sent)
	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(sent.sequences) != MaxBatchEvents {
		t.Fatalf("the pass sent %d sequences", len(sent.sequences))
	}
	for index, sequence := range sent.sequences {
		if sequence != held[index] {
			t.Fatalf("position %d sent sequence %d, the oldest held there is %d",
				index, sequence, held[index])
		}
	}
}

type recordingTransport struct {
	sequences []uint64
}

func (transport *recordingTransport) Upload(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (Acknowledgement, error) {
	batch, err := DecodeBatch(body)
	if err != nil {
		return Acknowledgement{}, err
	}
	accepted := make([]metadata.UUID, 0, len(batch.Events))
	highest := uint64(0)
	for _, item := range batch.Events {
		transport.sequences = append(transport.sequences, item.Metadata.Sequence)
		accepted = append(accepted, item.Metadata.EventID)
		if item.Metadata.Sequence > highest {
			highest = item.Metadata.Sequence
		}
	}
	return Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           envelope.Envelope.NodeID,
		RequestID:        envelope.Envelope.RequestID,
		AcceptedEventIDs: accepted,
		HighWatermark:    highest,
	}, nil
}

func newTestUploader(
	t *testing.T,
	journal *spool.Spool,
	transport Transport,
) *Uploader {
	t.Helper()
	randomBytes := make([]byte, 4096)
	for index := range randomBytes {
		randomBytes[index] = byte(index%251 + 1)
	}
	uploader, err := NewUploader(
		journal,
		uploaderKey(t),
		transport,
		bytes.NewReader(randomBytes),
		func() time.Time {
			return time.Date(2026, time.October, 10, 15, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("NewUploader: %v", err)
	}
	return uploader
}
