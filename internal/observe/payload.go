package observe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"golang.org/x/net/proxy"
)

// PayloadObservation is whether traffic traversed the path, not whether the
// path answered.
//
// A completed connection proves that something accepted a socket. It does not
// prove that a request reached a server and a response came back, and those are
// different facts about a tunnel: the one the supervisor acts on, twice in
// sixty-one days, is a tunnel that is up and carries nothing.
type PayloadObservation struct {
	Name      string
	Traversed bool
	Status    int
	Latency   time.Duration
}

// PayloadEndpoint is a path to exercise rather than to reach.
type PayloadEndpoint struct {
	Name string
	// URL is requested in full, because the response status is the evidence.
	URL string
	// ProxyAddress, when set, is a SOCKS5 proxy the request goes through. It is
	// how the path under test is chosen: the same URL through a different proxy
	// is a different path.
	ProxyAddress netip.AddrPort
	// InsecureSkipVerify matches what the production probe does. The evidence
	// is that a response traversed, not that a certificate chained: the tunnel
	// terminates at an endpoint whose certificate this runtime does not pin.
	InsecureSkipVerify bool
	Timeout            time.Duration
}

var (
	ErrInvalidPayloadEndpoint = errors.New("invalid payload endpoint")
	ErrPayloadProbe           = errors.New("payload probe failed")
)

func (endpoint PayloadEndpoint) Validate() error {
	if !endpointNamePattern.MatchString(endpoint.Name) ||
		endpoint.URL == "" ||
		endpoint.Timeout <= 0 || endpoint.Timeout > time.Minute {
		return ErrInvalidPayloadEndpoint
	}
	return nil
}

// PayloadProber exercises a path and reports whether traffic traversed it.
type PayloadProber struct {
	dial func(ctx context.Context, network, address string) (net.Conn, error)
	now  func() time.Time
}

func NewPayloadProber() *PayloadProber {
	return &PayloadProber{now: time.Now}
}

// Payload requests the endpoint and reports whether a response came back.
//
// Traversal is a response status. A connection that is accepted and then closed
// without one is a failure here and a success to anything that only dials,
// which is the whole distinction this exists to make.
func (prober *PayloadProber) Payload(
	ctx context.Context,
	endpoint PayloadEndpoint,
) (PayloadObservation, error) {
	if prober == nil || ctx == nil {
		return PayloadObservation{}, ErrInvalidPayloadEndpoint
	}
	if err := endpoint.Validate(); err != nil {
		return PayloadObservation{}, err
	}

	transport := &http.Transport{
		Proxy:               nil,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: endpoint.Timeout,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: endpoint.InsecureSkipVerify, //nolint:gosec
			MinVersion:         tls.VersionTLS12,
		},
	}
	if prober.dial != nil {
		transport.DialContext = prober.dial
	}
	if endpoint.ProxyAddress.IsValid() {
		dialer, err := socksDialer(endpoint.ProxyAddress, endpoint.Timeout)
		if err != nil {
			return PayloadObservation{Name: endpoint.Name}, err
		}
		transport.DialContext = dialer
	}

	requestCtx, cancel := context.WithTimeout(ctx, endpoint.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestCtx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		return PayloadObservation{Name: endpoint.Name}, ErrInvalidPayloadEndpoint
	}

	started := prober.clock()
	client := &http.Client{Transport: transport, Timeout: endpoint.Timeout}
	response, err := client.Do(request)
	latency := prober.clock().Sub(started)
	if err != nil {
		// No response line came back. Whatever happened at the socket, nothing
		// traversed.
		return PayloadObservation{
			Name: endpoint.Name, Traversed: false, Latency: latency,
		}, nil
	}
	defer response.Body.Close()
	// The body is read and discarded so that a server which sends a status and
	// then abandons the response is not counted as traversal.
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4096)); err != nil {
		return PayloadObservation{
			Name: endpoint.Name, Traversed: false,
			Status: response.StatusCode, Latency: latency,
		}, nil
	}
	// A response came back and its body was readable. That is traversal, and
	// the status is recorded rather than judged: a 502 reached a server and
	// was answered, which is what this probe is asked about. What the path
	// carries is the application's business.
	return PayloadObservation{
		Name:      endpoint.Name,
		Traversed: true,
		Status:    response.StatusCode,
		Latency:   latency,
	}, nil
}

func (prober *PayloadProber) clock() time.Time {
	if prober == nil || prober.now == nil {
		return time.Now()
	}
	return prober.now()
}

// socksDialer sends the connection through a SOCKS5 proxy, which is how the
// path under test is selected: the same URL through a different proxy is a
// different path.
func socksDialer(
	proxyAddress netip.AddrPort,
	timeout time.Duration,
) (func(context.Context, string, string) (net.Conn, error), error) {
	if !proxyAddress.IsValid() {
		return nil, ErrInvalidPayloadEndpoint
	}
	dialer, err := proxy.SOCKS5(
		"tcp", proxyAddress.String(), nil, &net.Dialer{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayloadProbe, err)
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("%w: SOCKS dialer lacks context support",
			ErrPayloadProbe)
	}
	return contextDialer.DialContext, nil
}
