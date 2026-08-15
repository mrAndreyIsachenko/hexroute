package telemetry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

func TestIncidentBundleContainsOnlyValidatedEvents(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	createdAt := time.Date(2026, time.July, 23, 20, 0, 0, 0, time.UTC)
	encoded, err := EncodeIncidentBundle("incident-01", createdAt, entries)
	if err != nil {
		t.Fatalf("EncodeIncidentBundle() error = %v", err)
	}
	decoded, err := DecodeIncidentBundle(encoded)
	if err != nil {
		t.Fatalf("DecodeIncidentBundle() error = %v", err)
	}
	if decoded.IncidentID != "incident-01" || len(decoded.Events) != 1 {
		t.Fatalf("DecodeIncidentBundle() = %+v", decoded)
	}

	corrupt := entries[0]
	corrupt.Event = []byte(`{"schema":"raw.log","secret":"value"}`)
	if _, err := EncodeIncidentBundle(
		"incident-01",
		createdAt,
		[]spool.Entry{corrupt},
	); !errors.Is(err, ErrInvalidIncidentBundle) {
		t.Fatalf("EncodeIncidentBundle(corrupt) error = %v, want %v", err, ErrInvalidIncidentBundle)
	}
}

func TestUploadFailureDoesNotBlockStateTransitionOrDeleteSpool(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	key := uploaderKey(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	transport := &blockingFailTransport{entered: entered, release: release}
	randomBytes := make([]byte, 32)
	randomBytes[0] = 4
	randomBytes[16] = 5
	uploader, err := NewUploader(
		journal,
		key,
		transport,
		bytes.NewReader(randomBytes),
		func() time.Time {
			return time.Date(2026, time.July, 23, 20, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	uploadResult := make(chan error, 1)
	go func() {
		uploadResult <- uploader.RunOnce(context.Background())
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("uploader did not reach transport")
	}

	machine, err := control.NewMachine(
		control.Policy{
			FailureThreshold:   2,
			ActionBudget:       2,
			BaseBackoff:        1,
			MaxBackoff:         4,
			VerificationWindow: 1,
			Cooldown:           10,
		},
		control.NewSnapshot(control.StateHealthy),
	)
	if err != nil {
		t.Fatalf("NewMachine() error = %v", err)
	}
	decision, err := machine.Step(0, 1, control.EventProbeFailed)
	if err != nil || decision.Reason != control.ReasonProbeFailed {
		t.Fatalf("Step() = %+v, %v", decision, err)
	}

	close(release)
	if err := <-uploadResult; !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("RunOnce() error = %v, want %v", err, ErrUploadFailed)
	}
	entries, err := journal.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool after failed upload = %d entries, %v; want 1, nil", len(entries), err)
	}
}

func TestCloudLossRetainsEvidenceWhileLocalRecoveryContinuesAndDrainsOnReturn(
	t *testing.T,
) {
	journal := newJournal(t)
	appendObservation(t, journal)
	key := uploaderKey(t)
	transport := &recoveringTransport{failuresRemaining: 2}
	randomBytes := make([]byte, 32*3)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 1)
	}
	uploader, err := NewUploader(
		journal,
		key,
		transport,
		bytes.NewReader(randomBytes),
		func() time.Time {
			return time.Date(2026, time.July, 25, 13, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}
	machine, err := control.NewMachine(
		control.Policy{
			FailureThreshold:   1,
			ActionBudget:       2,
			BaseBackoff:        1,
			MaxBackoff:         4,
			VerificationWindow: 1,
			Cooldown:           10,
		},
		control.NewSnapshot(control.StateHealthy),
	)
	if err != nil {
		t.Fatalf("NewMachine() error = %v", err)
	}

	if err := uploader.RunOnce(context.Background()); !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("RunOnce(first outage) error = %v, want %v", err, ErrUploadFailed)
	}
	failed, err := machine.Step(0, 1, control.EventProbeFailed)
	if err != nil || failed.To != control.StateDegraded {
		t.Fatalf("Step(failed) = %+v, %v", failed, err)
	}
	recovery, err := machine.Step(failed.Generation, 2, control.EventBeginRecovery)
	if err != nil || !recovery.RecoveryApproved ||
		recovery.To != control.StateRecovering {
		t.Fatalf("Step(recovery) = %+v, %v", recovery, err)
	}

	if err := uploader.RunOnce(context.Background()); !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("RunOnce(second outage) error = %v, want %v", err, ErrUploadFailed)
	}
	verified, err := machine.Step(recovery.Generation, 3, control.EventProbeSucceeded)
	if err != nil || verified.To != control.StateHealthy {
		t.Fatalf("Step(verified) = %+v, %v", verified, err)
	}
	entries, err := journal.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool during outage = %d entries, %v; want 1, nil", len(entries), err)
	}

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(cloud restored) error = %v", err)
	}
	entries, err = journal.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool after acknowledgement = %d entries, %v; want 0, nil", len(entries), err)
	}
	if transport.attempts != 3 {
		t.Fatalf("upload attempts = %d, want 3", transport.attempts)
	}
}

