package rootdaemon

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol"
)

const validConfig = `{
  "schema": "hexroute.root-observe.v1",
  "mode": "observe-only",
  "observation_interval_seconds": 60,
  "operator_uid": 501,
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
      "certificate_policy": "handshake_only",
      "address": "198.51.100.20:443",
      "server_name": "outer.example.invalid",
      "timeout_seconds": 4
    },
    {
      "name": "normal-codex",
      "purpose": "normal_codex",
      "transport": "direct_tls",
      "certificate_policy": "verify",
      "address": "203.0.113.40:443",
      "server_name": "normal.example.invalid",
      "timeout_seconds": 4
    },
    {
      "name": "twilight-codex",
      "purpose": "twilight_codex",
      "transport": "socks5_tls",
      "certificate_policy": "verify",
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
		config.OperatorUID != 501 ||
		config.PhysicalInterface != "en7" ||
		len(config.Targets) != 3 ||
		len(config.Endpoints) != 3 {
		t.Fatalf("DecodeConfig() = %+v", config)
	}
}

func TestDecodeConfigRejectsMutationModeAndUnknownFields(t *testing.T) {
	fixtures := []string{
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "active"`, 1),
		strings.Replace(validConfig, `"operator_uid": 501`, `"operator_uid": -1`, 1),
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

func TestDecodeConfigAcceptsOnlyRootPolicyControlIdentity(t *testing.T) {
	fixture := configWithPolicyControl(t, validConfig, policy.DomainRoot)
	config, err := DecodeConfig(bytes.NewReader(fixture))
	if err != nil || config.PolicyControl == nil ||
		config.PolicyControl.Installed.Domain != policy.DomainRoot {
		t.Fatalf("DecodeConfig() = %+v, %v", config, err)
	}
	fixture = configWithPolicyControl(t, validConfig, policy.DomainUser)
	if _, err := DecodeConfig(bytes.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("cross-domain policy control error = %v", err)
	}
}

func configWithPolicyControl(
	t *testing.T,
	base string,
	domain policy.Domain,
) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(base), &document); err != nil {
		t.Fatal(err)
	}
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	document["policy_control"] = policycontrol.StaticConfig{
		Schema: policycontrol.StaticConfigSchema,
		Installed: policy.InstalledCompatibility{
			Domain: domain, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
			CurrentPolicySchema: 1, StaticSHA256: policy.SHA256Hex([]byte("synthetic-static")),
			TrustedCompilerSHA256: []string{policy.SHA256Hex([]byte("synthetic-compiler"))},
		},
		PinnedPublicKey:   base64.RawStdEncoding.EncodeToString(publicKey),
		SignerFingerprint: policy.SHA256Hex(publicKey),
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
