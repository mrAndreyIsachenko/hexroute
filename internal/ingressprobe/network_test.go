package ingressprobe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
)

const (
	probeNodeID    = metadata.UUID("11111111-1111-4111-8111-111111111111")
	probeRequestID = metadata.UUID("22222222-2222-4222-8222-222222222222")
)

func TestTCPAndTLSFallbackRemainIndependent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go acceptAndClose(listener)

	runner := DefaultRunner()
	endpoint := endpointFromAddress(t, listener.Addr().String())
	tcpResult := runner.Probe(
		context.Background(),
		KindTCP,
		mustJSON(t, TCPRequest{Endpoint: endpoint, TimeoutMS: 500}),
	)
	if tcpResult.State != StatePass || tcpResult.Category != CategoryOK {
		t.Fatalf("TCP result = %+v", tcpResult)
	}
	tlsResult := runner.Probe(
		context.Background(),
		KindTLSFallback,
		mustJSON(t, TLSFallbackRequest{
			Endpoint: endpoint, ServerName: "fallback.example", TimeoutMS: 500,
		}),
	)
	if tlsResult.State != StateFail || tlsResult.Category != CategoryTLS {
		t.Fatalf("TLS result = %+v", tlsResult)
	}
}

func TestTCPTimeoutIsBoundedAndCategorized(t *testing.T) {
	runner := DefaultRunner()
	runner.dialContext = func(
		ctx context.Context,
		_ string,
		_ string,
	) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result := runner.Probe(
		context.Background(),
		KindTCP,
		mustJSON(t, TCPRequest{
			Endpoint:  Endpoint{Host: "timeout.invalid", Port: 443},
			TimeoutMS: 100,
		}),
	)
	if result.Category != CategoryTimeout || result.DurationMS > 500 {
		t.Fatalf("result = %+v", result)
	}
}

func TestTLSFallbackValidatesCertificateAndServerName(t *testing.T) {
	listener, roots := startTLSServer(t, "fallback.example")
	runner := DefaultRunner()
	runner.tlsConfig = func(serverName string) *tls.Config {
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: serverName,
			RootCAs:    roots,
		}
	}
	request := TLSFallbackRequest{
		Endpoint:   endpointFromAddress(t, listener.Addr().String()),
		ServerName: "fallback.example",
		TimeoutMS:  500,
	}
	result := runner.Probe(context.Background(), KindTLSFallback, mustJSON(t, request))
	if result.State != StatePass {
		t.Fatalf("valid TLS result = %+v", result)
	}
	request.ServerName = "wrong.example"
	result = runner.Probe(context.Background(), KindTLSFallback, mustJSON(t, request))
	if result.Category != CategoryTLS {
		t.Fatalf("wrong-name TLS result = %+v", result)
	}
}

func TestSignedHeartbeatCategories(t *testing.T) {
	now := time.Date(2026, time.August, 1, 1, 0, 0, 0, time.UTC)
	key := heartbeatTestKey(t)
	generation := "provider-b-generation-1"
	tests := []struct {
		name             string
		observedAt       time.Time
		signedAt         time.Time
		generation       string
		transportHealthy bool
		tamperSignature  bool
		expectedCategory Category
	}{
		{
			name: "healthy", observedAt: now, signedAt: now,
			generation: generation, transportHealthy: true,
			expectedCategory: CategoryOK,
		},
		{
			name: "invalid signature", observedAt: now, signedAt: now,
			generation: generation, transportHealthy: true, tamperSignature: true,
			expectedCategory: CategoryHeartbeatAuthenticity,
		},
		{
			name: "stale", observedAt: now.Add(-2 * time.Minute),
			signedAt: now.Add(-2 * time.Minute), generation: generation,
			transportHealthy: true, expectedCategory: CategoryHeartbeatFreshness,
		},
		{
			name: "generation mismatch", observedAt: now, signedAt: now,
			generation: "provider-b-generation-2", transportHealthy: true,
			expectedCategory: CategoryHeartbeatGeneration,
		},
		{
			name: "runtime unhealthy", observedAt: now, signedAt: now,
			generation: generation, transportHealthy: false,
			expectedCategory: CategoryHeartbeatUnhealthy,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodedBody := mustJSON(t, Heartbeat{
				Schema: HeartbeatSchema, Version: HeartbeatVersion,
				NodeID: probeNodeID, Generation: test.generation,
				ObservedAt:       test.observedAt.Format(time.RFC3339Nano),
				TransportHealthy: test.transportHealthy,
			})
			signed, err := signing.Sign(key, probeRequestID, test.signedAt, encodedBody)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			if test.tamperSignature {
				signed.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(response).Encode(SignedHeartbeat{
					Schema: HeartbeatResponseSchema, Body: encodedBody, Signed: signed,
				})
			}))
			defer server.Close()

			runner := DefaultRunner()
			runner.now = func() time.Time { return now }
			runner.httpClient = trustedTestClientFactory(server)
			request := HeartbeatRequest{
				EndpointURL:        server.URL,
				ExpectedNodeID:     string(key.NodeID),
				ExpectedKeyID:      string(key.KeyID),
				PublicKey:          base64.RawURLEncoding.EncodeToString(key.PublicKey()),
				ExpectedGeneration: generation,
				MaxAgeMS:           60_000,
				TimeoutMS:          1_000,
			}
			result := runner.Probe(context.Background(), KindHeartbeat, mustJSON(t, request))
			if result.Category != test.expectedCategory {
				t.Fatalf("result = %+v, want category %q", result, test.expectedCategory)
			}
		})
	}
}

func TestHeartbeatRejectsUnknownResponseFieldsAndOversize(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"schema":"hexroute.ingress-heartbeat-response.v1","unknown":true}`))
	}))
	defer server.Close()
	runner := DefaultRunner()
	runner.httpClient = trustedTestClientFactory(server)
	request := HeartbeatRequest{
		EndpointURL:        server.URL,
		ExpectedNodeID:     "11111111-1111-4111-8111-111111111111",
		ExpectedKeyID:      "22222222-2222-4222-8222-222222222222",
		PublicKey:          base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		ExpectedGeneration: "generation-1", MaxAgeMS: 1000, TimeoutMS: 1000,
	}
	result := runner.Probe(context.Background(), KindHeartbeat, mustJSON(t, request))
	if result.Category != CategoryHeartbeatResponse {
		t.Fatalf("result = %+v", result)
	}
}

func heartbeatTestKey(t *testing.T) signing.Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node-key.json")
	key, err := signing.GenerateFile(path, probeNodeID, rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	return key
}

func trustedTestClientFactory(server *httptest.Server) func(time.Duration) *http.Client {
	return func(timeout time.Duration) *http.Client {
		client := server.Client()
		client.Timeout = timeout
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return client
	}
}

func startTLSServer(t *testing.T, serverName string) (net.Listener, *x509.CertPool) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate TLS key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create TLS certificate: %v", err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate},
	})
	if err != nil {
		t.Fatalf("TLS listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go acceptTLSAndClose(listener)
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse TLS certificate: %v", err)
	}
	roots.AddCert(parsed)
	return listener, roots
}

func acceptAndClose(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		_ = connection.Close()
	}
}

func acceptTLSAndClose(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		if tlsConnection, ok := connection.(*tls.Conn); ok {
			_ = tlsConnection.Handshake()
		}
		_ = connection.Close()
	}
}

func endpointFromAddress(t *testing.T, address string) Endpoint {
	t.Helper()
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return Endpoint{Host: host, Port: uint16(port)}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return encoded
}
