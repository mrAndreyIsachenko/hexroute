package telemetry

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

const (
	IncidentBundleSchema             = "hexroute.incident-bundle.v1"
	MaxIncidentBundleEvents          = 128
	MaxIncidentBundleCompressedBytes = 1024 * 1024
)

type IncidentBundle struct {
	Schema     string       `json:"schema"`
	Version    uint16       `json:"version"`
	IncidentID string       `json:"incident_id"`
	CreatedAt  string       `json:"created_at"`
	Events     []BatchEvent `json:"events"`
}

var ErrInvalidIncidentBundle = errors.New("invalid incident bundle")

func EncodeIncidentBundle(
	incidentID string,
	createdAt time.Time,
	entries []spool.Entry,
) ([]byte, error) {
	if !validBundleReference(incidentID) ||
		createdAt.IsZero() ||
		len(entries) == 0 ||
		len(entries) > MaxIncidentBundleEvents {
		return nil, ErrInvalidIncidentBundle
	}

	events := make([]BatchEvent, 0, len(entries))
	for _, entry := range entries {
		if err := metadata.Validate(entry.Metadata); err != nil ||
			entry.Metadata.Sequence != entry.Sequence {
			return nil, ErrInvalidIncidentBundle
		}
		if _, err := event.Decode(entry.Event); err != nil {
			return nil, ErrInvalidIncidentBundle
		}
		events = append(events, BatchEvent{
			Metadata: entry.Metadata,
			Record:   append(json.RawMessage(nil), entry.Event...),
		})
	}
	bundle := IncidentBundle{
		Schema:     IncidentBundleSchema,
		Version:    ProtocolVersion,
		IncidentID: incidentID,
		CreatedAt:  createdAt.UTC().Format(time.RFC3339Nano),
		Events:     events,
	}
	uncompressed, err := json.Marshal(bundle)
	if err != nil || len(uncompressed) > MaxBatchUncompressedBytes {
		return nil, ErrInvalidIncidentBundle
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
	if output.Len() > MaxIncidentBundleCompressedBytes {
		return nil, ErrInvalidIncidentBundle
	}
	return output.Bytes(), nil
}

func DecodeIncidentBundle(compressed []byte) (IncidentBundle, error) {
	if len(compressed) == 0 || len(compressed) > MaxIncidentBundleCompressedBytes {
		return IncidentBundle{}, ErrInvalidIncidentBundle
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return IncidentBundle{}, ErrInvalidIncidentBundle
	}
	uncompressed, readErr := io.ReadAll(io.LimitReader(reader, MaxBatchUncompressedBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(uncompressed) > MaxBatchUncompressedBytes {
		return IncidentBundle{}, ErrInvalidIncidentBundle
	}

	var bundle IncidentBundle
	if err := decodeStrict(uncompressed, &bundle); err != nil ||
		bundle.Schema != IncidentBundleSchema ||
		bundle.Version != ProtocolVersion ||
		!validBundleReference(bundle.IncidentID) ||
		len(bundle.Events) == 0 ||
		len(bundle.Events) > MaxIncidentBundleEvents {
		return IncidentBundle{}, ErrInvalidIncidentBundle
	}
	createdAt, err := time.Parse(time.RFC3339Nano, bundle.CreatedAt)
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != bundle.CreatedAt {
		return IncidentBundle{}, ErrInvalidIncidentBundle
	}
	for _, item := range bundle.Events {
		if err := metadata.Validate(item.Metadata); err != nil {
			return IncidentBundle{}, ErrInvalidIncidentBundle
		}
		if _, err := event.Decode(item.Record); err != nil {
			return IncidentBundle{}, ErrInvalidIncidentBundle
		}
	}
	return bundle, nil
}

func validBundleReference(value string) bool {
	if value == "" || len(value) > event.MaxReferenceBytes {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		if index > 0 && strings.ContainsRune(".:-", character) {
			continue
		}
		return false
	}
	return true
}