func TestUploaderReplaysRetainedGapExactlyOnce(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	appendObservation(t, journal)
	original, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	key := uploaderKey(t)
	transport := &gapReplayTransport{}
	uploader, err := NewUploader(
		journal,
		key,
		transport,
		bytes.NewReader(repeatedBytes(64)),
		func() time.Time {
			return time.Date(2026, time.August, 15, 13, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(transport.batches) != 2 {
		t.Fatalf("uploads = %d, want 2", len(transport.batches))
	}
	replay := transport.batches[1]
	if len(replay.Events) != 1 ||
		replay.Events[0].Metadata.Sequence != 2 ||
		!bytes.Equal(replay.Events[0].Record, original[1].Event) {
		t.Fatalf("replay batch = %+v", replay)
	}
	if transport.envelopes[0].Envelope.RequestID == transport.envelopes[1].Envelope.RequestID {
		t.Fatal("replay reused the original request identity")
	}
	remaining, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries(after replay) error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].Sequence != 1 {
		t.Fatalf("remaining entries = %+v", remaining)
	}
}

func TestUploaderCanDisableGapRepairWithoutDisablingBaseUpload(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	appendTransition(t, journal)
	appendObservation(t, journal)
	key := uploaderKey(t)
	transport := &gapReplayTransport{}
	uploader, err := NewUploader(
		journal,
		key,
		transport,
		bytes.NewReader(repeatedBytes(32)),
		func() time.Time {
			return time.Date(2026, time.August, 15, 13, 15, 0, 0, time.UTC)
		},
		WithGapRepairEnabled(false),
	)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(transport.batches) != 1 {
		t.Fatalf("uploads = %d, want 1", len(transport.batches))
	}
	remaining, err := journal.Entries()
	if err != nil {
		t.Fatalf("Entries(after disabled gap repair) error = %v", err)
	}
	if len(remaining) != 2 ||
		remaining[0].Sequence != 1 ||
		remaining[1].Sequence != 2 {
		t.Fatalf("remaining entries = %+v", remaining)
	}
}

func TestUploaderReportsExpiredGapAndAllowsNewUploads(t *testing.T) {
	journal := newJournal(t)
	appendObservation(t, journal)
	key := uploaderKey(t)
	transport := &expiredGapTransport{}
	uploader, err := NewUploader(
		journal,
		key,
		transport,
		bytes.NewReader(repeatedBytes(128)),
		func() time.Time {
			return time.Date(2026, time.August, 15, 13, 30, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("NewUploader() error = %v", err)
	}

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(first) error = %v", err)
	}
	entries, err := journal.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries after expired gap = %d, %v", len(entries), err)
	}
	record, err := event.Decode(entries[0].Event)
	if err != nil || record.Schema != event.SchemaDiagnostic {
		t.Fatalf("diagnostic record = %+v, %v", record, err)
	}
	diagnostic := record.Payload.(*event.Diagnostic)
	if diagnostic.Code != event.DiagnosticTelemetryGapUnrecoverable {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}

	if err := uploader.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(second) error = %v", err)
	}
	entries, err = journal.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries after diagnostic upload = %d, %v", len(entries), err)
	}
	if transport.calls != 2 {
		t.Fatalf("transport calls = %d, want 2", transport.calls)
	}
}

