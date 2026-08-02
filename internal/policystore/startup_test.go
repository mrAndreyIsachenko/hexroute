package policystore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

const (
	startupIssued = "2030-01-01T00:00:00Z"
	startupExpiry = "2030-01-01T01:00:00Z"
)

func TestStoreRevalidatesActiveGenerationForEachDomainWithoutWrites(t *testing.T) {
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			store, path := newTestStore(t, domain)
			defer store.Close()
			fixture := newStartupFixture(t, domain, 1)
			installStartupFixture(t, store, fixture)
			before := snapshotStoreTree(t, path)

			active, err := store.RevalidateActive(
				fixture.installed,
				fixture.publicKey,
				time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC),
			)
			if err != nil {
				t.Fatalf("revalidate active: %v", err)
			}
			if active.Domain != domain || active.Generation != fixture.generation ||
				active.ManifestSHA256 != fixture.manifestDigest ||
				active.PayloadSHA256 != fixture.payloadDigest ||
				active.Payload.Domain != domain || active.ActivatedAt != activatedAt {
				t.Fatalf("revalidated active = %+v", active)
			}
			after := snapshotStoreTree(t, path)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("startup revalidation modified store:\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestStoreStartupRevalidationRejectsInvalidActiveEvidence(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)

	t.Run("non-canonical manifest", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installStartupFixture(t, store, fixture)
		replaceGenerationArtifact(
			t, path, fixture.generation, policy.DomainRoot, ArtifactManifest,
			append(append([]byte(nil), fixture.artifacts[ArtifactManifest]...), '\n'),
		)
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, validAt); !errors.Is(err, policy.ErrInvalidCandidateBundle) {
			t.Fatalf("non-canonical manifest error = %v", err)
		}
	})

	t.Run("payload digest mismatch", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainUser)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainUser, 1)
		installStartupFixture(t, store, fixture)
		tampered := fixture.payload
		tampered.PolicyGeneration++
		_, encoded, err := policy.CanonicalSHA256(tampered)
		if err != nil {
			t.Fatal(err)
		}
		replaceGenerationArtifact(
			t, path, fixture.generation, policy.DomainUser, ArtifactPayload, encoded,
		)
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, validAt); !errors.Is(err, ErrActivePointerConsistency) {
			t.Fatalf("payload digest mismatch error = %v", err)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		fixture.approval.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		fixture.rebindApproval(t)
		installStartupFixture(t, store, fixture)
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, validAt); !errors.Is(err, policyapproval.ErrApprovalSignature) {
			t.Fatalf("invalid signature error = %v", err)
		}
	})

	t.Run("wrong pinned key", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installStartupFixture(t, store, fixture)
		wrongPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{99}, ed25519.SeedSize))
		wrongPublic := wrongPrivate.Public().(ed25519.PublicKey)
		if _, err := store.RevalidateActive(fixture.installed, wrongPublic, validAt); !errors.Is(err, policyapproval.ErrSignerMismatch) {
			t.Fatalf("wrong pinned key error = %v", err)
		}
	})

	t.Run("static mismatch", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installStartupFixture(t, store, fixture)
		installed := fixture.installed
		installed.StaticSHA256 = policy.SHA256Hex([]byte("different-installed-static"))
		if _, err := store.RevalidateActive(installed, fixture.publicKey, validAt); !errors.Is(err, policy.ErrRestartRequired) {
			t.Fatalf("static mismatch error = %v", err)
		}
	})

	t.Run("unsupported policy schema", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 2)
		installed := fixture.installed
		installed.MinimumPolicySchema = 1
		installed.MaximumPolicySchema = 1
		installed.CurrentPolicySchema = 1
		installStartupFixture(t, store, fixture)
		if _, err := store.RevalidateActive(installed, fixture.publicKey, validAt); !errors.Is(err, policy.ErrUnsupportedPolicy) {
			t.Fatalf("unsupported schema error = %v", err)
		}
	})

	t.Run("expired approval", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainUser)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainUser, 1)
		installStartupFixture(t, store, fixture)
		expiredAt := time.Date(2030, time.January, 1, 1, 0, 0, 0, time.UTC)
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, expiredAt); !errors.Is(err, policyapproval.ErrApprovalExpired) {
			t.Fatalf("expired approval error = %v", err)
		}
	})

	t.Run("missing active resolution", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installStartupFixture(t, store, fixture)
		resolutionName := mustTransactionRecordFilename(t, recordResolution, transactionOne)
		if err := os.Remove(filepath.Join(path, stateDirectory, resolutionName)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, validAt); !errors.Is(err, ErrActivePointerConsistency) {
			t.Fatalf("missing resolution error = %v", err)
		}
	})

	t.Run("future activation timestamp", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		fixture.pointer.ActivatedAt = "2030-01-01T00:45:00Z"
		installStartupFixture(t, store, fixture)
		if _, err := store.RevalidateActive(fixture.installed, fixture.publicKey, validAt); !errors.Is(err, ErrActiveClockAnomaly) {
			t.Fatalf("future activation timestamp error = %v", err)
		}
	})
}

