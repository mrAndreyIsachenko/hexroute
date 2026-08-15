package cloudingest

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

const (
	cloudKeyID   = metadata.UUID("55555555-5555-4555-8555-555555555555")
	cloudEventID = metadata.UUID("66666666-6666-4666-8666-666666666666")
)

func TestHTTPTransportAndHandlerRoundTripBoundedSignedBatch(t *testing.T) {
	acknowledgement := telemetry.Acknowledgement{
		Schema:           telemetry.AcknowledgementSchema,
		Version:          telemetry.ProtocolVersion,
		BatchID:          cloudBatchID,
		NodeID:           cloudNodeID,
		RequestID:        cloudRequestID,
		HighWatermark:    7,
		AcceptedEventIDs: []metadata.UUID{cloudEventID},
	}
	acceptor := &httpAcceptorFixture{acknowledgement: acknowledgement}
	handler, err := NewHTTPHandler(acceptor)
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport, err := NewHTTPTransport(
		&http.Client{Timeout: 2 * time.Second},
		server.URL,
	)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}
	envelope := signing.SignedEnvelope{
		Envelope: signing.Envelope{
			Schema:     signing.EnvelopeSchema,
			Version:    signing.EnvelopeVersion,
			NodeID:     cloudNodeID,
			KeyID:      cloudKeyID,
			RequestID:  cloudRequestID,
			Timestamp:  "2026-07-25T14:00:00Z",
			BodySHA256: "digest",
		},
		Signature: "signature",
	}
	body := []byte("bounded-gzip-body")
	got, err := transport.Upload(context.Background(), envelope, body)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if got.BatchID != acknowledgement.BatchID ||
		len(got.AcceptedEventIDs) != 1 ||
		!bytes.Equal(acceptor.body, body) ||
		acceptor.envelope.Envelope.RequestID != cloudRequestID {
		t.Fatalf("round trip = %+v acceptor=%+v", got, acceptor)
	}
}

func TestHTTPHandlerMapsDatabaseWriteGateToFrozenStatus(t *testing.T) {
	handler, err := NewHTTPHandler(&httpAcceptorFixture{err: errors.Join(
		ErrUnavailable,
		&pgconn.PgError{Code: "55000", Message: "write_frozen"},
	)})
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, IngestPath, bytes.NewReader([]byte("body")))
	request.Header.Set("Content-Type", IngestContentType)
	request.Header.Set(EnvelopeHeader, encodedHTTPEnvelope(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		response.Header().Get("Retry-After") != "60" ||
		response.Body.String() != `{"status":"write_frozen"}`+"\n" {
		t.Fatalf("frozen response=%d retry=%q body=%q", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func TestHTTPHandlerRejectsMalformedOversizedAndUnavailableRequests(t *testing.T) {
	handler, err := NewHTTPHandler(&httpAcceptorFixture{})
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	tests := []struct {
		name        string
		method      string
		contentType string
		envelope    string
		body        []byte
		status      int
	}{
		{
			name:   "method",
			method: http.MethodGet,
			status: http.StatusMethodNotAllowed,
		},
		{
			name:        "media type",
			method:      http.MethodPost,
			contentType: "application/json",
			status:      http.StatusUnsupportedMediaType,
		},
		{
			name:        "envelope",
			method:      http.MethodPost,
			contentType: IngestContentType,
			envelope:    "not-base64",
			status:      http.StatusUnauthorized,
		},
		{
			name:        "oversized",
			method:      http.MethodPost,
			contentType: IngestContentType,
			envelope:    encodedHTTPEnvelope(t),
			body:        make([]byte, telemetry.MaxBatchCompressedBytes+1),
			status:      http.StatusRequestEntityTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				test.method,
				IngestPath,
				bytes.NewReader(test.body),
			)
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set(EnvelopeHeader, test.envelope)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if bytes.Contains(response.Body.Bytes(), test.body) && len(test.body) > 0 {
				t.Fatal("rejection reflected request body")
			}
		})
	}

	unavailable, err := NewHTTPHandler(&httpAcceptorFixture{
		err: errors.Join(ErrUnavailable, errors.New("database detail")),
	})
	if err != nil {
		t.Fatalf("NewHTTPHandler(unavailable) error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		IngestPath,
		bytes.NewReader([]byte("body")),
	)
	request.Header.Set("Content-Type", IngestContentType)
	request.Header.Set(EnvelopeHeader, encodedHTTPEnvelope(t))
	response := httptest.NewRecorder()
	unavailable.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		response.Body.String() != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("unavailable response = %d %q", response.Code, response.Body.String())
	}
}

func TestHTTPTransportRequiresHTTPSExceptLoopback(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	if _, err := NewHTTPTransport(client, "http://api.example"); err == nil {
		t.Fatal("NewHTTPTransport(http remote) succeeded")
	}
	if _, err := NewHTTPTransport(client, "https://api.example/path"); err == nil {
		t.Fatal("NewHTTPTransport(path) succeeded")
	}
	if _, err := NewHTTPTransport(client, "http://127.0.0.1:8080"); err != nil {
		t.Fatalf("NewHTTPTransport(loopback) error = %v", err)
	}
}

type httpAcceptorFixture struct {
	acknowledgement telemetry.Acknowledgement
	err             error
	envelope        signing.SignedEnvelope
	body            []byte
}

func (fixture *httpAcceptorFixture) Accept(
	_ context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (telemetry.Acknowledgement, error) {
	fixture.envelope = envelope
	fixture.body = append([]byte(nil), body...)
	return fixture.acknowledgement, fixture.err
}

func encodedHTTPEnvelope(t *testing.T) string {
	t.Helper()
	envelope := signing.SignedEnvelope{
		Envelope: signing.Envelope{
			Schema:     signing.EnvelopeSchema,
			Version:    signing.EnvelopeVersion,
			NodeID:     cloudNodeID,
			KeyID:      cloudKeyID,
			RequestID:  cloudRequestID,
			Timestamp:  "2026-07-25T14:00:00Z",
			BodySHA256: "digest",
		},
		Signature: "signature",
	}
	encoded, err := encodeEnvelopeHeader(envelope)
	if err != nil {
		t.Fatalf("encodeEnvelopeHeader() error = %v", err)
	}
	return encoded
}
