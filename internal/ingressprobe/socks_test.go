package ingressprobe

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

func TestHeartbeatUsesLiteralLoopbackSOCKS(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	key, err := signing.GenerateFile(filepath.Join(t.TempDir(), "key.json"), probeNodeID, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateFile() error = %v", err)
	}
	body := mustJSON(t, Heartbeat{
		Schema: HeartbeatSchema, Version: HeartbeatVersion, NodeID: probeNodeID,
		Generation: "provider-b-generation-1", ObservedAt: now.Format(time.RFC3339Nano),
		TransportHealthy: true,
	})
	signed, err := signing.Sign(key, probeRequestID, now, body)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(SignedHeartbeat{
			Schema: HeartbeatResponseSchema, Body: body, Signed: signed,
		})
	}))
	defer server.Close()

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS: %v", err)
	}
	defer proxyListener.Close()
	go serveTestSOCKS(proxyListener)

	runner := DefaultRunner()
	runner.now = func() time.Time { return now }
	request := HeartbeatRequest{
		EndpointURL:        server.URL,
		ProxyURL:           "socks5://" + proxyListener.Addr().String(),
		ExpectedNodeID:     string(key.NodeID),
		ExpectedKeyID:      string(key.KeyID),
		PublicKey:          base64.RawURLEncoding.EncodeToString(key.PublicKey()),
		ExpectedGeneration: "provider-b-generation-1",
		MaxAgeMS:           60_000,
		TimeoutMS:          1_000,
	}
	result := runner.Probe(context.Background(), KindHeartbeat, mustJSON(t, request))
	if result.State != StatePass || result.Category != CategoryOK {
		t.Fatalf("result = %+v", result)
	}
}

func TestHeartbeatTransportRejectsUnsafePlaintextAndProxyEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		proxy    string
		valid    bool
	}{
		{name: "remote HTTPS direct", endpoint: "https://status.example/v1/heartbeat", valid: true},
		{name: "loopback HTTP through loopback SOCKS", endpoint: "http://127.0.0.1:9080/v1/heartbeat", proxy: "socks5://127.0.0.1:2080", valid: true},
		{name: "plaintext direct", endpoint: "http://127.0.0.1:9080/v1/heartbeat"},
		{name: "hostname target", endpoint: "http://localhost:9080/v1/heartbeat", proxy: "socks5://127.0.0.1:2080"},
		{name: "hostname proxy", endpoint: "http://127.0.0.1:9080/v1/heartbeat", proxy: "socks5://localhost:2080"},
		{name: "public proxy", endpoint: "https://status.example/v1/heartbeat", proxy: "socks5://198.51.100.2:2080"},
		{name: "credential proxy", endpoint: "http://127.0.0.1:9080/v1/heartbeat", proxy: "socks5://user:pass@127.0.0.1:2080"},
		{name: "invalid proxy port", endpoint: "http://127.0.0.1:9080/v1/heartbeat", proxy: "socks5://127.0.0.1:99999"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validHeartbeatTransport(test.endpoint, test.proxy); got != test.valid {
				t.Fatalf("validHeartbeatTransport() = %t, want %t", got, test.valid)
			}
		})
	}
}

func serveTestSOCKS(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go handleTestSOCKS(connection)
	}
}

func handleTestSOCKS(client net.Conn) {
	defer client.Close()
	greeting := make([]byte, 3)
	if _, err := io.ReadFull(client, greeting); err != nil ||
		greeting[0] != 5 || greeting[1] != 1 || greeting[2] != 0 {
		return
	}
	if _, err := client.Write([]byte{5, 0}); err != nil {
		return
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil ||
		header[0] != 5 || header[1] != 1 || header[2] != 0 {
		return
	}
	var host string
	switch header[3] {
	case 1:
		address := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(client, address); err != nil {
			return
		}
		host = net.IP(address).String()
	case 4:
		address := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(client, address); err != nil {
			return
		}
		host = net.IP(address).String()
	case 3:
		length := []byte{0}
		if _, err := io.ReadFull(client, length); err != nil || length[0] == 0 {
			return
		}
		address := make([]byte, int(length[0]))
		if _, err := io.ReadFull(client, address); err != nil {
			return
		}
		host = string(address)
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, formatPort(binary.BigEndian.Uint16(portBytes))))
	if err != nil {
		_, _ = client.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	if _, err := client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(target, client)
		done <- struct{}{}
	}()
	_, _ = io.Copy(client, target)
	<-done
}