type startupFixture struct {
	domain         policy.Domain
	generation     Generation
	manifest       policy.Manifest
	payload        policy.DomainPayload
	review         policyapproval.ReviewReport
	approval       policyapproval.SignedApproval
	publicKey      ed25519.PublicKey
	manifestDigest string
	payloadDigest  string
	artifacts      map[ArtifactKind][]byte
	installed      policy.InstalledCompatibility
	receipt        PrepareReceipt
	intent         CommitIntent
	pointer        ActivePointer
	resolution     ResolutionRecord
}

func newStartupFixture(t *testing.T, domain policy.Domain, policySchema uint16) *startupFixture {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	root := policy.DomainPayload{
		Schema: policy.DomainPayloadSchema, Domain: policy.DomainRoot,
		PolicyGeneration: 4, Rules: []policy.Rule{},
	}
	user := policy.DomainPayload{
		Schema: policy.DomainPayloadSchema, Domain: policy.DomainUser,
		PolicyGeneration: 5, Rules: []policy.Rule{},
	}
	rootDigest, rootJSON, err := policy.CanonicalSHA256(root)
	if err != nil {
		t.Fatal(err)
	}
	userDigest, userJSON, err := policy.CanonicalSHA256(user)
	if err != nil {
		t.Fatal(err)
	}
	staticDigest := policy.SHA256Hex([]byte("synthetic-installed-static"))
	compilerDigest := policy.SHA256Hex([]byte("synthetic-compiler"))
	manifest := policy.Manifest{
		Schema: policy.ManifestSchema, PolicySchema: policySchema,
		CompilerVersion: "v0.1.0", CompilerSHA256: compilerDigest,
		BundleGeneration: 7, ParentBundleGeneration: 6,
		Root:         policy.DomainReference{Generation: root.PolicyGeneration, PayloadSHA256: rootDigest},
		User:         policy.DomainReference{Generation: user.PolicyGeneration, PayloadSHA256: userDigest},
		StaticSHA256: staticDigest, SignerFingerprint: policy.SHA256Hex(publicKey),
		IssuedAt: startupIssued, NotBefore: startupIssued, ExpiresAt: startupExpiry,
	}
	manifestDigest, manifestJSON, err := policy.CanonicalSHA256(manifest)
	if err != nil || manifest.Validate() != nil {
		t.Fatalf("synthetic manifest: %v", err)
	}
	review := policyapproval.ReviewReport{
		Schema: policyapproval.ReviewSchema, ManifestSHA256: manifestDigest,
		CandidateSemanticSHA256: policy.SHA256Hex([]byte("synthetic-semantic")),
		DiffSHA256:              policy.SHA256Hex([]byte("synthetic-diff")),
		ReplaySHA256:            policy.SHA256Hex([]byte("synthetic-replay")),
		Checks: []policyapproval.GateCode{
			policyapproval.GateCompatible,
			policyapproval.GateConflictFree,
			policyapproval.GateReplayPassed,
			policyapproval.GateSemanticDiff,
		},
	}
	reviewDigest, err := policyapproval.ReviewSHA256(review)
	if err != nil {
		t.Fatal(err)
	}
	statement := policyapproval.ApprovalStatement{
		Schema: policyapproval.ApprovalSchema, ManifestSHA256: manifestDigest,
		RootSHA256: rootDigest, UserSHA256: userDigest, ReviewSHA256: reviewDigest,
		SignerFingerprint: policy.SHA256Hex(publicKey),
		NotBefore:         startupIssued, ExpiresAt: startupExpiry,
	}
	canonicalStatement, err := policy.MarshalCanonical(statement)
	if err != nil {
		t.Fatal(err)
	}
	approval := policyapproval.SignedApproval{
		Statement: statement,
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, canonicalStatement)),
	}
	payload := root
	payloadDigest := rootDigest
	payloadJSON := rootJSON
	if domain == policy.DomainUser {
		payload = user
		payloadDigest = userDigest
		payloadJSON = userJSON
	}
	fixture := &startupFixture{
		domain: domain, generation: Generation{Bundle: 7, Policy: payload.PolicyGeneration},
		manifest: manifest, payload: payload, review: review, approval: approval,
		publicKey:      append(ed25519.PublicKey(nil), publicKey...),
		manifestDigest: manifestDigest, payloadDigest: payloadDigest,
		artifacts: map[ArtifactKind][]byte{
			ArtifactManifest: manifestJSON,
			ArtifactPayload:  payloadJSON,
		},
		installed: policy.InstalledCompatibility{
			Domain: domain, MinimumPolicySchema: policySchema, MaximumPolicySchema: policySchema,
			CurrentPolicySchema: policySchema, CurrentBundleGeneration: 7,
			CurrentPolicyGeneration: payload.PolicyGeneration, CurrentPayloadSHA256: payloadDigest,
			StaticSHA256: staticDigest, TrustedCompilerSHA256: []string{compilerDigest},
		},
	}
	fixture.rebindApproval(t)
	return fixture
}

