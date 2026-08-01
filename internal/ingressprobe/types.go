package ingressprobe

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	RequestMaxBytes = 64 * 1024
	ResultSchema    = "hexroute.ingress-probe-result.v1"

	HeartbeatSchema         = "hexroute.ingress-heartbeat.v1"
	HeartbeatResponseSchema = "hexroute.ingress-heartbeat-response.v1"
	HeartbeatVersion        = 1
)

type Kind string

const (
	KindUnknown       Kind = "unknown"
	KindTCP           Kind = "tcp"
	KindTLSFallback   Kind = "tls-fallback"
	KindAuthenticated Kind = "authenticated"
	KindHeartbeat     Kind = "heartbeat"
)

type State string

const (
	StatePass State = "pass"
	StateFail State = "fail"
)

type Category string

const (
	CategoryOK                     Category = "ok"
	CategoryInvalidInput           Category = "invalid_input"
	CategoryTimeout                Category = "timeout"
	CategoryUnreachable            Category = "unreachable"
	CategoryTLS                    Category = "tls"
	CategoryDependency             Category = "dependency"
	CategoryAuthenticatedTransport Category = "authenticated_transport"
	CategoryHeartbeatResponse      Category = "heartbeat_response"
	CategoryHeartbeatAuthenticity  Category = "heartbeat_authenticity"
	CategoryHeartbeatFreshness     Category = "heartbeat_freshness"
	CategoryHeartbeatGeneration    Category = "heartbeat_generation"
	CategoryHeartbeatUnhealthy     Category = "heartbeat_unhealthy"
	CategoryInternal               Category = "internal"
)

type Result struct {
	Schema     string   `json:"schema"`
	Probe      Kind     `json:"probe"`
	State      State    `json:"state"`
	Category   Category `json:"category"`
	DurationMS int64    `json:"duration_ms"`
}

type Endpoint struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

type TCPRequest struct {
	Endpoint  Endpoint `json:"endpoint"`
	TimeoutMS uint32   `json:"timeout_ms"`
}

type TLSFallbackRequest struct {
	Endpoint   Endpoint `json:"endpoint"`
	ServerName string   `json:"server_name"`
	TimeoutMS  uint32   `json:"timeout_ms"`
}

type AuthenticatedRequest struct {
	Endpoint          Endpoint `json:"endpoint"`
	ServerName        string   `json:"server_name"`
	UserID            string   `json:"user_id"`
	RealityPublicKey  string   `json:"reality_public_key"`
	RealityShortID    string   `json:"reality_short_id"`
	TargetURL         string   `json:"target_url"`
	ExpectedStatusMin uint16   `json:"expected_status_min,omitempty"`
	ExpectedStatusMax uint16   `json:"expected_status_max,omitempty"`
	TimeoutMS         uint32   `json:"timeout_ms"`
}

type HeartbeatRequest struct {
	EndpointURL        string `json:"endpoint_url"`
	ExpectedNodeID     string `json:"expected_node_id"`
	ExpectedKeyID      string `json:"expected_key_id"`
	PublicKey          string `json:"public_key"`
	ExpectedGeneration string `json:"expected_generation"`
	MaxAgeMS           uint32 `json:"max_age_ms"`
	TimeoutMS          uint32 `json:"timeout_ms"`
}

type probeProcess interface {
	Done() <-chan struct{}
	Stop()
}

type processStarter func(context.Context, string, string) (probeProcess, error)
type socksFetcher func(context.Context, string, string, int, int) error

type Runner struct {
	now          func() time.Time
	dialContext  func(context.Context, string, string) (net.Conn, error)
	tlsConfig    func(string) *tls.Config
	httpClient   func(time.Duration) *http.Client
	binaryPath   string
	startProcess processStarter
	mkdirTemp    func(string, string) (string, error)
	removeAll    func(string) error
	socksFetch   socksFetcher
}
