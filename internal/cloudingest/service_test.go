package cloudingest

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
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

const (
	cloudNodeID    = metadata.UUID("11111111-1111-4111-8111-111111111111")
	cloudSessionID = metadata.UUID("22222222-2222-4222-8222-222222222222")
	cloudBatchID   = metadata.UUID("33333333-3333-4333-8333-333333333333")
	cloudRequestID = metadata.UUID("44444444-4444-4444-8444-444444444444")
)

func TestServiceAcceptsAuthenticatedAllowlistedBatch(t *testing.T) {
	now := time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC)
	key := cloudSigningKey(t)
	store := &fakeStore{key: keyRecord(key, now)}
	service := newTestService(t, store, now)
	body := encodedTestBatch(t, cloudBatchID, testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	signed := signedTestBatch(t, key, cloudRequestID, now, body)

	acknowledgement, err := service.Accept(context.Background(), signed, body)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if acknowledgement.BatchID != cloudBatchID ||
		acknowledgement.NodeID != cloudNodeID ||
		len(acknowledgement.AcceptedEventIDs) != 1 {
		t.Fatalf("Accept() acknowledgement = %+v", acknowledgement)
	}
	if len(store.accepted) != 1 || len(store.audits) != 0 {
		t.Fatalf("store accepted=%d audits=%d", len(store.accepted), len(store.audits))
	}
	request := store.accepted[0]
	if request.RequestID != cloudRequestID ||
		request.SigningKeyID != key.KeyID ||
		request.FirstSequence != 1 ||
		request.LastSequence != 1 ||
		len(request.Events) != 1 {
		t.Fatalf("stored request = %+v", request)
	}
	if request.Events[0].Schema != event.SchemaObservation ||
		request.Events[0].Priority != event.PriorityOperational {
		t.Fatalf("stored event = %+v", request.Events[0])
	}
}