func (fixture *startupFixture) rebindApproval(t *testing.T) {
	t.Helper()
	reviewJSON, err := policy.MarshalCanonical(fixture.review)
	if err != nil {
		t.Fatal(err)
	}
	approvalJSON, err := policy.MarshalCanonical(fixture.approval)
	if err != nil {
		t.Fatal(err)
	}
	approvalDigest := policy.SHA256Hex(approvalJSON)
	fixture.artifacts[ArtifactReview] = reviewJSON
	fixture.artifacts[ArtifactApproval] = approvalJSON
	fixture.intent = CommitIntent{
		Schema: CommitIntentSchema, TransactionID: transactionOne,
		BundleGeneration:     fixture.generation.Bundle,
		RootPolicyGeneration: fixture.manifest.Root.Generation,
		UserPolicyGeneration: fixture.manifest.User.Generation,
		ManifestSHA256:       fixture.manifestDigest,
		RootPayloadSHA256:    fixture.manifest.Root.PayloadSHA256,
		UserPayloadSHA256:    fixture.manifest.User.PayloadSHA256,
		ApprovalSHA256:       approvalDigest, Approval: fixture.approval, CreatedAt: createdAt,
	}
	if fixture.intent.Validate() != nil {
		t.Fatal("synthetic startup commit intent is invalid")
	}
	fixture.receipt = PrepareReceipt{
		Schema: PrepareReceiptSchema, TransactionID: transactionOne, Domain: fixture.domain,
		BundleGeneration: fixture.generation.Bundle, PolicyGeneration: fixture.generation.Policy,
		ManifestSHA256: fixture.manifestDigest, PayloadSHA256: fixture.payloadDigest,
		ApprovalSHA256: approvalDigest, PreparedAt: preparedAt,
	}
	intentDigest, err := CommitIntentSHA256(fixture.intent)
	if err != nil {
		t.Fatal(err)
	}
	fixture.pointer = ActivePointer{
		Schema: ActivePointerSchema, TransactionID: transactionOne, Domain: fixture.domain,
		BundleGeneration: fixture.generation.Bundle, PolicyGeneration: fixture.generation.Policy,
		ManifestSHA256: fixture.manifestDigest, PayloadSHA256: fixture.payloadDigest,
		ApprovalSHA256: approvalDigest, CommitIntentSHA256: intentDigest,
		Approval: fixture.approval, ActivatedAt: activatedAt,
	}
	fixture.resolution = ResolutionRecord{
		Schema: ResolutionSchema, TransactionID: transactionOne, Domain: fixture.domain,
		State: policy.PolicyActive, BundleGeneration: fixture.generation.Bundle,
		PolicyGeneration: fixture.generation.Policy, ManifestSHA256: fixture.manifestDigest,
		PayloadSHA256: fixture.payloadDigest, ResolvedAt: "2030-01-01T00:04:00Z",
		Reason: policy.ReasonNone,
	}
}

func installStartupFixture(t *testing.T, store *Store, fixture *startupFixture) {
	t.Helper()
	for _, kind := range []ArtifactKind{ArtifactManifest, ArtifactPayload, ArtifactReview, ArtifactApproval} {
		if err := store.InstallArtifact(fixture.generation, kind, fixture.artifacts[kind]); err != nil {
			t.Fatalf("install %s: %v", kind, err)
		}
	}
	if err := store.PersistPrepareReceipt(fixture.receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistCommitIntent(fixture.intent); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActivePointer(fixture.pointer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveGeneration(
		fixture.resolution,
		time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
}

func replaceGenerationArtifact(
	t *testing.T,
	storePath string,
	generation Generation,
	domain policy.Domain,
	kind ArtifactKind,
	content []byte,
) {
	t.Helper()
	name, err := generationFilename(domain, generation, kind)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storePath, generationsDirectory, name)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, GenerationFileMode); err != nil {
		t.Fatal(err)
	}
}

func snapshotStoreTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
