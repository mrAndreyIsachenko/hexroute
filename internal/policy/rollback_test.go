package policy

import (
	"errors"
	"strings"
	"testing"
)

const (
	rollbackIssued  = "2026-08-02T12:00:00Z"
	rollbackExpires = "2026-08-02T13:00:00Z"
)

func TestCompileRollbackCreatesMonotonicGenerationWithoutExpiredLeases(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity := CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)}
	signerFingerprint := strings.Repeat("c", 64)
	targetSource := rollbackSource(
		staticDigest, 9, 8, 4, 5,
		EffectAllow, testTime, testExpiry,
	)
	target, err := CompileBundle(targetSource, envelope, identity, signerFingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSource := rollbackSource(
		staticDigest, 12, 11, 4, 6,
		EffectDeny, "2026-08-02T11:00:00Z", rollbackIssued,
	)
	currentSource.User.Leases = nil
	current, err := CompileBundle(currentSource, envelope, identity, signerFingerprint, &target.Snapshot)
	if err != nil {
		t.Fatal(err)
	}

	candidate, err := CompileRollback(
		target,
		current,
		envelope,
		identity,
		signerFingerprint,
		RollbackValidity{IssuedAt: rollbackIssued, NotBefore: rollbackIssued, ExpiresAt: rollbackExpires},
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Manifest.BundleGeneration != 13 || candidate.Manifest.ParentBundleGeneration != 12 {
		t.Fatalf("rollback bundle=%d parent=%d", candidate.Manifest.BundleGeneration, candidate.Manifest.ParentBundleGeneration)
	}
	if candidate.Root.PolicyGeneration != 4 {
		t.Fatalf("unchanged root generation=%d", candidate.Root.PolicyGeneration)
	}
	if candidate.User.PolicyGeneration != 7 {
		t.Fatalf("changed user generation=%d", candidate.User.PolicyGeneration)
	}
	if candidate.Manifest.IssuedAt != rollbackIssued || candidate.Manifest.NotBefore != rollbackIssued ||
		candidate.Manifest.ExpiresAt != rollbackExpires {
		t.Fatalf("rollback reused historical validity: %+v", candidate.Manifest)
	}
	if len(candidate.User.Leases) != 0 || len(candidate.User.Rules) != 0 {
		t.Fatalf("expired lease or its allow rule was revived: rules=%+v leases=%+v", candidate.User.Rules, candidate.User.Leases)
	}
	if candidate.Validate() != nil {
		t.Fatal("rollback candidate must remain canonical and valid")
	}
}

func TestCompileRollbackRejectsRevokedCredentialReference(t *testing.T) {
	envelope := broadTestEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity := CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)}
	signerFingerprint := strings.Repeat("c", 64)
	targetSource := OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 9, ParentBundleGeneration: 8,
		StaticSHA256: staticDigest, IssuedAt: testTime, NotBefore: testTime, ExpiresAt: testExpiry,
		Root: DomainSource{PolicyGeneration: 4},
		User: DomainSource{PolicyGeneration: 5, Rules: []Rule{{
			ID: "user.deny-old-credential", Effect: EffectDeny,
			Selector: Selector{
				ID: "user.old-credential", Kind: SelectorCredential,
				Credential: &CredentialSelector{Reference: "old-login", Owner: DomainUser},
			},
		}}},
	}
	target, err := CompileBundle(targetSource, envelope, identity, signerFingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSource := OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: 12, ParentBundleGeneration: 11,
		StaticSHA256: staticDigest,
		IssuedAt:     "2026-08-02T11:00:00Z", NotBefore: "2026-08-02T11:00:00Z", ExpiresAt: rollbackIssued,
		Root: DomainSource{PolicyGeneration: 4},
		User: DomainSource{PolicyGeneration: 6},
	}
	current, err := CompileBundle(currentSource, envelope, identity, signerFingerprint, &target.Snapshot)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CompileRollback(
		target,
		current,
		envelope,
		identity,
		signerFingerprint,
		RollbackValidity{IssuedAt: rollbackIssued, NotBefore: rollbackIssued, ExpiresAt: rollbackExpires},
	)
	if !errors.Is(err, ErrRollbackCredentialChange) {
		t.Fatalf("revoked credential rollback error=%v", err)
	}
}

func TestCompileRollbackRequiresOlderCompatibleTarget(t *testing.T) {
	envelope := DefaultSafetyEnvelope()
	staticDigest, err := envelope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	identity := CompilerIdentity{Version: "v0.1.0", SHA256: strings.Repeat("b", 64)}
	signerFingerprint := strings.Repeat("c", 64)
	source := rollbackSource(staticDigest, 12, 11, 4, 6, EffectDeny, testTime, testExpiry)
	source.User.Leases = nil
	current, err := CompileBundle(source, envelope, identity, signerFingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileRollback(
		current,
		current,
		envelope,
		identity,
		signerFingerprint,
		RollbackValidity{IssuedAt: rollbackIssued, NotBefore: rollbackIssued, ExpiresAt: rollbackExpires},
	)
	if !errors.Is(err, ErrInvalidRollback) {
		t.Fatalf("non-historical target error=%v", err)
	}
}

func rollbackSource(
	staticDigest string,
	bundle uint64,
	parent uint64,
	rootGeneration uint64,
	userGeneration uint64,
	userEffect Effect,
	issuedAt string,
	expiresAt string,
) OperatorSource {
	return OperatorSource{
		Schema: OperatorSourceSchema, PolicySchema: 1,
		BundleGeneration: bundle, ParentBundleGeneration: parent,
		StaticSHA256: staticDigest, IssuedAt: issuedAt, NotBefore: issuedAt, ExpiresAt: expiresAt,
		Root: DomainSource{PolicyGeneration: rootGeneration, Rules: []Rule{{
			ID: "root.deny-routes", Effect: EffectDeny,
			Selector: Selector{ID: "root.routes", Kind: SelectorAction,
				Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "routes"}},
		}}},
		User: DomainSource{PolicyGeneration: userGeneration, Rules: []Rule{{
			ID: "user.resume-pritunl", Effect: userEffect,
			Selector: Selector{ID: "user.pritunl", Kind: SelectorAction,
				Action: &ActionSelector{Capability: CapabilityOperatorResume, Target: "pritunl"}},
		}}, Leases: []AuthorizationLease{{
			ID: "user.rollback-lease", Domain: DomainUser, Capability: CapabilityOperatorResume,
			SelectorIDs: []string{"user.pritunl"}, IssuedAt: issuedAt, ExpiresAt: expiresAt,
		}}},
	}
}
