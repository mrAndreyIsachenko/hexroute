package cloudingest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/cutoverfreeze"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

const (
	IngestPath        = "/v1/ingest/batches"
	IngestContentType = "application/vnd.hexroute.ingest-batch+gzip"
	EnvelopeHeader    = "X-Hexroute-Signed-Envelope"

	maxEnvelopeHeaderBytes = 4096
)

type Acceptor interface {
	Accept(
		context.Context,
		signing.SignedEnvelope,
		[]byte,
	) (telemetry.Acknowledgement, error)
}

type HTTPHandler struct {
	acceptor Acceptor
}

type HTTPTransport struct {
	client   *http.Client
	endpoint string
}

var (
	ErrInvalidHTTPConfig = errors.New("invalid ingest HTTP configuration")
	ErrHTTPRejected      = errors.New("ingest HTTP request rejected")
	ErrHTTPUnavailable   = errors.New("ingest HTTP endpoint unavailable")
)

func NewHTTPHandler(acceptor Acceptor) (*HTTPHandler, error) {
	if acceptor == nil {
		return nil, ErrInvalidHTTPConfig
	}
	return &HTTPHandler{acceptor: acceptor}, nil
}

func (handler *HTTPHandler) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	setIngestHeaders(response)
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeIngestStatus(response, http.StatusMethodNotAllowed, "rejected")
		return
	}
	if request.URL.Path != IngestPath ||
		request.Header.Get("Content-Type") != IngestContentType {
		writeIngestStatus(response, http.StatusUnsupportedMediaType, "rejected")
		return
	}
	envelope, err := decodeEnvelopeHeader(request.Header.Get(EnvelopeHeader))
	if err != nil {
		writeIngestStatus(response, http.StatusUnauthorized, "rejected")
		return
	}
	if request.ContentLength > telemetry.MaxBatchCompressedBytes {
		writeIngestStatus(response, http.StatusRequestEntityTooLarge, "rejected")
		return
	}
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		telemetry.MaxBatchCompressedBytes,
	)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeIngestStatus(response, http.StatusRequestEntityTooLarge, "rejected")
		return
	}
	acknowledgement, err := handler.acceptor.Accept(
		request.Context(),
		envelope,
		body,
	)
	if err != nil {
		if cutoverfreeze.IsWriteFrozen(err) {
			response.Header().Set("Retry-After", "60")
			writeIngestStatus(response, http.StatusServiceUnavailable, "write_frozen")
			return
		}
		if errors.Is(err, ErrUnavailable) {
			writeIngestStatus(response, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeIngestStatus(response, http.StatusUnauthorized, "rejected")
		return
	}
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(acknowledgement)
}

func NewHTTPTransport(
	client *http.Client,
	baseURL string,
) (*HTTPTransport, error) {
	if client == nil || client.Timeout <= 0 {
		return nil, ErrInvalidHTTPConfig
	}
	parsed, err := url.Parse(baseURL)
	if err != nil ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") ||
		!validTransportScheme(parsed) {
		return nil, ErrInvalidHTTPConfig
	}
	boundedClient := *client
	boundedClient.CheckRedirect = func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}
	return &HTTPTransport{
		client:   &boundedClient,
		endpoint: strings.TrimSuffix(parsed.String(), "/") + IngestPath,
	}, nil
}

func (transport *HTTPTransport) Upload(
	ctx context.Context,
	envelope signing.SignedEnvelope,
	body []byte,
) (telemetry.Acknowledgement, error) {
	if transport == nil ||
		transport.client == nil ||
		ctx == nil ||
		len(body) == 0 ||
		len(body) > telemetry.MaxBatchCompressedBytes {
		return telemetry.Acknowledgement{}, ErrInvalidHTTPConfig
	}
	encodedEnvelope, err := encodeEnvelopeHeader(envelope)
	if err != nil {
		return telemetry.Acknowledgement{}, ErrInvalidHTTPConfig
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		transport.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return telemetry.Acknowledgement{}, ErrInvalidHTTPConfig
	}
	request.Header.Set("Content-Type", IngestContentType)
	request.Header.Set(EnvelopeHeader, encodedEnvelope)
	response, err := transport.client.Do(request)
	if err != nil {
		return telemetry.Acknowledgement{}, ErrHTTPUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(
			io.Discard,
			io.LimitReader(response.Body, telemetry.MaxAcknowledgementBytes),
		)
		if response.StatusCode >= http.StatusInternalServerError {
			return telemetry.Acknowledgement{}, ErrHTTPUnavailable
		}
		return telemetry.Acknowledgement{}, ErrHTTPRejected
	}
	encoded, err := io.ReadAll(
		io.LimitReader(response.Body, telemetry.MaxAcknowledgementBytes+1),
	)
	if err != nil || len(encoded) > telemetry.MaxAcknowledgementBytes {
		return telemetry.Acknowledgement{}, ErrHTTPUnavailable
	}
	acknowledgement, err := telemetry.DecodeAcknowledgement(encoded)
	if err != nil {
		return telemetry.Acknowledgement{}, ErrHTTPUnavailable
	}
	return acknowledgement, nil
}

func encodeEnvelopeHeader(envelope signing.SignedEnvelope) (string, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) == 0 || len(encoded) > maxEnvelopeHeaderBytes {
		return "", ErrInvalidHTTPConfig
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeEnvelopeHeader(value string) (signing.SignedEnvelope, error) {
	if value == "" || len(value) > maxEnvelopeHeaderBytes*2 {
		return signing.SignedEnvelope{}, ErrInvalidHTTPConfig
	}
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(encoded) == 0 || len(encoded) > maxEnvelopeHeaderBytes {
		return signing.SignedEnvelope{}, ErrInvalidHTTPConfig
	}
	var envelope signing.SignedEnvelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return signing.SignedEnvelope{}, ErrInvalidHTTPConfig
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return signing.SignedEnvelope{}, ErrInvalidHTTPConfig
	}
	return envelope, nil
}

func validTransportScheme(parsed *url.URL) bool {
	if parsed == nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func setIngestHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeIngestStatus(
	response http.ResponseWriter,
	status int,
	value string,
) {
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"status": value})
}
