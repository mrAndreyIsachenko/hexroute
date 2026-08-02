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
