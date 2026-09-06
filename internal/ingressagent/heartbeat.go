package ingressagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressprobe"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

const (
	maxHeartbeatBytes = 64 * 1024
	heartbeatTimeout  = 5 * time.Second
	// heartbeatTolerance is how stale a heartbeat may be and still describe
	// now. The observer answers a request by measuring, so anything older
	// than this is a reply to a question nobody asked.
	heartbeatTolerance = 2 * time.Minute
)

// LoopbackObserver reads this host's own signed heartbeat.
//
// It verifies the signature against the node's published public identity, and
// holds no key that could produce one. An unverified read would make proof
// depend on whatever answered the port.
type LoopbackObserver struct {
	endpoint   string
	registered signing.RegisteredKey
	client     *http.Client
	now        func() time.Time
}

// NewLoopbackObserver builds an observer for one loopback endpoint.
func NewLoopbackObserver(endpoint string, identityFile string) (*LoopbackObserver, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" ||
		!isLoopbackHost(parsed.Hostname()) {
		// The heartbeat is served on loopback and read on loopback. Reaching
		// for it anywhere else would be reading somebody else's host and
		// calling it this one's health.
		return nil, fmt.Errorf("%w: heartbeat endpoint must be loopback http", ErrAgent)
	}
	registered, err := signing.LoadPublicIdentityFile(identityFile)
	if err != nil {
		return nil, fmt.Errorf("%w: node identity", ErrAgent)
	}
	return &LoopbackObserver{
		endpoint:   endpoint,
		registered: registered,
		client:     &http.Client{Timeout: heartbeatTimeout},
		now:        time.Now,
	}, nil
}

// Observe reads and verifies one heartbeat.
func (observer *LoopbackObserver) Observe(ctx context.Context) (Observation, error) {
	if observer == nil || ctx == nil {
		return Observation{}, fmt.Errorf("%w: no observer", ErrAgent)
	}
	requestCtx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestCtx, http.MethodGet, observer.endpoint, nil)
	if err != nil {
		return Observation{}, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := observer.client.Do(request)
	if err != nil {
		return Observation{}, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	defer response.Body.Close()
	mediaType, _, mediaTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaTypeErr != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxHeartbeatBytes))
		return Observation{}, fmt.Errorf("%w: heartbeat status", ErrAgent)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxHeartbeatBytes+1))
	if err != nil || len(encoded) == 0 || len(encoded) > maxHeartbeatBytes {
		return Observation{}, fmt.Errorf("%w: heartbeat body", ErrAgent)
	}

	var signedHeartbeat ingressprobe.SignedHeartbeat
	if err := decodeStrict(encoded, &signedHeartbeat); err != nil ||
		signedHeartbeat.Schema != ingressprobe.HeartbeatResponseSchema ||
		len(signedHeartbeat.Body) == 0 {
		return Observation{}, fmt.Errorf("%w: heartbeat response", ErrAgent)
	}
	var heartbeat ingressprobe.Heartbeat
	if err := decodeStrict(signedHeartbeat.Body, &heartbeat); err != nil ||
		heartbeat.Schema != ingressprobe.HeartbeatSchema ||
		heartbeat.Version != ingressprobe.HeartbeatVersion {
		return Observation{}, fmt.Errorf("%w: heartbeat body", ErrAgent)
	}
	if err := signing.VerifyAuthenticity(
		signedHeartbeat.Signed,
		signedHeartbeat.Body,
		observer.now().UTC(),
		heartbeatTolerance,
		observer.registered,
	); err != nil {
		return Observation{}, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if heartbeat.NodeID != observer.registered.NodeID {
		return Observation{}, fmt.Errorf("%w: heartbeat node", ErrAgent)
	}
	return Observation{
		Generation: heartbeat.Generation,
		Healthy:    heartbeat.TransportHealthy,
	}, nil
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func decodeStrict(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing content")
	}
	return nil
}
