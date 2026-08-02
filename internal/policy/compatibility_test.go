package policy

import (
	"errors"
	"testing"
)

func TestCheckCandidateCompatibilityForBothDomains(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := validEnvelopeSource(t, envelope)
	root, _ := source.DomainPayload(DomainRoot)
	user, _ := source.DomainPayload(DomainUser)
	rootDigest, _, _ := CanonicalSHA256(root)
	userDigest, _, _ := CanonicalSHA256(user)
	staticDigest, _ := envelope.SHA256()

	manifest := validManifest()
	manifest.StaticSHA256 = staticDigest
	manifest.Root = DomainReference{Generation: root.PolicyGeneration, PayloadSHA256: rootDigest}
	manifest.User = DomainReference{Generation: user.PolicyGeneration, PayloadSHA256: userDigest}

	for _, test := range []struct {
		name      string
		payload   DomainPayload
		installed InstalledCompatibility
	}{
		{
			name:    "root unchanged payload",
			payload: root,
			installed: InstalledCompatibility{
				Domain: DomainRoot, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
				CurrentPolicySchema: 1, CurrentBundleGeneration: 1,
				CurrentPolicyGeneration: 1, CurrentPayloadSHA256: rootDigest,
				StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{testDigest},
			},
		},
		{
			name:    "user advancing payload",
			payload: user,
			installed: InstalledCompatibility{
				Domain: DomainUser, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
				CurrentPolicySchema: 1, CurrentBundleGeneration: 1,
				CurrentPolicyGeneration: 1, CurrentPayloadSHA256: testDigest,
				StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{testDigest},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := CheckCandidateCompatibility(manifest, test.payload, test.installed); err != nil {
				t.Fatalf("compatible candidate: %v", err)
			}
		})
	}
}

func TestCheckCandidateCompatibilityRejectsUnsafeChanges(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := validEnvelopeSource(t, envelope)
	user, _ := source.DomainPayload(DomainUser)
	userDigest, _, _ := CanonicalSHA256(user)
	staticDigest, _ := envelope.SHA256()
	manifest := validManifest()
	manifest.StaticSHA256 = staticDigest
	manifest.User = DomainReference{Generation: user.PolicyGeneration, PayloadSHA256: userDigest}
	installed := InstalledCompatibility{
		Domain: DomainUser, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
		CurrentPolicySchema: 1, CurrentBundleGeneration: 1,
		CurrentPolicyGeneration: 1, CurrentPayloadSHA256: testDigest,
		StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{testDigest},
	}

	t.Run("restart required", func(t *testing.T) {
		candidate := manifest
		candidate.StaticSHA256 = testDigest
		if !errors.Is(CheckCandidateCompatibility(candidate, user, installed), ErrRestartRequired) {
			t.Fatal("static change should require restart")
		}
	})

	t.Run("untrusted compiler", func(t *testing.T) {
		candidate := manifest
		candidate.CompilerSHA256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		if !errors.Is(CheckCandidateCompatibility(candidate, user, installed), ErrUntrustedCompiler) {
			t.Fatal("untrusted compiler should be rejected")
		}
	})

	t.Run("unsupported schema", func(t *testing.T) {
		candidate := manifest
		candidate.PolicySchema = 3
		if !errors.Is(CheckCandidateCompatibility(candidate, user, installed), ErrUnsupportedPolicy) {
			t.Fatal("unsupported schema should be rejected")
		}
	})

	t.Run("schema downgrade", func(t *testing.T) {
		candidate := manifest
		candidate.PolicySchema = 1
		compatibility := installed
		compatibility.MaximumPolicySchema = 2
		compatibility.CurrentPolicySchema = 2
		if !errors.Is(CheckCandidateCompatibility(candidate, user, compatibility), ErrPolicyDowngrade) {
			t.Fatal("schema downgrade should be rejected")
		}
	})

	t.Run("generation downgrade", func(t *testing.T) {
		candidate := manifest
		candidate.BundleGeneration = 1
		candidate.ParentBundleGeneration = 0
		if !errors.Is(CheckCandidateCompatibility(candidate, user, installed), ErrPolicyDowngrade) {
			t.Fatal("generation downgrade should be rejected")
		}
	})

	t.Run("wrong domain", func(t *testing.T) {
		payload := user
		payload.Domain = DomainRoot
		if !errors.Is(CheckCandidateCompatibility(manifest, payload, installed), ErrPolicyDomainMismatch) {
			t.Fatal("wrong daemon domain should be rejected")
		}
	})
}

func TestCheckActiveCompatibilityRequiresExactInstalledState(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	staticDigest, _ := envelope.SHA256()
	source := validEnvelopeSource(t, envelope)
	payload, _ := source.DomainPayload(DomainUser)
	payloadDigest, _, _ := CanonicalSHA256(payload)
	manifest := validManifest()
	manifest.StaticSHA256 = staticDigest
	manifest.User = DomainReference{Generation: payload.PolicyGeneration, PayloadSHA256: payloadDigest}
	installed := InstalledCompatibility{
		Domain: DomainUser, MinimumPolicySchema: 1, MaximumPolicySchema: 1,
		CurrentPolicySchema: 1, CurrentBundleGeneration: manifest.BundleGeneration,
		CurrentPolicyGeneration: payload.PolicyGeneration, CurrentPayloadSHA256: payloadDigest,
		StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{testDigest},
	}
	if err := CheckActiveCompatibility(manifest, payload, installed); err != nil {
		t.Fatalf("active compatibility: %v", err)
	}

	wrongStatic := installed
	wrongStatic.StaticSHA256 = testDigest
	if !errors.Is(CheckActiveCompatibility(manifest, payload, wrongStatic), ErrRestartRequired) {
		t.Fatal("active static mismatch must require restart")
	}
	wrongGeneration := installed
	wrongGeneration.CurrentBundleGeneration++
	if !errors.Is(CheckActiveCompatibility(manifest, payload, wrongGeneration), ErrActivePolicyMismatch) {
		t.Fatal("active generation mismatch must fail")
	}
	wrongDigest := installed
	wrongDigest.CurrentPayloadSHA256 = testDigest
	if !errors.Is(CheckActiveCompatibility(manifest, payload, wrongDigest), ErrActivePolicyMismatch) {
		t.Fatal("active payload mismatch must fail")
	}
	unsupported := installed
	unsupported.MinimumPolicySchema = 2
	unsupported.MaximumPolicySchema = 2
	unsupported.CurrentPolicySchema = 2
	if !errors.Is(CheckActiveCompatibility(manifest, payload, unsupported), ErrUnsupportedPolicy) {
		t.Fatal("unsupported active schema must fail")
	}
}
