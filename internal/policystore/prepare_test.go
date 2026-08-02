package policystore

import (
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

func TestPrepareCandidateRevalidatesAndPersistsDomainReceipt(t *testing.T) {
	preparedAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			store, _ := newTestStore(t, domain)
			defer store.Close()
			fixture := newStartupFixture(t, domain, 1)
			installCandidateArtifacts(t, store, fixture)
			installed := candidateCompatibility(fixture)
			input := prepareCandidateInput(fixture)

			receipt, err := store.PrepareCandidate(input, installed, fixture.publicKey, preparedAt)
			if err != nil {
				t.Fatalf("PrepareCandidate() error: %v", err)
			}
			persisted, err := store.ReadPrepareReceipt(input.TransactionID)
			if err != nil {
				t.Fatalf("ReadPrepareReceipt() error: %v", err)
			}
			if !reflect.DeepEqual(persisted, receipt) || receipt.Domain != domain ||
				receipt.PreparedAt != preparedAt.Format(time.RFC3339Nano) {
				t.Fatalf("persisted receipt = %+v, returned = %+v", persisted, receipt)
			}
		})
	}
}

func TestPrepareCandidateRejectsIdentitySignatureAndCompatibilityBeforeReceipt(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)

	t.Run("identity mismatch", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installCandidateArtifacts(t, store, fixture)
		input := prepareCandidateInput(fixture)
		input.UserPayloadSHA256 = policy.SHA256Hex([]byte("wrong-user-payload"))
		if _, err := store.PrepareCandidate(
			input, candidateCompatibility(fixture), fixture.publicKey, validAt,
		); !errors.Is(err, ErrCandidateIdentity) {
			t.Fatalf("identity mismatch error = %v", err)
		}
		assertNoPrepareReceipt(t, store, input)
	})

	t.Run("invalid signature", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainUser)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainUser, 1)
		fixture.approval.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
		fixture.rebindApproval(t)
		installCandidateArtifacts(t, store, fixture)
		input := prepareCandidateInput(fixture)
		if _, err := store.PrepareCandidate(
			input, candidateCompatibility(fixture), fixture.publicKey, validAt,
		); !errors.Is(err, policyapproval.ErrApprovalSignature) {
			t.Fatalf("signature error = %v", err)
		}
		assertNoPrepareReceipt(t, store, input)
	})

	t.Run("static mismatch", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		fixture := newStartupFixture(t, policy.DomainRoot, 1)
		installCandidateArtifacts(t, store, fixture)
		installed := candidateCompatibility(fixture)
		installed.StaticSHA256 = policy.SHA256Hex([]byte("different-static"))
		input := prepareCandidateInput(fixture)
		if _, err := store.PrepareCandidate(
			input, installed, fixture.publicKey, validAt,
		); !errors.Is(err, policy.ErrRestartRequired) {
			t.Fatalf("compatibility error = %v", err)
		}
		assertNoPrepareReceipt(t, store, input)
	})
}

func installCandidateArtifacts(t *testing.T, store *Store, fixture *startupFixture) {
	t.Helper()
	for _, kind := range []ArtifactKind{ArtifactManifest, ArtifactPayload, ArtifactReview, ArtifactApproval} {
		if err := store.InstallArtifact(fixture.generation, kind, fixture.artifacts[kind]); err != nil {
			t.Fatalf("install %s: %v", kind, err)
		}
	}
}

func candidateCompatibility(fixture *startupFixture) policy.InstalledCompatibility {
	installed := fixture.installed
	installed.CurrentBundleGeneration = fixture.manifest.ParentBundleGeneration
	return installed
}

func prepareCandidateInput(fixture *startupFixture) PrepareCandidateInput {
	return PrepareCandidateInput{
		TransactionID: transactionOne, BundleGeneration: fixture.manifest.BundleGeneration,
		RootPolicyGeneration: fixture.manifest.Root.Generation,
		UserPolicyGeneration: fixture.manifest.User.Generation,
		ManifestSHA256:       fixture.manifestDigest,
		RootPayloadSHA256:    fixture.manifest.Root.PayloadSHA256,
		UserPayloadSHA256:    fixture.manifest.User.PayloadSHA256,
		ApprovalSHA256:       policy.SHA256Hex(fixture.artifacts[ArtifactApproval]),
	}
}

func assertNoPrepareReceipt(t *testing.T, store *Store, input PrepareCandidateInput) {
	t.Helper()
	if _, err := store.ReadPrepareReceipt(input.TransactionID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("ReadPrepareReceipt() error = %v, want %v", err, ErrRecordNotFound)
	}
}
