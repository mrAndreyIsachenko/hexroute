package policy

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCompileBundleEmitsCanonicalManifestAndDomainPayloads(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	source := validEnvelopeSource(t, envelope)
	candidate, err := CompileBundle(
		source,
		envelope,
		CompilerIdentity{Version: "v0.1.0", SHA256: testDigest},
		testDigest,
		nil,
	)
	if err != nil {
		t.Fatalf("compile bundle: %v", err)
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("validate candidate: %v", err)
	}
	if string(candidate.ManifestJSON) == "" || string(candidate.RootJSON) == "" || string(candidate.UserJSON) == "" {
		t.Fatal("canonical candidate blobs must be emitted")
	}
	if candidate.Manifest.Root.PayloadSHA256 == candidate.Manifest.User.PayloadSHA256 {
		t.Fatal("distinct domain payloads unexpectedly have the same digest")
	}
}

func TestDecodeCandidateBundleRejectsTamperedManifestAndPayload(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	candidate := mustCompileBundle(t, validEnvelopeSource(t, envelope), envelope, nil)
	if _, err := DecodeCandidateBundle(candidate.ManifestJSON, candidate.RootJSON, candidate.UserJSON); err != nil {
		t.Fatalf("decode canonical candidate: %v", err)
	}

	tampered := candidate.Manifest
	tampered.Root.PayloadSHA256 = strings.Repeat("f", 64)
	_, tamperedManifest, _ := CanonicalSHA256(tampered)
	if _, err := DecodeCandidateBundle(tamperedManifest, candidate.RootJSON, candidate.UserJSON); !errors.Is(err, ErrInvalidCandidateBundle) {
		t.Fatalf("tampered manifest error = %v", err)
	}
	tamperedRoot := bytes.Replace(candidate.RootJSON, []byte(`"deny"`), []byte(`"allow"`), 1)
	if _, err := DecodeCandidateBundle(candidate.ManifestJSON, tamperedRoot, candidate.UserJSON); !errors.Is(err, ErrInvalidCandidateBundle) {
		t.Fatalf("tampered root payload error = %v", err)
	}
}

func TestCompileBundleIsStableAcrossSourceOrdering(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	firstSource := validEnvelopeSource(t, envelope)
	firstSource.User.Rules = append(firstSource.User.Rules, Rule{
		ID: "user.allow-resume-copy", Effect: EffectAllow,
		Selector: Selector{
			ID: "user.resume-pritunl-copy", Kind: SelectorAction,
			Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"},
		},
	})
	firstSource.User.Leases = append(firstSource.User.Leases, AuthorizationLease{
		ID: "user.night-recovery-copy", Domain: DomainUser,
		Capability: CapabilityOperatorResume, SelectorIDs: []string{"user.resume-pritunl-copy"},
		IssuedAt: testTime, ExpiresAt: testExpiry,
	})
	secondSource := firstSource
	secondSource.User.Rules = []Rule{firstSource.User.Rules[1], firstSource.User.Rules[0]}
	secondSource.User.Leases = []AuthorizationLease{firstSource.User.Leases[1], firstSource.User.Leases[0]}

	first := mustCompileBundle(t, firstSource, envelope, nil)
	second := mustCompileBundle(t, secondSource, envelope, nil)
	if !bytesEqual(first.ManifestJSON, second.ManifestJSON) ||
		!bytesEqual(first.RootJSON, second.RootJSON) ||
		!bytesEqual(first.UserJSON, second.UserJSON) {
		t.Fatal("source ordering changed canonical bundle output")
	}
}

func TestCompileBundleKeepsUnchangedDomainBlob(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	currentSource := validEnvelopeSource(t, envelope)
	current := mustCompileBundle(t, currentSource, envelope, nil)

	nextSource := validEnvelopeSource(t, envelope)
	nextSource.BundleGeneration = 3
	nextSource.ParentBundleGeneration = 2
	nextSource.User.PolicyGeneration = 3
	nextSource.User.Rules[0].Effect = EffectDeny
	next := mustCompileBundle(t, nextSource, envelope, &current.Snapshot)

	if current.Manifest.Root.PayloadSHA256 != next.Manifest.Root.PayloadSHA256 ||
		!bytesEqual(current.RootJSON, next.RootJSON) ||
		current.Manifest.Root.Generation != next.Manifest.Root.Generation {
		t.Fatal("user-only policy change modified the root domain payload")
	}
	if current.Manifest.User.PayloadSHA256 == next.Manifest.User.PayloadSHA256 ||
		next.Manifest.User.Generation <= current.Manifest.User.Generation {
		t.Fatal("user policy change did not advance only the user payload")
	}
}

func TestCompileBundleRejectsSemanticNoOp(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	currentSource := validEnvelopeSource(t, envelope)
	current := mustCompileBundle(t, currentSource, envelope, nil)
	nextSource := validEnvelopeSource(t, envelope)
	nextSource.BundleGeneration = 3
	nextSource.ParentBundleGeneration = 2

	if _, err := CompileBundle(
		nextSource,
		envelope,
		CompilerIdentity{Version: "v0.1.0", SHA256: testDigest},
		testDigest,
		&current.Snapshot,
	); !errors.Is(err, ErrSemanticNoOp) {
		t.Fatalf("semantic no-op error = %v", err)
	}
}

func mustCompileBundle(t *testing.T, source OperatorSource, envelope SafetyEnvelope, current *EffectiveSnapshot) CandidateBundle {
	t.Helper()
	candidate, err := CompileBundle(
		source,
		envelope,
		CompilerIdentity{Version: "v0.1.0", SHA256: testDigest},
		testDigest,
		current,
	)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}