func TestServiceRejectsInvalidSignatureAndWritesBoundedAudit(t *testing.T) {
	now := time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC)
	key := cloudSigningKey(t)
	store := &fakeStore{key: keyRecord(key, now)}
	service := newTestService(t, store, now)
	body := encodedTestBatch(t, cloudBatchID, testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	signed := signedTestBatch(t, key, cloudRequestID, now, body)
	signed.Signature = "invalid"

	if _, err := service.Accept(
		context.Background(),
		signed,
		body,
	); !errors.Is(err, ErrRejected) || !errors.Is(err, signing.ErrInvalidSignature) {
		t.Fatalf("Accept(invalid signature) error = %v", err)
	}
	if len(store.accepted) != 0 || len(store.audits) != 1 {
		t.Fatalf("store accepted=%d audits=%d", len(store.accepted), len(store.audits))
	}
	audit := store.audits[0]
	if audit.Category != AuditSignature ||
		audit.ReasonCode != "invalid_signature" ||
		audit.NodeID != cloudNodeID ||
		audit.RequestID != cloudRequestID {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestServiceRejectsReplayAndMalformedBatchWithoutPayloadAudit(t *testing.T) {
	now := time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC)
	key := cloudSigningKey(t)

	replayStore := &fakeStore{
		key:       keyRecord(key, now),
		acceptErr: ErrReplay,
	}
	replayService := newTestService(t, replayStore, now)
	body := encodedTestBatch(t, cloudBatchID, testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	signed := signedTestBatch(t, key, cloudRequestID, now, body)
	if _, err := replayService.Accept(
		context.Background(),
		signed,
		body,
	); !errors.Is(err, ErrReplay) || !errors.Is(err, ErrRejected) {
		t.Fatalf("Accept(replay) error = %v", err)
	}
	if len(replayStore.audits) != 1 ||
		replayStore.audits[0].Category != AuditReplay ||
		replayStore.audits[0].ReasonCode != "request_reused" {
		t.Fatalf("replay audit = %+v", replayStore.audits)
	}

	malformedStore := &fakeStore{key: keyRecord(key, now)}
	malformedService := newTestService(t, malformedStore, now)
	malformedBody := []byte("not a gzip batch")
	malformedSigned := signedTestBatch(
		t,
		key,
		metadata.UUID("55555555-5555-4555-8555-555555555555"),
		now,
		malformedBody,
	)
	if _, err := malformedService.Accept(
		context.Background(),
		malformedSigned,
		malformedBody,
	); !errors.Is(err, telemetry.ErrInvalidBatch) {
		t.Fatalf("Accept(malformed) error = %v", err)
	}
	if len(malformedStore.audits) != 1 ||
		malformedStore.audits[0].Category != AuditSchema ||
		malformedStore.audits[0].ReasonCode != "invalid_batch" {
		t.Fatalf("malformed audit = %+v", malformedStore.audits)
	}
}

func TestServiceRejectsInactiveAndUnknownKeys(t *testing.T) {
	now := time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC)
	key := cloudSigningKey(t)
	body := encodedTestBatch(t, cloudBatchID, testEntry(t, 1, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	signed := signedTestBatch(t, key, cloudRequestID, now, body)

	inactive := keyRecord(key, now)
	inactive.Status = signing.KeyRetired
	inactiveStore := &fakeStore{key: inactive}
	inactiveService := newTestService(t, inactiveStore, now)
	if _, err := inactiveService.Accept(
		context.Background(),
		signed,
		body,
	); !errors.Is(err, signing.ErrRevokedKey) {
		t.Fatalf("Accept(inactive key) error = %v", err)
	}
	if len(inactiveStore.audits) != 1 ||
		inactiveStore.audits[0].ReasonCode != "inactive_key" {
		t.Fatalf("inactive audit = %+v", inactiveStore.audits)
	}

	unknownStore := &fakeStore{lookupErr: ErrKeyNotFound}
	unknownService := newTestService(t, unknownStore, now)
	if _, err := unknownService.Accept(
		context.Background(),
		signed,
		body,
	); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Accept(unknown key) error = %v", err)
	}
	if len(unknownStore.audits) != 1 ||
		unknownStore.audits[0].ReasonCode != "unknown_key" {
		t.Fatalf("unknown audit = %+v", unknownStore.audits)
	}
}

type fakeStore struct {
	key       KeyRecord
	lookupErr error
	acceptErr error
	auditErr  error
	accepted  []BatchRequest
	audits    []AuditRecord
}

func (store *fakeStore) LookupKey(
	_ context.Context,
	_ metadata.UUID,
	_ metadata.UUID,
) (KeyRecord, error) {
	return store.key, store.lookupErr
}

func (store *fakeStore) AcceptBatch(
	_ context.Context,
	request BatchRequest,
) error {
	store.accepted = append(store.accepted, request)
	return store.acceptErr
}

func (store *fakeStore) RecordAudit(
	_ context.Context,
	record AuditRecord,
) error {
	store.audits = append(store.audits, record)
	return store.auditErr
}

func newTestService(t *testing.T, store Store, now time.Time) *Service {
	t.Helper()
	service, err := NewService(
		store,
		5*time.Minute,
		bytes.NewReader(make([]byte, 16*16)),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func cloudSigningKey(t *testing.T) signing.Key {
	t.Helper()
	randomBytes := make([]byte, ed25519.SeedSize+16)
	randomBytes[0] = 9
	key, err := signing.GenerateFile(
		filepath.Join(t.TempDir(), "node.json"),
		cloudNodeID,
		bytes.NewReader(randomBytes),
	)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	return key
}

func keyRecord(key signing.Key, now time.Time) KeyRecord {
	return KeyRecord{
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: key.PublicKey(),
		Status:    signing.KeyActive,
		ValidFrom: now.Add(-time.Hour),
	}
}

func signedTestBatch(
	t *testing.T,
	key signing.Key,
	requestID metadata.UUID,
	now time.Time,
	body []byte,
) signing.SignedEnvelope {
	t.Helper()
	signed, err := signing.Sign(key, requestID, now, body)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	return signed
}

func encodedTestBatch(
	t *testing.T,
	batchID metadata.UUID,
	entries ...spool.Entry,
) []byte {
	t.Helper()
	body, err := telemetry.EncodeBatch(batchID, entries)
	if err != nil {
		t.Fatalf("EncodeBatch() error = %v", err)
	}
	return body
}

func testEntry(
	t *testing.T,
	sequence uint64,
	eventID metadata.UUID,
) spool.Entry {
	t.Helper()
	encoded, err := event.Encode(event.SchemaObservation, event.Observation{
		Component: control.ComponentNetwork,
		Health:    control.HealthReady,
		Reason:    control.ReasonProbeSucceeded,
	})
	if err != nil {
		t.Fatalf("event.Encode() error = %v", err)
	}
	return spool.Entry{
		Sequence: sequence,
		Priority: event.PriorityOperational,
		Metadata: metadata.Metadata{
			EventID:        eventID,
			NodeID:         cloudNodeID,
			SessionID:      cloudSessionID,
			Sequence:       sequence,
			WallClock:      time.Date(2026, time.July, 24, 18, 0, int(sequence), 0, time.UTC),
			MonotonicNanos: int64(sequence),
		},
		Event: encoded,
	}
}
