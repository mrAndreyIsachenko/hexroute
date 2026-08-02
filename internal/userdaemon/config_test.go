package userdaemon

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
  "schema": "hexroute.user-observe.v1",
  "mode": "observe-only",
  "observation_interval_seconds": 15,
  "expected_uid": 501,
  "profile_id": "synthetic-profile",
  "pritunl_cli": "/usr/local/bin/pritunl-client",
  "outer_endpoint": {
    "transport": "direct_tls",
    "certificate_policy": "handshake_only",
    "address": "198.51.100.30:443",
    "server_name": "outer.example.invalid",
    "timeout_seconds": 4
  },
  "policy": {
    "failure_threshold": 2,
    "action_budget": 3,
    "base_backoff_seconds": 15,
    "max_backoff_seconds": 120,
    "verification_window_seconds": 30,
    "cooldown_seconds": 600,
    "wake_settle_seconds": 30,
    "connecting_grace_seconds": 120,
    "otp_period_seconds": 30,
    "otp_min_valid_seconds": 8
  }
}`

func TestDecodeConfigAcceptsSyntheticObserveOnlyConfig(t *testing.T) {
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	if config.Interval != 15*time.Second ||
		config.ExpectedUID != 501 ||
		config.ProfileID != "synthetic-profile" ||
		config.Policy.Recovery.ActionBudget != 3 {
		t.Fatalf("DecodeConfig() = %+v", config)
	}
}

func TestDecodeConfigRejectsMutationAndCredentialFields(t *testing.T) {
	fixtures := []string{
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "active"`, 1),
		strings.Replace(validConfig, `"mode": "observe-only"`, `"mode": "observe-only", "command": "connect"`, 1),
		strings.Replace(validConfig, `"profile_id": "synthetic-profile"`, `"profile_id": "synthetic-profile", "pin": "canary"`, 1),
		strings.Replace(validConfig, `"profile_id": "synthetic-profile"`, `"profile_id": "bad;profile"`, 1),
		strings.Replace(validConfig, `"transport": "direct_tls"`, `"transport": "socks5_tls"`, 1),
	}
	for _, fixture := range fixtures {
		if _, err := DecodeConfig(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("DecodeConfig() error = %v, want %v", err, ErrInvalidConfig)
		}
	}
}

func TestDecodeConfigAcceptsOnlyUserPolicyControlIdentity(t *testing.T) {
	fixture := userConfigWithPolicyControl(t, validConfig, policy.DomainUser)
	config, err := DecodeConfig(bytes.NewReader(fixture))
	if err != nil || config.PolicyControl == nil ||
		config.PolicyControl.Installed.Domain != policy.DomainUser {
		t.Fatalf("DecodeConfig() = %+v, %v", config, err)
	}
	fixture = userConfigWithPolicyControl(t, validConfig, policy.DomainRoot)
	if _, err := DecodeConfig(bytes.NewReader(fixture)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("cross-domain policy control error = %v", err)
	}
}

func userConfigWithPolicyControl(
	t *testing.T,
	base string,
	domain policy.Domain,
) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(base), &document); err != nil {
		t.Fatal(err)
	}
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{6}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
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
