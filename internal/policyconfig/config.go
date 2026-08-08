package policyconfig

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const StaticConfigSchema = "hexroute.policy-daemon-static.v1"

type StaticConfig struct {
	Schema            string                        `json:"schema"`
	Installed         policy.InstalledCompatibility `json:"installed"`
	PinnedPublicKey   string                        `json:"pinned_public_key"`
	SignerFingerprint string                        `json:"signer_fingerprint"`
}

type RuntimeConfig struct {
	Installed       policy.InstalledCompatibility
	PinnedPublicKey ed25519.PublicKey
}

var ErrInvalidConfig = errors.New("invalid policy control configuration")

func (config StaticConfig) Runtime(expectedDomain policy.Domain) (RuntimeConfig, error) {
	publicKey, err := decodePublicKey(config.PinnedPublicKey)
	if config.Schema != StaticConfigSchema || !expectedDomain.Valid() ||
		config.Installed.Validate() != nil || config.Installed.Domain != expectedDomain ||
		err != nil || policy.SHA256Hex(publicKey) != config.SignerFingerprint {
		return RuntimeConfig{}, ErrInvalidConfig
	}
	return RuntimeConfig{
		Installed: policy.InstalledCompatibility{
			Domain:                  config.Installed.Domain,
			MinimumPolicySchema:     config.Installed.MinimumPolicySchema,
			MaximumPolicySchema:     config.Installed.MaximumPolicySchema,
			CurrentPolicySchema:     config.Installed.CurrentPolicySchema,
			CurrentBundleGeneration: config.Installed.CurrentBundleGeneration,
			CurrentPolicyGeneration: config.Installed.CurrentPolicyGeneration,
			CurrentPayloadSHA256:    config.Installed.CurrentPayloadSHA256,
			StaticSHA256:            config.Installed.StaticSHA256,
			TrustedCompilerSHA256: append(
				[]string(nil),
				config.Installed.TrustedCompilerSHA256...,
			),
		},
		PinnedPublicKey: append(ed25519.PublicKey(nil), publicKey...),
	}, nil
}

func (config RuntimeConfig) Validate() error {
	if config.Installed.Validate() != nil ||
		len(config.PinnedPublicKey) != ed25519.PublicKeySize {
		return ErrInvalidConfig
	}
	return nil
}

func decodePublicKey(value string) (ed25519.PublicKey, error) {
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
	}
	return nil, ErrInvalidConfig
}
