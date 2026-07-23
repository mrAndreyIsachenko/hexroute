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
