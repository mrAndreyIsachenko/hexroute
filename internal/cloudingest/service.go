package cloudingest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

type AuditCategory string

const (
	AuditSignature AuditCategory = "signature"
	AuditReplay    AuditCategory = "replay"
	AuditTimestamp AuditCategory = "timestamp"
	AuditSchema    AuditCategory = "schema"
	AuditSize      AuditCategory = "size"
)

type KeyRecord struct {
	NodeID     metadata.UUID
	KeyID      metadata.UUID
	PublicKey  ed25519.PublicKey
	Status     signing.KeyStatus
	ValidFrom  time.Time
	ValidUntil time.Time
}

type StoredEvent struct {
	Metadata metadata.Metadata
	Schema   event.Schema
	Version  uint16
	Priority event.Priority
	Payload  json.RawMessage
}

type BatchRequest struct {
	RequestID       metadata.UUID
	BatchID         metadata.UUID
	NodeID          metadata.UUID
	SigningKeyID    metadata.UUID
	ProtocolVersion uint16
	FirstSequence   uint64
	LastSequence    uint64
	CompressedBytes int
	ContentSHA256   [32]byte
	SignedAt        time.Time
	ReceivedAt      time.Time
	Events          []StoredEvent
}

type AcceptResult struct {
	HighWatermark    uint64
	MissingSequences []telemetry.SequenceRange
}

type AuditRecord struct {
	AuditRecordID metadata.UUID
	NodeID        metadata.UUID
	RequestID     metadata.UUID
	Category      AuditCategory
	ReasonCode    string
	OccurredAt    time.Time
}

type Store interface {
	LookupKey(context.Context, metadata.UUID, metadata.UUID) (KeyRecord, error)
	AcceptBatch(context.Context, BatchRequest) (AcceptResult, error)
	RecordAudit(context.Context, AuditRecord) error
}

type Service struct {
	store     Store
	tolerance time.Duration
	random    io.Reader
	now       func() time.Time
}

var (
	ErrRejected         = errors.New("ingest request rejected")
	ErrUnavailable      = errors.New("ingest storage unavailable")
	ErrKeyNotFound      = errors.New("ingest signing key not found")
	ErrReplay           = errors.New("ingest request replay")
	ErrBatchConflict    = errors.New("ingest batch identity conflict")
	ErrEventConflict    = errors.New("ingest event identity conflict")
	ErrSequenceConflict = errors.New("ingest sequence conflict")
	ErrInvalidRequest   = errors.New("invalid ingest request")
)

func NewService(
	store Store,
	tolerance time.Duration,
	random io.Reader,
	now func() time.Time,
) (*Service, error) {
	if store == nil || tolerance <= 0 {
		return nil, ErrInvalidRequest
	}
	if random == nil {
		random = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:     store,
		tolerance: tolerance,
		random:    random,
		now:       now,
	}, nil
}

