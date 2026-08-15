package telemetry

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

const (
	ProtocolVersion           = 1
	BatchSchema               = "hexroute.ingest-batch.v1"
	AcknowledgementSchema     = "hexroute.ingest-ack.v1"
	MaxBatchEvents            = 256
	MaxBatchCompressedBytes   = 1024 * 1024
	MaxBatchUncompressedBytes = 3 * 1024 * 1024
	MaxAcknowledgementBytes   = 64 * 1024
	MaxMissingRanges          = 8
	MaxMissingRangeWidth      = 512
)

type BatchEvent struct {
	Metadata metadata.Metadata `json:"metadata"`
	Record   json.RawMessage   `json:"record"`
}

type Batch struct {
	Schema        string        `json:"schema"`
	Version       uint16        `json:"version"`
	BatchID       metadata.UUID `json:"batch_id"`
	NodeID        metadata.UUID `json:"node_id"`
	FirstSequence uint64        `json:"first_sequence"`
	LastSequence  uint64        `json:"last_sequence"`
	Events        []BatchEvent  `json:"events"`
}

type Acknowledgement struct {
	Schema           string          `json:"schema"`
	Version          uint16          `json:"version"`
	BatchID          metadata.UUID   `json:"batch_id"`
	NodeID           metadata.UUID   `json:"node_id"`
	RequestID        metadata.UUID   `json:"request_id"`
	HighWatermark    uint64          `json:"high_watermark"`
	AcceptedEventIDs []metadata.UUID `json:"accepted_event_ids"`
	MissingSequences []SequenceRange `json:"missing_sequences,omitempty"`
}

type SequenceRange struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

var (
	ErrInvalidBatch            = errors.New("invalid telemetry batch")
	ErrBatchTooLarge           = errors.New("telemetry batch exceeds size limit")
	ErrInvalidAcknowledgement  = errors.New("invalid telemetry acknowledgement")
	ErrAcknowledgementMismatch = errors.New("telemetry acknowledgement does not match request")
)

func EncodeBatch(batchID metadata.UUID, entries []spool.Entry) ([]byte, error) {
	if _, err := metadata.ParseUUID(string(batchID)); err != nil {
		return nil, ErrInvalidBatch
	}
	if len(entries) == 0 || len(entries) > MaxBatchEvents {
		return nil, ErrInvalidBatch
	}

	events := make([]BatchEvent, 0, len(entries))
	for _, entry := range entries {
		if err := metadata.Validate(entry.Metadata); err != nil ||
			entry.Metadata.Sequence != entry.Sequence {
			return nil, ErrInvalidBatch
		}
		if _, err := event.Decode(entry.Event); err != nil {
			return nil, ErrInvalidBatch
		}
		events = append(events, BatchEvent{
			Metadata: entry.Metadata,
			Record:   append(json.RawMessage(nil), entry.Event...),
		})
	}
	batch := Batch{
		Schema:        BatchSchema,
		Version:       ProtocolVersion,
		BatchID:       batchID,
		NodeID:        events[0].Metadata.NodeID,
		FirstSequence: events[0].Metadata.Sequence,
		LastSequence:  events[len(events)-1].Metadata.Sequence,
		Events:        events,
	}
	if err := validateBatch(batch); err != nil {
		return nil, err
	}
	return encodeGzip(batch)
}

