package ingressprobe

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

const maxHeartbeatResponseBytes = 64 * 1024

type Heartbeat struct {
	Schema           string        `json:"schema"`
	Version          uint16        `json:"version"`
	NodeID           metadata.UUID `json:"node_id"`
	Generation       string        `json:"generation"`
	ObservedAt       string        `json:"observed_at"`
	TransportHealthy bool          `json:"transport_healthy"`
}

type SignedHeartbeat struct {
	Schema string                 `json:"schema"`
	Body   json.RawMessage        `json:"body"`
	Signed signing.SignedEnvelope `json:"signed"`
}

func (runner *Runner) probeHeartbeat(parent context.Context, raw []byte) Category {
	var request HeartbeatRequest
	if decodeStrict(raw, &request) != nil || runner.httpClient == nil {
		return CategoryInvalidInput
	}
	timeout, timeoutOK := timeoutDuration(request.TimeoutMS)
	maxAge, maxAgeOK := heartbeatAgeDuration(request.MaxAgeMS)
	nodeID, nodeErr := metadata.ParseUUID(request.ExpectedNodeID)
	keyID, keyErr := metadata.ParseUUID(request.ExpectedKeyID)
	publicKey, publicKeyErr := base64.RawURLEncoding.DecodeString(request.PublicKey)
	if !timeoutOK || !maxAgeOK || nodeErr != nil || keyErr != nil ||
		publicKeyErr != nil || len(publicKey) != ed25519.PublicKeySize ||
		!validReference(request.ExpectedGeneration) ||
		!validHTTPSURL(request.EndpointURL) {
		return CategoryInvalidInput
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		request.EndpointURL,
		nil,
	)
	if err != nil {
		return CategoryInvalidInput
	}
	httpRequest.Header.Set("Accept", "application/json")
	client := runner.httpClient(timeout)
	if client == nil {
		return CategoryInternal
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return networkFailure(ctx, err, CategoryHeartbeatResponse)
	}
	defer response.Body.Close()
	mediaType, _, mediaTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaTypeErr != nil || mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxHeartbeatResponseBytes))
		return CategoryHeartbeatResponse
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxHeartbeatResponseBytes+1))
	if err != nil || len(encoded) > maxHeartbeatResponseBytes {
		return CategoryHeartbeatResponse
	}

	var signedHeartbeat SignedHeartbeat
	if decodeHeartbeatJSON(encoded, &signedHeartbeat) != nil ||
		signedHeartbeat.Schema != HeartbeatResponseSchema ||
		len(signedHeartbeat.Body) == 0 {
		return CategoryHeartbeatResponse
	}
	var heartbeat Heartbeat
	if decodeHeartbeatJSON(signedHeartbeat.Body, &heartbeat) != nil ||
		heartbeat.Schema != HeartbeatSchema || heartbeat.Version != HeartbeatVersion {
		return CategoryHeartbeatResponse
	}
	now := runner.now().UTC()
	registered := signing.RegisteredKey{
		NodeID:    nodeID,
		KeyID:     keyID,
		PublicKey: append(ed25519.PublicKey(nil), publicKey...),
		Status:    signing.KeyActive,
	}
	if err := signing.VerifyAuthenticity(
		signedHeartbeat.Signed,
		signedHeartbeat.Body,
		now,
		maxAge,
		registered,
	); err != nil {
		if errors.Is(err, signing.ErrTimestamp) {
			return CategoryHeartbeatFreshness
		}
		return CategoryHeartbeatAuthenticity
	}
	observedAt, err := time.Parse(time.RFC3339Nano, heartbeat.ObservedAt)
	if err != nil || observedAt.UTC().Format(time.RFC3339Nano) != heartbeat.ObservedAt ||
		observedAt.Before(now.Add(-maxAge)) || observedAt.After(now.Add(maxAge)) {
		return CategoryHeartbeatFreshness
	}
	if heartbeat.NodeID != nodeID ||
		signedHeartbeat.Signed.Envelope.NodeID != heartbeat.NodeID {
		return CategoryHeartbeatAuthenticity
	}
	if heartbeat.Generation != request.ExpectedGeneration {
		return CategoryHeartbeatGeneration
	}
	if !heartbeat.TransportHealthy {
		return CategoryHeartbeatUnhealthy
	}
	return CategoryOK
}

func decodeHeartbeatJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid heartbeat response")
	}
	return nil
}
