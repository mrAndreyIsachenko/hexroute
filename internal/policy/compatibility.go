package policy

import "errors"

const MaxTrustedCompilers = 16

type InstalledCompatibility struct {
	Domain                  Domain   `json:"domain"`
	MinimumPolicySchema     uint16   `json:"minimum_policy_schema"`
	MaximumPolicySchema     uint16   `json:"maximum_policy_schema"`
	CurrentPolicySchema     uint16   `json:"current_policy_schema"`
	CurrentBundleGeneration uint64   `json:"current_bundle_generation"`
	CurrentPolicyGeneration uint64   `json:"current_policy_generation"`
	CurrentPayloadSHA256    string   `json:"current_payload_sha256,omitempty"`
	StaticSHA256            string   `json:"static_sha256"`
	TrustedCompilerSHA256   []string `json:"trusted_compiler_sha256"`
}

var (
	ErrInvalidCompatibility = errors.New("invalid installed policy compatibility")
	ErrUnsupportedPolicy    = errors.New("unsupported policy schema")
	ErrUntrustedCompiler    = errors.New("untrusted policy compiler")
	ErrRestartRequired      = errors.New("policy static configuration requires restart")
	ErrPolicyDowngrade      = errors.New("policy downgrade is not allowed")
	ErrPolicyDomainMismatch = errors.New("policy domain does not match daemon")
)

func (installed InstalledCompatibility) Validate() error {
	if !installed.Domain.Valid() ||
		installed.MinimumPolicySchema == 0 ||
		installed.MaximumPolicySchema < installed.MinimumPolicySchema ||
		installed.CurrentPolicySchema < installed.MinimumPolicySchema ||
		installed.CurrentPolicySchema > installed.MaximumPolicySchema ||
		!validSHA256(installed.StaticSHA256) ||
		len(installed.TrustedCompilerSHA256) == 0 ||
		len(installed.TrustedCompilerSHA256) > MaxTrustedCompilers ||
		hasDuplicateStrings(installed.TrustedCompilerSHA256) {
		return ErrInvalidCompatibility
	}
	for _, digest := range installed.TrustedCompilerSHA256 {
		if !validSHA256(digest) {
			return ErrInvalidCompatibility
		}
	}
	if installed.CurrentBundleGeneration == 0 {
		if installed.CurrentPolicyGeneration != 0 || installed.CurrentPayloadSHA256 != "" {
			return ErrInvalidCompatibility
		}
	} else if installed.CurrentPolicyGeneration == 0 || !validSHA256(installed.CurrentPayloadSHA256) {
		return ErrInvalidCompatibility
	}
	return nil
}

func CheckCandidateCompatibility(
	manifest Manifest,
	payload DomainPayload,
	installed InstalledCompatibility,
) error {
	if manifest.Validate() != nil || installed.Validate() != nil {
		return ErrInvalidCompatibility
	}
	if payload.Domain != installed.Domain {
		return ErrPolicyDomainMismatch
	}
	if payload.Validate() != nil {
		return ErrInvalidCompatibility
	}
	if manifest.PolicySchema < installed.MinimumPolicySchema ||
		manifest.PolicySchema > installed.MaximumPolicySchema {
		return ErrUnsupportedPolicy
	}
	if manifest.PolicySchema < installed.CurrentPolicySchema {
		return ErrPolicyDowngrade
	}
	if !containsString(installed.TrustedCompilerSHA256, manifest.CompilerSHA256) {
		return ErrUntrustedCompiler
	}
	if manifest.StaticSHA256 != installed.StaticSHA256 {
		return ErrRestartRequired
	}
	if manifest.ParentBundleGeneration != installed.CurrentBundleGeneration ||
		manifest.BundleGeneration <= installed.CurrentBundleGeneration ||
		payload.PolicyGeneration < installed.CurrentPolicyGeneration {
		return ErrPolicyDowngrade
	}

	digest, _, err := CanonicalSHA256(payload)
	if err != nil {
		return ErrInvalidCompatibility
	}
	reference := manifest.Root
	if installed.Domain == DomainUser {
		reference = manifest.User
	}
	if reference.Generation != payload.PolicyGeneration || reference.PayloadSHA256 != digest {
		return ErrPolicyDomainMismatch
	}
	if payload.PolicyGeneration == installed.CurrentPolicyGeneration && digest != installed.CurrentPayloadSHA256 {
		return ErrPolicyDowngrade
	}
	return nil
}
