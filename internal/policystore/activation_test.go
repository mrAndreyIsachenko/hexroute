package policystore

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestCommitCandidatePersistsIntentPointerAndActiveResolutionIdempotently(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			store, _ := newTestStore(t, domain)
			defer store.Close()
			fixture := newStartupFixture(t, domain, 1)
			installCandidateArtifacts(t, store, fixture)
			input := prepareCandidateInput(fixture)
			if _, err := store.PrepareCandidate(
				input, candidateCompatibility(fixture), fixture.publicKey, validAt,
			); err != nil {
				t.Fatal(err)
			}

			committed, err := store.CommitCandidate(input, validAt)
			if err != nil {
				t.Fatalf("CommitCandidate() error: %v", err)
			}
			pointer := committed.Pointer
			if pointer.Domain != domain || pointer.TransactionID != input.TransactionID ||
				pointer.BundleGeneration != input.BundleGeneration {
				t.Fatalf("active pointer = %+v", pointer)
			}
			installed := fixture.installed
			active, err := store.RevalidateActive(installed, fixture.publicKey, validAt)
			if err != nil || active.Generation.Bundle != input.BundleGeneration {
				t.Fatalf("RevalidateActive() = %+v, %v", active, err)
			}
			retryAt := validAt.Add(5 * time.Minute)
			retried, err := store.CommitCandidate(input, retryAt)
			if err != nil || retried.Pointer != pointer || retried.PolicySchema != committed.PolicySchema {
				t.Fatalf("idempotent CommitCandidate() = %+v, %v", retried, err)
			}
		})
	}
}

func TestAbortCandidatePersistsRedactedResolutionAndRemovesCandidate(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
	store, _ := newTestStore(t, policy.DomainUser)
	defer store.Close()
	fixture := newStartupFixture(t, policy.DomainUser, 1)
	installCandidateArtifacts(t, store, fixture)
	input := prepareCandidateInput(fixture)
	if _, err := store.PrepareCandidate(
		input, candidateCompatibility(fixture), fixture.publicKey, validAt,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortCandidate(input, validAt); err != nil {
		t.Fatalf("AbortCandidate() error: %v", err)
	}
	if _, err := store.ReadPrepareReceipt(input.TransactionID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("prepare receipt error = %v", err)
	}
	if _, err := store.ReadArtifact(fixture.generation, ArtifactPayload); !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("payload error = %v", err)
	}
	index, err := store.ReadAuditIndex()
	if err != nil || len(index.Entries) != 1 ||
		index.Entries[0].Reason != policy.ReasonOperatorAborted {
		t.Fatalf("audit index = %+v, %v", index, err)
	}
}

func TestCommitCandidateAdvancesAnOlderActivePointer(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	oldIntent := syntheticCommitIntent(t, transactionTwo, 6)
	if err := store.PersistPrepareReceipt(syntheticPrepareReceipt(t, oldIntent, policy.DomainRoot)); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistCommitIntent(oldIntent); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActivePointer(syntheticActivePointer(t, oldIntent, policy.DomainRoot)); err != nil {
		t.Fatal(err)
	}

	fixture := newStartupFixture(t, policy.DomainRoot, 1)
	installCandidateArtifacts(t, store, fixture)
	input := prepareCandidateInput(fixture)
	if _, err := store.PrepareCandidate(
		input, candidateCompatibility(fixture), fixture.publicKey, validAt,
	); err != nil {
		t.Fatal(err)
	}
	committed, err := store.CommitCandidate(input, validAt)
	if err != nil {
		t.Fatalf("CommitCandidate() error: %v", err)
	}
	if committed.Pointer.BundleGeneration != 7 ||
		committed.Pointer.TransactionID != transactionOne {
		t.Fatalf("advanced pointer = %+v", committed.Pointer)
	}
}

func TestCommitAndAbortRequireMatchingPreparedIdentity(t *testing.T) {
	validAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	fixture := newStartupFixture(t, policy.DomainRoot, 1)
	installCandidateArtifacts(t, store, fixture)
	input := prepareCandidateInput(fixture)
	if _, err := store.PrepareCandidate(
		input, candidateCompatibility(fixture), fixture.publicKey, validAt,
	); err != nil {
		t.Fatal(err)
	}
	mismatch := input
	mismatch.UserPayloadSHA256 = policy.SHA256Hex([]byte("different-user"))
	if _, err := store.CommitCandidate(mismatch, validAt); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("commit mismatch error = %v", err)
	}
	if err := store.AbortCandidate(mismatch, validAt); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("abort mismatch error = %v", err)
	}
}