func (service *Service) Accept(
	ctx context.Context,
	signed signing.SignedEnvelope,
	body []byte,
) (telemetry.Acknowledgement, error) {
	now := service.now().UTC()
	nodeID, requestID, idsValid := envelopeIDs(signed)
	if !idsValid {
		service.audit(ctx, "", "", AuditSchema, "invalid_envelope", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, ErrInvalidRequest)
	}
	if len(body) == 0 {
		service.audit(ctx, nodeID, requestID, AuditSchema, "invalid_batch", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, telemetry.ErrInvalidBatch)
	}
	if len(body) > telemetry.MaxBatchCompressedBytes {
		service.audit(ctx, nodeID, requestID, AuditSize, "batch_too_large", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, telemetry.ErrBatchTooLarge)
	}

	key, err := service.store.LookupKey(
		ctx,
		signed.Envelope.NodeID,
		signed.Envelope.KeyID,
	)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			service.audit(ctx, nodeID, requestID, AuditSignature, "unknown_key", now)
			return telemetry.Acknowledgement{}, errors.Join(ErrRejected, ErrKeyNotFound)
		}
		return telemetry.Acknowledgement{}, errors.Join(ErrUnavailable, err)
	}
	registered := signing.RegisteredKey{
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: key.PublicKey,
		Status:    key.Status,
	}
	if err := signing.VerifyAuthenticity(
		signed,
		body,
		now,
		service.tolerance,
		registered,
	); err != nil {
		category, reason := verificationAudit(err)
		service.audit(ctx, nodeID, requestID, category, reason, now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
	}
	signedAt := mustParseTimestamp(signed.Envelope.Timestamp)
	if signedAt.Before(key.ValidFrom) ||
		(!key.ValidUntil.IsZero() && !signedAt.Before(key.ValidUntil)) {
		service.audit(ctx, nodeID, requestID, AuditSignature, "inactive_key", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, signing.ErrRevokedKey)
	}

	batch, err := telemetry.DecodeBatch(body)
	if err != nil {
		category := AuditSchema
		reason := "invalid_batch"
		if errors.Is(err, telemetry.ErrBatchTooLarge) {
			category = AuditSize
			reason = "batch_too_large"
		}
		service.audit(ctx, nodeID, requestID, category, reason, now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
	}
	if batch.NodeID != signed.Envelope.NodeID {
		service.audit(ctx, nodeID, requestID, AuditSchema, "node_mismatch", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, ErrInvalidRequest)
	}

	events, err := prepareEvents(batch)
	if err != nil {
		service.audit(ctx, nodeID, requestID, AuditSchema, "invalid_event", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
	}
	digest, err := hex.DecodeString(signed.Envelope.BodySHA256)
	if err != nil || len(digest) != 32 {
		service.audit(ctx, nodeID, requestID, AuditSchema, "invalid_digest", now)
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, ErrInvalidRequest)
	}
	var contentSHA256 [32]byte
	copy(contentSHA256[:], digest)

	request := BatchRequest{
		RequestID:       signed.Envelope.RequestID,
		BatchID:         batch.BatchID,
		NodeID:          batch.NodeID,
		SigningKeyID:    signed.Envelope.KeyID,
		ProtocolVersion: batch.Version,
		FirstSequence:   batch.FirstSequence,
		LastSequence:    batch.LastSequence,
		CompressedBytes: len(body),
		ContentSHA256:   contentSHA256,
		SignedAt:        signedAt,
		ReceivedAt:      now,
		Events:          events,
	}
	result, err := service.store.AcceptBatch(ctx, request)
	if err != nil {
		switch {
		case errors.Is(err, ErrReplay):
			service.audit(ctx, nodeID, requestID, AuditReplay, "request_reused", now)
			return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
		case errors.Is(err, ErrBatchConflict):
			service.audit(ctx, nodeID, requestID, AuditReplay, "batch_reused", now)
			return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
		case errors.Is(err, ErrEventConflict):
			service.audit(ctx, nodeID, requestID, AuditSchema, "event_conflict", now)
			return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
		case errors.Is(err, ErrSequenceConflict):
			service.audit(ctx, nodeID, requestID, AuditSchema, "sequence_conflict", now)
			return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
		default:
			return telemetry.Acknowledgement{}, errors.Join(ErrUnavailable, err)
		}
	}

	accepted := make([]metadata.UUID, 0, len(batch.Events))
	for _, item := range batch.Events {
		accepted = append(accepted, item.Metadata.EventID)
	}
	highWatermark := result.HighWatermark
	if highWatermark == 0 || highWatermark < batch.LastSequence {
		highWatermark = batch.LastSequence
	}
	acknowledgement := telemetry.Acknowledgement{
		Schema:           telemetry.AcknowledgementSchema,
		Version:          telemetry.ProtocolVersion,
		BatchID:          batch.BatchID,
		NodeID:           batch.NodeID,
		RequestID:        signed.Envelope.RequestID,
		HighWatermark:    highWatermark,
		AcceptedEventIDs: accepted,
		MissingSequences: result.MissingSequences,
	}
	if _, err := telemetry.EncodeAcknowledgement(acknowledgement); err != nil {
		return telemetry.Acknowledgement{}, errors.Join(ErrRejected, err)
	}
	return acknowledgement, nil
}

func prepareEvents(batch telemetry.Batch) ([]StoredEvent, error) {
	events := make([]StoredEvent, 0, len(batch.Events))
	for _, item := range batch.Events {
		if item.Metadata.Sequence > math.MaxInt64 {
			return nil, ErrInvalidRequest
		}
		record, err := event.Decode(item.Record)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(record.Payload)
		if err != nil || len(payload) == 0 {
			return nil, ErrInvalidRequest
		}
		events = append(events, StoredEvent{
			Metadata: item.Metadata,
			Schema:   record.Schema,
			Version:  record.Version,
			Priority: record.Priority,
			Payload:  payload,
		})
	}
	return events, nil
}

func envelopeIDs(
	signed signing.SignedEnvelope,
) (metadata.UUID, metadata.UUID, bool) {
	nodeID, nodeErr := metadata.ParseUUID(string(signed.Envelope.NodeID))
	if nodeErr != nil {
		return "", "", false
	}
	if _, err := metadata.ParseUUID(string(signed.Envelope.KeyID)); err != nil {
		return "", "", false
	}
	requestID, requestErr := metadata.ParseUUID(string(signed.Envelope.RequestID))
	if requestErr != nil {
		return "", "", false
	}
	return nodeID, requestID, true
}

func verificationAudit(err error) (AuditCategory, string) {
	switch {
	case errors.Is(err, signing.ErrTimestamp):
		return AuditTimestamp, "timestamp_outside_window"
	case errors.Is(err, signing.ErrUnknownKey):
		return AuditSignature, "unknown_key"
	case errors.Is(err, signing.ErrRevokedKey):
		return AuditSignature, "inactive_key"
	case errors.Is(err, signing.ErrInvalidSignature):
		return AuditSignature, "invalid_signature"
	default:
		return AuditSchema, "invalid_envelope"
	}
}

func (service *Service) audit(
	ctx context.Context,
	nodeID metadata.UUID,
	requestID metadata.UUID,
	category AuditCategory,
	reason string,
	now time.Time,
) {
	auditID, err := metadata.NewUUID(service.random)
	if err != nil {
		return
	}
	_ = service.store.RecordAudit(ctx, AuditRecord{
		AuditRecordID: auditID,
		NodeID:        nodeID,
		RequestID:     requestID,
		Category:      category,
		ReasonCode:    reason,
		OccurredAt:    now,
	})
}

func mustParseTimestamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
