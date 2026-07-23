package observe

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

type fakeConnector struct {
	err      error
	endpoint Endpoint
	calls    int
}

func (connector *fakeConnector) Connect(_ context.Context, endpoint Endpoint) error {
	connector.calls++
	connector.endpoint = endpoint
	return connector.err
}

func TestReadinessObserverReportsOnlyBoundedResult(t *testing.T) {
	connector := &fakeConnector{}
	observer, err := NewReadinessObserver(connector)
	if err != nil {
		t.Fatalf("NewReadinessObserver() error: %v", err)
	}
	times := []time.Time{
		time.Unix(100, 0),
		time.Unix(100, int64(25*time.Millisecond)),
	}
	observer.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}
	endpoint := Endpoint{
		Name:       "outer-ready",
		Address:    netip.MustParseAddrPort("192.0.2.20:443"),
		ServerName: "probe.example.invalid",
		Timeout:    4 * time.Second,
	}

	observation, err := observer.Endpoint(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Endpoint() error: %v", err)
	}
	if !observation.Ready || observation.Latency != 25*time.Millisecond ||
		observation.Name != endpoint.Name || connector.calls != 1 {
		t.Fatalf("Endpoint() = %+v, connector calls=%d", observation, connector.calls)
	}
}

func TestReadinessFailureIsAnObservationNotRawError(t *testing.T) {
	connector := &fakeConnector{err: errors.New("private diagnostic")}
	observer, _ := NewReadinessObserver(connector)
	observer.now = func() time.Time { return time.Unix(100, 0) }
	endpoint := Endpoint{
		Name:       "outer-ready",
		Address:    netip.MustParseAddrPort("198.51.100.20:443"),
		ServerName: "probe.example.invalid",
		Timeout:    time.Second,
	}

	observation, err := observer.Endpoint(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Endpoint() error: %v", err)
	}
	if observation.Ready {
		t.Fatalf("Endpoint() = %+v, want not ready", observation)
	}
}

func TestEndpointValidatesTransportSpecificProxy(t *testing.T) {
	base := Endpoint{
		Name:        "outer-ready",
		Transport:   TransportDirectTLS,
		Certificate: CertificateVerify,
		Address:     netip.MustParseAddrPort("198.51.100.20:443"),
		ServerName:  "probe.example.invalid",
		Timeout:     time.Second,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("direct endpoint Validate() error: %v", err)
	}

	directWithProxy := base
	directWithProxy.ProxyAddress = netip.MustParseAddrPort("127.0.0.1:2080")
	if err := directWithProxy.Validate(); err == nil {
		t.Fatal("direct endpoint accepted proxy address")
	}

	socksWithoutProxy := base
	socksWithoutProxy.Transport = TransportSOCKS5TLS
	if err := socksWithoutProxy.Validate(); err == nil {
		t.Fatal("SOCKS endpoint accepted missing proxy address")
	}

	socks := socksWithoutProxy
	socks.ProxyAddress = netip.MustParseAddrPort("127.0.0.1:2080")
	if err := socks.Validate(); err != nil {
		t.Fatalf("SOCKS endpoint Validate() error: %v", err)
	}
}

func TestEndpointRejectsUnknownCertificatePolicy(t *testing.T) {
	endpoint := Endpoint{
		Name:        "outer-ready",
		Transport:   TransportDirectTLS,
		Certificate: CertificatePolicy("disabled"),
		Address:     netip.MustParseAddrPort("198.51.100.20:443"),
		ServerName:  "probe.example.invalid",
		Timeout:     time.Second,
	}
	if err := endpoint.Validate(); err == nil {
		t.Fatal("Endpoint.Validate() accepted unknown certificate policy")
	}
}
