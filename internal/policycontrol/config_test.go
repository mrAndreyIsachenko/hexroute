package policycontrol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestStaticConfigPinsDomainAndSignerIdentity(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	config := syntheticStaticConfig(policy.DomainRoot, publicKey)
	runtime, err := config.Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatalf("Runtime() error: %v", err)
	}
	if runtime.Installed.Domain != policy.DomainRoot || !bytes.Equal(runtime.PinnedPublicKey, publicKey) {
		t.Fatalf("runtime config = %+v", runtime)
	}

	tests := []StaticConfig{config, config, config, config}
	tests[0].Installed.Domain = policy.DomainUser
	tests[1].SignerFingerprint = strings.Repeat("f", 64)
	tests[2].PinnedPublicKey = "not-base64"
	tests[3].Installed.TrustedCompilerSHA256 = nil
	for _, invalid := range tests {
		if _, err := invalid.Runtime(policy.DomainRoot); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Runtime() error = %v, want %v", err, ErrInvalidConfig)
		}
	}
}

func syntheticStaticConfig(domain policy.Domain, publicKey ed25519.PublicKey) StaticConfig {
	return StaticConfig{
		Schema: StaticConfigSchema,
		Installed: policy.InstalledCompatibility{
			Domain: domain, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
			CurrentPolicySchema: 1, StaticSHA256: strings.Repeat("a", 64),
			TrustedCompilerSHA256: []string{strings.Repeat("b", 64)},
		},
		PinnedPublicKey:   base64.RawStdEncoding.EncodeToString(publicKey),
		SignerFingerprint: policy.SHA256Hex(publicKey),
	}
}
