package ingressobserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressprobe"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

func TestHeartbeatIsSignedAndReportsBoundedRuntimeHealth(t *testing.T) {
	nodeID := metadata.UUID("11111111-1111-4111-8111-111111111111")
	keyPath := filepath.Join(t.TempDir(), "observer-key.json")
	key, err := signing.GenerateFile(keyPath, nodeID, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	service, err := NewService(Config{
		ListenAddr:       "127.0.0.1:9080",
		XRayEndpoint:     "127.0.0.1:443",
		OutboundEndpoint: "1.1.1.1:443",
		NodeID:           nodeID,
		Generation:       "provider-b-generation-1",
		KeyFile:          keyPath,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.dial = func(_ context.Context, _, endpoint string) (net.Conn, error) {
		if endpoint == "1.1.1.1:443" || endpoint == "127.0.0.1:443" {
			client, server := net.Pipe()
			go server.Close()
			return client, nil
		}
		return nil, errors.New("sensitive dependency failure")
	}

	request := httptest.NewRequest(http.MethodGet, HeartbeatPath, nil)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response status=%d headers=%v", response.Code, response.Header())
	}
	var signed ingressprobe.SignedHeartbeat
	if err := json.Unmarshal(response.Body.Bytes(), &signed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var heartbeat ingressprobe.Heartbeat
	if err := json.Unmarshal(signed.Body, &heartbeat); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if !heartbeat.TransportHealthy || heartbeat.NodeID != nodeID ||
		heartbeat.Generation != "provider-b-generation-1" {
		t.Fatalf("heartbeat = %+v", heartbeat)
	}
	if err := signing.VerifyAuthenticity(
		signed.Signed,
		signed.Body,
		now,
		time.Minute,
		signing.RegisteredKey{
			NodeID: nodeID, KeyID: key.KeyID, PublicKey: key.PublicKey(), Status: signing.KeyActive,
		},
	); err != nil {
		t.Fatalf("VerifyAuthenticity() error = %v", err)
	}
	if strings.Contains(response.Body.String(), "1.1.1.1") ||
		strings.Contains(response.Body.String(), "127.0.0.1") ||
		strings.Contains(response.Body.String(), "sensitive") {
		t.Fatal("response exposed runtime configuration or dependency error")
	}
}

func TestHeartbeatSignsUnhealthyStateWithoutRawError(t *testing.T) {
	nodeID := metadata.UUID("11111111-1111-4111-8111-111111111111")
	keyPath := filepath.Join(t.TempDir(), "observer-key.json")
	if _, err := signing.GenerateFile(keyPath, nodeID, rand.Reader); err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	service, err := NewService(Config{
		ListenAddr: "127.0.0.1:9080", XRayEndpoint: "127.0.0.1:443",
		OutboundEndpoint: "1.1.1.1:443", NodeID: nodeID,
		Generation: "provider-b-generation-1", KeyFile: keyPath,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.dial = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("sensitive dependency failure")
	}
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, HeartbeatPath, nil))
	var signed ingressprobe.SignedHeartbeat
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &signed) != nil {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	var heartbeat ingressprobe.Heartbeat
	if json.Unmarshal(signed.Body, &heartbeat) != nil || heartbeat.TransportHealthy {
		t.Fatalf("heartbeat = %+v", heartbeat)
	}
	if strings.Contains(response.Body.String(), "sensitive") {
		t.Fatal("response exposed dependency error")
	}
}
