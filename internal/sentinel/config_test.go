package sentinel

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const validConfig = `{
  "schema": "hexroute.sentinel-observe.v1",
  "mode": "observe-only",
  "observation_interval_seconds": 15,
  "stale_threshold_seconds": 90,
  "heartbeat_path": "/Library/Application Support/Hexroute/observe-root/state/control-loop.heartbeat.json",
  "data_path_endpoint": {
    "transport": "socks5_tls",
    "certificate_policy": "verify",
    "address": "203.0.113.40:443",
    "proxy_address": "127.0.0.1:2080",
    "server_name": "fallback.example.invalid",
    "timeout_seconds": 4
  }
}`

func TestDecodeConfigAcceptsBoundedIndependentProbe(t *testing.T) {
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	if config.Interval != 15*time.Second ||
		config.StaleThreshold != 90 ||
		config.DataPathEndpoint.ProxyAddress.String() != "127.0.0.1:2080" {
		t.Fatalf("DecodeConfig() = %+v", config)
	}
}

func TestDecodeConfigRejectsMutationAndUnboundedProbeFields(t *testing.T) {
	fixtures := []string{
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "active"`, 1),
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "observe-only", "command": "restart"`, 1),
		strings.Replace(validConfig, `"stale_threshold_seconds": 90`, `"stale_threshold_seconds": 20`, 1),
		strings.Replace(validConfig, `"transport": "socks5_tls"`, `"transport": "direct_tls"`, 1),
		strings.Replace(validConfig, `"certificate_policy": "verify"`, `"certificate_policy": "handshake_only"`, 1),
		strings.Replace(validConfig, `"proxy_address": "127.0.0.1:2080"`, `"proxy_address": "192.0.2.10:2080"`, 1),
	}
	for _, fixture := range fixtures {
		if _, err := DecodeConfig(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("DecodeConfig() error = %v, want %v", err, ErrInvalidConfig)
		}
	}
}
