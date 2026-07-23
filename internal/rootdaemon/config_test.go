package rootdaemon

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const validConfig = `{
  "schema": "hexroute.root-observe.v1",
  "mode": "observe-only",
  "observation_interval_seconds": 60,
  "physical_interface": "en7",
  "managed_tun_address": "198.51.100.1",
  "upstream_probe_address": "203.0.113.53",
  "routes": [
    {
      "name": "ingress-a",
      "address": "192.0.2.20",
      "role": "ingress",
      "preferred_link": "physical"
    },
    {
      "name": "ingress-b",
      "address": "192.0.2.21",
      "role": "ingress",
      "preferred_link": "upstream_vpn"
    },
    {
      "name": "codex-a",
      "address": "203.0.113.20",
      "role": "codex_fallback"
    }
  ],
  "endpoints": [
    {
      "name": "outer-ready",
      "purpose": "outer_ready",
      "transport": "direct_tls",
      "address": "198.51.100.20:443",
      "server_name": "outer.example.invalid",
      "timeout_seconds": 4
    },
    {
      "name": "normal-codex",
      "purpose": "normal_codex",
      "transport": "direct_tls",
      "address": "203.0.113.40:443",
      "server_name": "normal.example.invalid",
      "timeout_seconds": 4
    },
    {
      "name": "twilight-codex",
      "purpose": "twilight_codex",
      "transport": "socks5_tls",
      "address": "203.0.113.41:443",
      "proxy_address": "127.0.0.1:2080",
      "server_name": "fallback.example.invalid",
      "timeout_seconds": 4
    }
  ]
}`

func TestDecodeConfigAcceptsSyntheticObserveOnlyConfig(t *testing.T) {
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	if config.Interval != time.Minute ||
		config.PhysicalInterface != "en7" ||
		len(config.Targets) != 3 ||
		len(config.Endpoints) != 3 {
		t.Fatalf("DecodeConfig() = %+v", config)
	}
}

func TestDecodeConfigRejectsMutationModeAndUnknownFields(t *testing.T) {
	fixtures := []string{
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "active"`, 1),
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "observe-only", "command": "route add"`, 1),
		strings.Replace(validConfig, `"physical_interface": "en7"`, `"physical_interface": "utun8; restart"`, 1),
	}
	for _, fixture := range fixtures {
		if _, err := DecodeConfig(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("DecodeConfig() error = %v, want %v", err, ErrInvalidConfig)
		}
	}
}

func TestDecodeConfigRequiresCodexReadinessPair(t *testing.T) {
	fixture := strings.Replace(
		validConfig,
		`"purpose": "twilight_codex"`,
		`"purpose": "outer_ready"`,
		1,
	)
	if _, err := DecodeConfig(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("DecodeConfig() error = %v, want %v", err, ErrInvalidConfig)
	}
}

func TestDecodeConfigRequiresSOCKSForTwilightCodex(t *testing.T) {
	fixture := strings.Replace(
		validConfig,
		`"transport": "socks5_tls"`,
		`"transport": "direct_tls"`,
		1,
	)
	fixture = strings.Replace(
		fixture,
		`      "proxy_address": "127.0.0.1:2080",
`,
		"",
		1,
	)
	if _, err := DecodeConfig(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("DecodeConfig() error = %v, want %v", err, ErrInvalidConfig)
	}
}
