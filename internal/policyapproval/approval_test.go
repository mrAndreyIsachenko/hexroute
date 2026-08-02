package policyapproval

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/replay"
)

const (
	approvalIssued = "2026-08-02T09:00:00Z"
	approvalExpiry = "2026-08-02T10:00:00Z"
)

type syntheticSigner struct {
	private   ed25519.PrivateKey
	signCalls int
}

func (signer *syntheticSigner) PublicKey() (ed25519.PublicKey, error) {
	return append(ed25519.PublicKey(nil), signer.private.Public().(ed25519.PublicKey)...), nil
}

func (signer *syntheticSigner) Sign(message []byte) ([]byte, error) {
	signer.signCalls++
	return ed25519.Sign(signer.private, message), nil
}

func TestApproveAndVerifyCandidate(t *testing.T) {
	fixture := newApprovalFixture(t)
	review, approval, err := ApproveCandidate(
		fixture.candidate, &fixture.current, fixture.diff, fixture.replay,
		fixture.installed, fixture.signer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.signer.signCalls != 1 {
		t.Fatalf("sign calls = %d", fixture.signer.signCalls)
	}
	publicKey, _ := fixture.signer.PublicKey()
	now := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	if err := VerifyCandidate(fixture.candidate, review, approval, publicKey, now); err != nil {
		t.Fatalf("verify candidate: %v", err)
	}
}

func TestRollbackCandidatePassesNormalSigningAndVerificationGates(t *testing.T) {
	signer := &syntheticSigner{private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize))}
	publicKey, _ := signer.PublicKey()
	fingerprint := policy.SHA256Hex(publicKey)
	envelope := policy.DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity := policy.CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)}
	targetSource := approvalSource(staticDigest, 2, 1, 2, policy.EffectAllow)
	target, err := policy.CompileBundle(targetSource, envelope, identity, fingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSource := approvalSource(staticDigest, 3, 1, 3, policy.EffectDeny)
	current, err := policy.CompileBundle(currentSource, envelope, identity, fingerprint, &target.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := policy.CompileRollback(
		target,
		current,
		envelope,
		identity,
		fingerprint,
		policy.RollbackValidity{
			IssuedAt:  "2026-08-02T09:10:00Z",
			NotBefore: "2026-08-02T09:10:00Z",
			ExpiresAt: "2026-08-02T09:50:00Z",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := policy.BuildSemanticDiff(&current.Snapshot, candidate.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	replayReport, err := replay.EvaluatePolicy(candidate.Snapshot, []replay.PolicyCase{
		approvalCase("synthetic-root-deny", policy.DomainRoot, "routes", replay.DecisionDeny),
		approvalCase("synthetic-user-allow", policy.DomainUser, "pritunl", replay.DecisionAllow),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootDigest, _, _ := policy.CanonicalSHA256(current.Root)
	userDigest, _, _ := policy.CanonicalSHA256(current.User)
	installed := InstalledDomains{
		Root: policy.InstalledCompatibility{
			Domain: policy.DomainRoot, MinimumPolicySchema: 1, MaximumPolicySchema: 1, CurrentPolicySchema: 1,
			CurrentBundleGeneration: current.Manifest.BundleGeneration,
			CurrentPolicyGeneration: current.Root.PolicyGeneration, CurrentPayloadSHA256: rootDigest,
			StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{identity.SHA256},
		},
		User: policy.InstalledCompatibility{
			Domain: policy.DomainUser, MinimumPolicySchema: 1, MaximumPolicySchema: 1, CurrentPolicySchema: 1,
			CurrentBundleGeneration: current.Manifest.BundleGeneration,
			CurrentPolicyGeneration: current.User.PolicyGeneration, CurrentPayloadSHA256: userDigest,
			StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{identity.SHA256},
		},
	}
	review, approval, err := ApproveCandidate(
		candidate, &current.Snapshot, diff, replayReport, installed, signer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if signer.signCalls != 1 {
		t.Fatalf("rollback sign calls=%d", signer.signCalls)
	}
	validAt := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	if err := VerifyCandidate(candidate, review, approval, publicKey, validAt); err != nil {
		t.Fatalf("verify signed rollback: %v", err)
	}
}

func TestVerifyDomainCandidateAndCanonicalArtifacts(t *testing.T) {
	fixture := newApprovalFixture(t)
	review, approval, err := ApproveCandidate(
		fixture.candidate, &fixture.current, fixture.diff, fixture.replay,
		fixture.installed, fixture.signer,
	)
	if err != nil {
		t.Fatal(err)
	}
	reviewJSON, err := policy.MarshalCanonical(review)
	if err != nil {
		t.Fatal(err)
	}
	approvalJSON, err := policy.MarshalCanonical(approval)
	if err != nil {
		t.Fatal(err)
	}
	decodedReview, err := DecodeReviewArtifact(reviewJSON)
	if err != nil {
		t.Fatal(err)
	}
	decodedApproval, err := DecodeApprovalArtifact(approvalJSON)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _ := fixture.signer.PublicKey()
	validAt := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	for _, payload := range []policy.DomainPayload{fixture.candidate.Root, fixture.candidate.User} {
		if err := VerifyDomainCandidate(
			fixture.candidate.Manifest,
			fixture.candidate.ManifestSHA256,
			payload,
			decodedReview,
			decodedApproval,
			publicKey,
			validAt,
		); err != nil {
			t.Fatalf("verify %s domain: %v", payload.Domain, err)
		}
	}
	if _, err := DecodeReviewArtifact(append(reviewJSON, '\n')); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("non-canonical review error = %v", err)
	}
	if _, err := DecodeApprovalArtifact(append(approvalJSON, '\n')); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("non-canonical approval error = %v", err)
	}
}

func TestSigningGatesRunBeforeSigner(t *testing.T) {
	t.Run("failed replay", func(t *testing.T) {
		fixture := newApprovalFixture(t)
		failed, err := replay.EvaluatePolicy(fixture.candidate.Snapshot, []replay.PolicyCase{
			approvalCase("unsafe-expectation", policy.DomainUser, "pritunl", replay.DecisionAllow),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ApproveCandidate(
			fixture.candidate, &fixture.current, fixture.diff, failed,
			fixture.installed, fixture.signer,
		)
		if !errors.Is(err, ErrGateFailed) || fixture.signer.signCalls != 0 {
			t.Fatalf("failed replay reached signer: err=%v calls=%d", err, fixture.signer.signCalls)
		}
	})

	t.Run("compatibility", func(t *testing.T) {
		fixture := newApprovalFixture(t)
		fixture.installed.User.StaticSHA256 = strings.Repeat("f", 64)
		_, _, err := ApproveCandidate(
			fixture.candidate, &fixture.current, fixture.diff, fixture.replay,
			fixture.installed, fixture.signer,
		)
		if !errors.Is(err, ErrGateFailed) || fixture.signer.signCalls != 0 {
			t.Fatalf("incompatible candidate reached signer: err=%v calls=%d", err, fixture.signer.signCalls)
		}
	})

	t.Run("semantic no-op", func(t *testing.T) {
		fixture := newApprovalFixture(t)
		envelope := policy.DefaultSafetyEnvelope()
		staticDigest, _ := envelope.SHA256()
		source := approvalSource(staticDigest, 3, 1, 2, policy.EffectAllow)
		source.ParentBundleGeneration = 2
		publicKey, _ := fixture.signer.PublicKey()
		candidate, err := policy.CompileBundle(
			source, envelope,
			policy.CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)},
			policy.SHA256Hex(publicKey), nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		diff, err := policy.BuildSemanticDiff(&fixture.current, candidate.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		replayReport, err := replay.EvaluatePolicy(candidate.Snapshot, []replay.PolicyCase{
			approvalCase("synthetic-user-allow", policy.DomainUser, "pritunl", replay.DecisionAllow),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ApproveCandidate(
			candidate, &fixture.current, diff, replayReport, fixture.installed, fixture.signer,
		)
		if !errors.Is(err, ErrGateFailed) || fixture.signer.signCalls != 0 {
			t.Fatalf("semantic no-op reached signer: err=%v calls=%d", err, fixture.signer.signCalls)
		}
	})

	t.Run("selector conflict", func(t *testing.T) {
		fixture := newApprovalFixture(t)
		candidate := fixture.candidate
		candidate.Root = clonePayload(candidate.Root)
		candidate.Root.Rules = append(candidate.Root.Rules, policy.Rule{
			ID: "root.allow-routes", Effect: policy.EffectAllow,
			Selector: policy.Selector{ID: "root.allow-routes-selector", Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "routes"}},
		})
		candidate.Snapshot.Root = candidate.Root
		candidate.Manifest.Root.PayloadSHA256, candidate.RootJSON, _ = policy.CanonicalSHA256(candidate.Root)
		candidate.ManifestSHA256, candidate.ManifestJSON, _ = policy.CanonicalSHA256(candidate.Manifest)
		if candidate.Validate() != nil {
			t.Fatal("conflict fixture must remain structurally valid")
		}
		_, _, err := ApproveCandidate(
			candidate, &fixture.current, fixture.diff, fixture.replay,
			fixture.installed, fixture.signer,
		)
		if !errors.Is(err, ErrGateFailed) || fixture.signer.signCalls != 0 {
			t.Fatalf("conflicting candidate reached signer: err=%v calls=%d", err, fixture.signer.signCalls)
		}
	})
}

func TestVerifierRejectsWrongSignerTamperingAndExpiry(t *testing.T) {
	fixture := newApprovalFixture(t)
	review, approval, err := ApproveCandidate(
		fixture.candidate, &fixture.current, fixture.diff, fixture.replay,
		fixture.installed, fixture.signer,
	)
	if err != nil {
		t.Fatal(err)
	}
	validAt := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	publicKey, _ := fixture.signer.PublicKey()

	wrongPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	wrongPublic := wrongPrivate.Public().(ed25519.PublicKey)
	if !errors.Is(VerifyCandidate(fixture.candidate, review, approval, wrongPublic, validAt), ErrSignerMismatch) {
		t.Fatal("wrong pinned signer must be rejected")
	}

	tampered := approval
	tampered.Signature = strings.Repeat("a", len(approval.Signature))
	if !errors.Is(VerifyCandidate(fixture.candidate, review, tampered, publicKey, validAt), ErrApprovalSignature) {
		t.Fatal("tampered approval signature must be rejected")
	}

	tamperedManifest := fixture.candidate
	tamperedManifest.Manifest.CompilerVersion = "v0.1.1"
	tamperedManifest.ManifestSHA256, tamperedManifest.ManifestJSON, _ = policy.CanonicalSHA256(tamperedManifest.Manifest)
	if !errors.Is(VerifyCandidate(tamperedManifest, review, approval, publicKey, validAt), ErrSignerMismatch) {
		t.Fatal("validly encoded tampered manifest must break approval binding")
	}

	tamperedPayload := fixture.candidate
	tamperedPayload.User = clonePayload(fixture.candidate.User)
	tamperedPayload.User.Rules[0].ID = "user.changed-deny"
	tamperedPayload.Snapshot.User = tamperedPayload.User
	tamperedPayload.Manifest.User.PayloadSHA256, tamperedPayload.UserJSON, _ = policy.CanonicalSHA256(tamperedPayload.User)
	tamperedPayload.ManifestSHA256, tamperedPayload.ManifestJSON, _ = policy.CanonicalSHA256(tamperedPayload.Manifest)
	if !errors.Is(VerifyCandidate(tamperedPayload, review, approval, publicKey, validAt), ErrSignerMismatch) {
		t.Fatal("validly encoded tampered payload must break approval binding")
	}

	expiredAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if !errors.Is(VerifyCandidate(fixture.candidate, review, approval, publicKey, expiredAt), ErrApprovalExpired) {
		t.Fatal("expired approval must be rejected")
	}
}

func clonePayload(source policy.DomainPayload) policy.DomainPayload {
	cloned := source
	cloned.Rules = append([]policy.Rule(nil), source.Rules...)
	for index := range cloned.Rules {
		selector := cloned.Rules[index].Selector
		if selector.Action != nil {
			action := *selector.Action
			selector.Action = &action
		}
		cloned.Rules[index].Selector = selector
	}
	cloned.Leases = append([]policy.AuthorizationLease(nil), source.Leases...)
	return cloned
}

type approvalFixture struct {
	current   policy.EffectiveSnapshot
	candidate policy.CandidateBundle
	diff      policy.SemanticDiff
	replay    replay.PolicyReport
	installed InstalledDomains
	signer    *syntheticSigner
}

func newApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	signer := &syntheticSigner{private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))}
	publicKey, _ := signer.PublicKey()
	fingerprint := policy.SHA256Hex(publicKey)
	envelope := policy.DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	currentSource := approvalSource(staticDigest, 2, 1, 2, policy.EffectAllow)
	current, err := policy.ComposeEffectiveSnapshot(currentSource, envelope)
	if err != nil {
		t.Fatal(err)
	}
	candidateSource := approvalSource(staticDigest, 3, 1, 3, policy.EffectDeny)
	candidateSource.ParentBundleGeneration = 2
	candidate, err := policy.CompileBundle(
		candidateSource, envelope,
		policy.CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)},
		fingerprint, &current,
	)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := policy.BuildSemanticDiff(&current, candidate.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	replayReport, err := replay.EvaluatePolicy(candidate.Snapshot, []replay.PolicyCase{
		approvalCase("synthetic-root-deny", policy.DomainRoot, "routes", replay.DecisionDeny),
		approvalCase("synthetic-user-deny", policy.DomainUser, "pritunl", replay.DecisionDeny),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootDigest, _, _ := policy.CanonicalSHA256(current.Root)
	userDigest, _, _ := policy.CanonicalSHA256(current.User)
	compatibility := func(domain policy.Domain, generation uint64, payloadDigest string) policy.InstalledCompatibility {
		return policy.InstalledCompatibility{
			Domain: domain, MinimumPolicySchema: 1, MaximumPolicySchema: 1, CurrentPolicySchema: 1,
			CurrentBundleGeneration: 2, CurrentPolicyGeneration: generation,
			CurrentPayloadSHA256: payloadDigest, StaticSHA256: staticDigest,
			TrustedCompilerSHA256: []string{strings.Repeat("b", 64)},
		}
	}
	return approvalFixture{
		current: current, candidate: candidate, diff: diff, replay: replayReport, signer: signer,
		installed: InstalledDomains{
			Root: compatibility(policy.DomainRoot, 1, rootDigest),
			User: compatibility(policy.DomainUser, 2, userDigest),
		},
	}
}

func approvalSource(staticDigest string, bundle, rootGeneration, userGeneration uint64, userEffect policy.Effect) policy.OperatorSource {
	return policy.OperatorSource{
		Schema: policy.OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: bundle, ParentBundleGeneration: bundle - 1,
		StaticSHA256: staticDigest, IssuedAt: approvalIssued, NotBefore: approvalIssued, ExpiresAt: approvalExpiry,
		Root: policy.DomainSource{PolicyGeneration: rootGeneration, Rules: []policy.Rule{{
			ID: "root.deny-routes", Effect: policy.EffectDeny,
			Selector: policy.Selector{ID: "root.routes", Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "routes"}},
		}}},
		User: policy.DomainSource{PolicyGeneration: userGeneration, Rules: []policy.Rule{{
			ID: "user.resume-pritunl", Effect: userEffect,
			Selector: policy.Selector{ID: "user.pritunl", Kind: policy.SelectorAction,
				Action: &policy.ActionSelector{Capability: policy.CapabilityOperatorResume, Target: "pritunl"}},
		}}, Leases: []policy.AuthorizationLease{{
			ID: "user.synthetic-lease", Domain: policy.DomainUser, Capability: policy.CapabilityOperatorResume,
			SelectorIDs: []string{"user.pritunl"}, IssuedAt: approvalIssued, ExpiresAt: approvalExpiry,
		}}},
	}
}

func approvalCase(id string, domain policy.Domain, target string, expected replay.PolicyDecision) replay.PolicyCase {
	return replay.PolicyCase{
		Schema: replay.PolicyCaseSchema, Kind: replay.CaseSyntheticInvariant, ID: id,
		Domain: domain, Capability: policy.CapabilityOperatorResume, Target: target, Expected: expected,
	}
}