func DecodeBatch(compressed []byte) (Batch, error) {
	if len(compressed) == 0 || len(compressed) > MaxBatchCompressedBytes {
		return Batch{}, ErrBatchTooLarge
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return Batch{}, ErrInvalidBatch
	}
	uncompressed, readErr := io.ReadAll(io.LimitReader(reader, MaxBatchUncompressedBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return Batch{}, ErrInvalidBatch
	}
	if len(uncompressed) > MaxBatchUncompressedBytes {
		return Batch{}, ErrBatchTooLarge
	}

	var batch Batch
	if err := decodeStrict(uncompressed, &batch); err != nil {
		return Batch{}, ErrInvalidBatch
	}
	if err := validateBatch(batch); err != nil {
		return Batch{}, err
	}
	return batch, nil
}

func EncodeAcknowledgement(acknowledgement Acknowledgement) ([]byte, error) {
	if err := validateAcknowledgement(acknowledgement); err != nil {
		return nil, err
	}
	return json.Marshal(acknowledgement)
}

func DecodeAcknowledgement(encoded []byte) (Acknowledgement, error) {
	if len(encoded) == 0 || len(encoded) > MaxAcknowledgementBytes {
		return Acknowledgement{}, ErrInvalidAcknowledgement
	}
	var acknowledgement Acknowledgement
	if err := decodeStrict(encoded, &acknowledgement); err != nil {
		return Acknowledgement{}, ErrInvalidAcknowledgement
	}
	if err := validateAcknowledgement(acknowledgement); err != nil {
		return Acknowledgement{}, err
	}
	return acknowledgement, nil
}

func ApplyAcknowledgement(
	journal *spool.Spool,
	expectedBatchID metadata.UUID,
	expectedNodeID metadata.UUID,
	expectedRequestID metadata.UUID,
	acknowledgement Acknowledgement,
) (int, error) {
	if journal == nil ||
		acknowledgement.BatchID != expectedBatchID ||
		acknowledgement.NodeID != expectedNodeID ||
		acknowledgement.RequestID != expectedRequestID {
		return 0, ErrAcknowledgementMismatch
	}
	if err := validateAcknowledgement(acknowledgement); err != nil {
		return 0, err
	}
	return journal.Acknowledge(acknowledgement.AcceptedEventIDs)
}

func SequenceGaps(batch Batch, expectedNext uint64) ([]SequenceRange, error) {
	if expectedNext == 0 {
		return nil, ErrInvalidBatch
	}
	if err := validateBatch(batch); err != nil {
		return nil, err
	}

	next := expectedNext
	gaps := make([]SequenceRange, 0)
	for _, item := range batch.Events {
		sequence := item.Metadata.Sequence
		if sequence < next {
			continue
		}
		if sequence > next {
			gaps = append(gaps, SequenceRange{
				First: next,
				Last:  sequence - 1,
			})
		}
		next = sequence + 1
	}
	return gaps, nil
}

func validateBatch(batch Batch) error {
	if batch.Schema != BatchSchema || batch.Version != ProtocolVersion ||
		len(batch.Events) == 0 || len(batch.Events) > MaxBatchEvents {
		return ErrInvalidBatch
	}
	if _, err := metadata.ParseUUID(string(batch.BatchID)); err != nil {
		return ErrInvalidBatch
	}
	if _, err := metadata.ParseUUID(string(batch.NodeID)); err != nil {
		return ErrInvalidBatch
	}

	seenEventIDs := make(map[metadata.UUID]struct{}, len(batch.Events))
	var previousSequence uint64
	for index, item := range batch.Events {
		if err := metadata.Validate(item.Metadata); err != nil ||
			item.Metadata.NodeID != batch.NodeID {
			return ErrInvalidBatch
		}
		if index > 0 && item.Metadata.Sequence <= previousSequence {
			return ErrInvalidBatch
		}
		if _, duplicate := seenEventIDs[item.Metadata.EventID]; duplicate {
			return ErrInvalidBatch
		}
		if _, err := event.Decode(item.Record); err != nil {
			return ErrInvalidBatch
		}
		seenEventIDs[item.Metadata.EventID] = struct{}{}
		previousSequence = item.Metadata.Sequence
	}
	if batch.FirstSequence != batch.Events[0].Metadata.Sequence ||
		batch.LastSequence != batch.Events[len(batch.Events)-1].Metadata.Sequence {
		return ErrInvalidBatch
	}
	return nil
}

func validateAcknowledgement(acknowledgement Acknowledgement) error {
	if acknowledgement.Schema != AcknowledgementSchema ||
		acknowledgement.Version != ProtocolVersion ||
		len(acknowledgement.AcceptedEventIDs) > MaxBatchEvents ||
		acknowledgement.HighWatermark == 0 ||
		len(acknowledgement.MissingSequences) > MaxMissingRanges {
		return ErrInvalidAcknowledgement
	}
	for _, id := range []metadata.UUID{
		acknowledgement.BatchID,
		acknowledgement.NodeID,
		acknowledgement.RequestID,
	} {
		if _, err := metadata.ParseUUID(string(id)); err != nil {
			return ErrInvalidAcknowledgement
		}
	}
	seen := make(map[metadata.UUID]struct{}, len(acknowledgement.AcceptedEventIDs))
	for _, eventID := range acknowledgement.AcceptedEventIDs {
		if _, err := metadata.ParseUUID(string(eventID)); err != nil {
			return ErrInvalidAcknowledgement
		}
		if _, duplicate := seen[eventID]; duplicate {
			return ErrInvalidAcknowledgement
		}
		seen[eventID] = struct{}{}
	}
	if len(acknowledgement.MissingSequences) > 0 {
		if err := validateMissingRanges(
			acknowledgement.MissingSequences,
			acknowledgement.HighWatermark,
		); err != nil {
			return err
		}
	}
	return nil
}

func encodeGzip(batch Batch) ([]byte, error) {
	uncompressed, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBatch, err)
	}
	if len(uncompressed) > MaxBatchUncompressedBytes {
		return nil, ErrBatchTooLarge
	}

	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	writer.Header.ModTime = time.Unix(0, 0).UTC()
	writer.Header.OS = 255
	if _, err := writer.Write(uncompressed); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if output.Len() > MaxBatchCompressedBytes {
		return nil, ErrBatchTooLarge
	}
	return output.Bytes(), nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidBatch
	}
	return nil
}
