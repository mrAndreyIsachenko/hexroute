package observe

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"time"

	"golang.org/x/net/proxy"
)

var endpointNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

type Transport string

const (
	TransportDirectTLS Transport = "direct_tls"
	TransportSOCKS5TLS Transport = "socks5_tls"
)

type CertificatePolicy string

const (
	CertificateVerify        CertificatePolicy = "verify"
	CertificateHandshakeOnly CertificatePolicy = "handshake_only"
)

type Endpoint struct {
	Name         string
	Transport    Transport
	Certificate  CertificatePolicy
	Address      netip.AddrPort
	ProxyAddress netip.AddrPort
	ServerName   string
	Timeout      time.Duration
}

func (endpoint Endpoint) Validate() error {
	if !endpointNamePattern.MatchString(endpoint.Name) ||
		!endpoint.Address.IsValid() ||
		endpoint.ServerName == "" ||
		len(endpoint.ServerName) > 253 ||
		endpoint.Timeout <= 0 ||
		endpoint.Timeout > 30*time.Second {
		return errors.New("invalid readiness endpoint")
	}
	if endpoint.Certificate != "" &&
		endpoint.Certificate != CertificateVerify &&
		endpoint.Certificate != CertificateHandshakeOnly {
		return errors.New("invalid certificate policy")
	}
	switch endpoint.Transport {
	case "", TransportDirectTLS:
		if endpoint.ProxyAddress.IsValid() {
			return errors.New("direct endpoint cannot define a proxy")
		}
		return nil
	case TransportSOCKS5TLS:
		if !endpoint.ProxyAddress.IsValid() {
			return errors.New("SOCKS endpoint requires a proxy")
		}
		return nil
	default:
		return errors.New("invalid readiness transport")
	}
}

type EndpointConnector interface {
	Connect(context.Context, Endpoint) error
}

type DefaultConnector struct {
	Direct TLSConnector
	SOCKS  SOCKS5TLSConnector
}

func (connector DefaultConnector) Connect(ctx context.Context, endpoint Endpoint) error {
	switch endpoint.Transport {
	case "", TransportDirectTLS:
		return connector.Direct.Connect(ctx, endpoint)
	case TransportSOCKS5TLS:
		return connector.SOCKS.Connect(ctx, endpoint)
	default:
		return errors.New("invalid readiness transport")
	}
}

type TLSConnector struct {
	Dialer *net.Dialer
}

func (connector TLSConnector) Connect(ctx context.Context, endpoint Endpoint) error {
	if err := endpoint.Validate(); err != nil {
		return err
	}
	dialer := connector.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	tlsDialer := tls.Dialer{
		NetDialer: dialer,
		Config:    endpoint.tlsConfig(),
	}
	connection, err := tlsDialer.DialContext(ctx, "tcp", endpoint.Address.String())
	if err != nil {
		return err
	}
	return connection.Close()
}

type SOCKS5TLSConnector struct {
	Forward *net.Dialer
}

func (connector SOCKS5TLSConnector) Connect(ctx context.Context, endpoint Endpoint) error {
	if err := endpoint.Validate(); err != nil {
		return err
	}
	if endpoint.Transport != TransportSOCKS5TLS {
		return errors.New("SOCKS connector requires SOCKS transport")
	}
	forward := connector.Forward
	if forward == nil {
		forward = &net.Dialer{}
	}
	dialer, err := proxy.SOCKS5("tcp", endpoint.ProxyAddress.String(), nil, forward)
	if err != nil {
		return err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return errors.New("SOCKS dialer lacks context support")
	}
	connection, err := contextDialer.DialContext(ctx, "tcp", endpoint.Address.String())
	if err != nil {
		return err
	}

	tlsConnection := tls.Client(connection, endpoint.tlsConfig())
	if err := tlsConnection.HandshakeContext(ctx); err != nil {
		_ = connection.Close()
		return err
	}
	return tlsConnection.Close()
}

func (endpoint Endpoint) tlsConfig() *tls.Config {
	// Handshake-only is restricted to explicitly configured legacy probes that
	// already use an IP endpoint without certificate validation.
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         endpoint.ServerName,
		InsecureSkipVerify: endpoint.Certificate == CertificateHandshakeOnly, // #nosec G402
	}
}

type ReadinessObservation struct {
	Name    string
	Ready   bool
	Latency time.Duration
}

type ReadinessObserver struct {
	connector EndpointConnector
	now       func() time.Time
}

func NewReadinessObserver(connector EndpointConnector) (*ReadinessObserver, error) {
	if connector == nil {
		return nil, errors.New("connector is required")
	}
	return &ReadinessObserver{
		connector: connector,
		now:       time.Now,
	}, nil
}

func (observer *ReadinessObserver) Endpoint(
	ctx context.Context,
	endpoint Endpoint,
) (ReadinessObservation, error) {
	if err := endpoint.Validate(); err != nil {
		return ReadinessObservation{}, err
	}
	probeContext, cancel := context.WithTimeout(ctx, endpoint.Timeout)
	defer cancel()

	started := observer.now()
	err := observer.connector.Connect(probeContext, endpoint)
	observation := ReadinessObservation{
		Name:    endpoint.Name,
		Ready:   err == nil,
		Latency: observer.now().Sub(started),
	}
	if observation.Latency < 0 {
		return ReadinessObservation{}, errors.New("non-monotonic readiness clock")
	}
	return observation, nil
}
