package ingressprobe

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

func newLoopbackSOCKSClient(timeout time.Duration, rawURL string) (*http.Client, error) {
	if timeout <= 0 || !validLoopbackSOCKSURL(rawURL) {
		return nil, errors.New("invalid socks proxy")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid socks proxy")
	}
	dialer, err := proxy.SOCKS5(
		"tcp",
		parsed.Host,
		nil,
		&net.Dialer{Timeout: timeout},
	)
	if err != nil {
		return nil, errors.New("invalid socks proxy")
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("invalid socks proxy")
	}
	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return contextDialer.DialContext(ctx, network, address)
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}