type blockingFailTransport struct {
	entered chan<- struct{}
	release <-chan struct{}
}

func (transport *blockingFailTransport) Upload(
	_ context.Context,
	_ signing.SignedEnvelope,
	_ []byte,
) (Acknowledgement, error) {
	close(transport.entered)
	<-transport.release
	return Acknowledgement{}, errors.New("offline")
}

type recoveringTransport struct {
	attempts          int
	failuresRemaining int
}

func (transport *recoveringTransport) Upload(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (Acknowledgement, error) {
	transport.attempts++
	if transport.failuresRemaining > 0 {
		transport.failuresRemaining--
		return Acknowledgement{}, errors.New("cloud unavailable")
	}
	batch, err := DecodeBatch(body)
	if err != nil {
		return Acknowledgement{}, err
	}
	eventIDs := make([]metadata.UUID, 0, len(batch.Events))
	for _, item := range batch.Events {
		eventIDs = append(eventIDs, item.Metadata.EventID)
	}
	return Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           batch.NodeID,
		RequestID:        envelope.Envelope.RequestID,
		HighWatermark:    batch.LastSequence,
		AcceptedEventIDs: eventIDs,
	}, nil
}

type gapReplayTransport struct {
	envelopes []signing.SignedEnvelope
	batches   []Batch
}

func (transport *gapReplayTransport) Upload(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (Acknowledgement, error) {
	batch, err := DecodeBatch(body)
	if err != nil {
		return Acknowledgement{}, err
	}
	transport.envelopes = append(transport.envelopes, envelope)
	transport.batches = append(transport.batches, batch)
	if len(transport.batches) == 1 {
		return Acknowledgement{
			Schema:           AcknowledgementSchema,
			Version:          ProtocolVersion,
			BatchID:          batch.BatchID,
			NodeID:           batch.NodeID,
			RequestID:        envelope.Envelope.RequestID,
			HighWatermark:    batch.LastSequence,
			AcceptedEventIDs: []metadata.UUID{batch.Events[len(batch.Events)-1].Metadata.EventID},
			MissingSequences: []SequenceRange{{First: 2, Last: 2}},
		}, nil
	}
	if len(batch.Events) != 1 || batch.Events[0].Metadata.Sequence != 2 {
		return Acknowledgement{}, ErrInvalidBatch
	}
	return Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           batch.NodeID,
		RequestID:        envelope.Envelope.RequestID,
		HighWatermark:    batch.LastSequence,
		AcceptedEventIDs: []metadata.UUID{batch.Events[0].Metadata.EventID},
	}, nil
}

type expiredGapTransport struct {
	calls int
}

func (transport *expiredGapTransport) Upload(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (Acknowledgement, error) {
	transport.calls++
	batch, err := DecodeBatch(body)
	if err != nil {
		return Acknowledgement{}, err
	}
	eventIDs := make([]metadata.UUID, 0, len(batch.Events))
	for _, item := range batch.Events {
		eventIDs = append(eventIDs, item.Metadata.EventID)
	}
	acknowledgement := Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           batch.NodeID,
		RequestID:        envelope.Envelope.RequestID,
		HighWatermark:    batch.LastSequence,
		AcceptedEventIDs: eventIDs,
	}
	if transport.calls == 1 {
		acknowledgement.HighWatermark = 2
		acknowledgement.MissingSequences = []SequenceRange{{First: 2, Last: 2}}
	}
	return acknowledgement, nil
}

func repeatedBytes(count int) []byte {
	output := make([]byte, count)
	for index := range output {
		output[index] = byte(index + 1)
	}
	return output
}

func uploaderKey(t *testing.T) signing.Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.json")
	randomBytes := make([]byte, ed25519.SeedSize+16)
	randomBytes[0] = 9
	key, err := signing.GenerateFile(path, testNodeID, bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	return key
}
