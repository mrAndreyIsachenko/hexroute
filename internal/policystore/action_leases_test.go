package policystore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	actionOne  metadata.UUID = "323e4567-e89b-42d3-a456-426614174000"
	actionTwo  metadata.UUID = "423e4567-e89b-42d3-a456-426614174000"
	nonceOne   metadata.UUID = "523e4567-e89b-42d3-a456-426614174000"
	nonceTwo   metadata.UUID = "623e4567-e89b-42d3-a456-426614174000"
	attemptOne metadata.UUID = "723e4567-e89b-42d3-a456-426614174000"
	attemptTwo metadata.UUID = "823e4567-e89b-42d3-a456-426614174000"
)

func TestActionLeaseIsDurableIdempotentAndDomainBound(t *testing.T) {
	store, path := newTestStore(t, policy.DomainUser)
	lease := installActionLeaseActivePolicy(t, store, policy.DomainUser)
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatalf("idempotent persist: %v", err)
	}
	if _, err := store.ApplyRetention(time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("retention with pending lease: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openStoreAt(path, policy.DomainUser, currentUID(), currentGID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.ReadActionLease(lease.ActionID)
	if err != nil || !reflect.DeepEqual(recovered, lease) {
		t.Fatalf("recovered=%+v error=%v", recovered, err)
	}

	wrongDomain := lease
	wrongDomain.Domain = policy.DomainRoot
	wrongDomain.ActionID = actionTwo
	if err := reopened.PersistActionLease(wrongDomain); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("wrong-domain error = %v", err)
	}
}

func TestActionLeaseRejectsNonceAndActionIdentityReuse(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	lease := installActionLeaseActivePolicy(t, store, policy.DomainRoot)
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatal(err)
	}

	reusedNonce := lease
	reusedNonce.ActionID = actionTwo
	if err := store.PersistActionLease(reusedNonce); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("nonce reuse error = %v", err)
	}
	changedAction := lease
	changedAction.Target = "different-synthetic-target"
	if err := store.PersistActionLease(changedAction); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("action identity reuse error = %v", err)
	}
	staleGeneration := lease
	staleGeneration.ActionID = actionTwo
	staleGeneration.Nonce = nonceTwo
	staleGeneration.BundleGeneration++
	if err := store.PersistActionLease(staleGeneration); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("stale generation error = %v", err)
	}
}

func TestActionLeaseRequiresConfirmedActivePolicy(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	defer store.Close()
	lease := syntheticStoredActionLease(policy.DomainUser)
	if err := store.PersistActionLease(lease); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("lease without active policy error = %v", err)
	}
	fixture := newStartupFixture(t, policy.DomainUser, 1)
	installStartupFixture(t, store, fixture)
	lease.BundleGeneration = fixture.generation.Bundle
	lease.DomainPolicyGeneration = fixture.generation.Policy
	if err := store.PersistActionLease(lease); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("lease with unconfirmed policy error = %v", err)
	}
}

func TestInterruptedActionLeaseWriteLeavesNonceClaimFailClosed(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	defer store.Close()
	lease := installActionLeaseActivePolicy(t, store, policy.DomainUser)
	store.persistenceFault = func(operation recordOperation, boundary persistenceBoundary) error {
		if operation == recordActionLease && boundary == boundaryBeforeRename {
			return ErrPersistenceInterrupted
		}
		return nil
	}
	if err := store.PersistActionLease(lease); !errors.Is(err, ErrPersistenceInterrupted) {
		t.Fatalf("interrupted persist error = %v", err)
	}
	if _, err := store.ReadActionLease(lease.ActionID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("partial lease read error = %v", err)
	}
	reusedNonce := lease
	reusedNonce.ActionID = actionTwo
	if err := store.PersistActionLease(reusedNonce); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("orphaned nonce reuse error = %v", err)
	}
	store.persistenceFault = nil
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatalf("idempotent recovery: %v", err)
	}
	if _, err := store.ReadActionLease(lease.ActionID); err != nil {
		t.Fatalf("recovered lease: %v", err)
	}
}

func TestActionLeaseOutcomeIsDurableAndOneTime(t *testing.T) {
	store, path := newTestStore(t, policy.DomainUser)
	lease := installActionLeaseActivePolicy(t, store, policy.DomainUser)
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatal(err)
	}
	outcome := syntheticActionLeaseOutcome(lease, policy.LeaseCommitted, policy.LeaseOutcomeCompleted)
	if err := store.PersistActionLeaseOutcome(outcome); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("commit without execution claim error = %v", err)
	}
	claim := syntheticActionLeaseExecutionClaim(lease)
	if err := store.PersistActionLeaseExecutionClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActionLeaseOutcome(outcome); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActionLeaseOutcome(outcome); !errors.Is(err, ErrActionLeaseResolved) {
		t.Fatalf("outcome replay error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openStoreAt(path, policy.DomainUser, currentUID(), currentGID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recoveredLease, recoveredOutcome, err := reopened.ReadActionLeaseState(lease.ActionID)
	if err != nil || !reflect.DeepEqual(recoveredLease, lease) || recoveredOutcome == nil ||
		!reflect.DeepEqual(*recoveredOutcome, outcome) {
		t.Fatalf("lease=%+v outcome=%+v error=%v", recoveredLease, recoveredOutcome, err)
	}
	if _, err := reopened.ApplyRetention(time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("retention with outcome: %v", err)
	}
}

func TestActionLeaseExecutionClaimIsDurableAndExclusive(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	lease := installActionLeaseActivePolicy(t, store, policy.DomainRoot)
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatal(err)
	}
	claim := syntheticActionLeaseExecutionClaim(lease)
	if err := store.PersistActionLeaseExecutionClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistActionLeaseExecutionClaim(claim); err != nil {
		t.Fatalf("idempotent claim: %v", err)
	}
	conflicting := claim
	conflicting.AttemptID = attemptTwo
	if err := store.PersistActionLeaseExecutionClaim(conflicting); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("conflicting attempt error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openStoreAt(path, policy.DomainRoot, currentUID(), currentGID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.ReadActionLeaseExecutionClaim(lease.ActionID)
	if err != nil || recovered != claim {
		t.Fatalf("recovered=%+v error=%v", recovered, err)
	}
	if _, err := reopened.ApplyRetention(time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("retention with execution claim: %v", err)
	}
}

func TestActionLeaseOutcomeRejectsWrongNonceAndAcceptsBoundedAbort(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	lease := installActionLeaseActivePolicy(t, store, policy.DomainRoot)
	if err := store.PersistActionLease(lease); err != nil {
		t.Fatal(err)
	}
	wrongNonce := syntheticActionLeaseOutcome(lease, policy.LeaseAborted, policy.LeaseOutcomeCanceled)
	wrongNonce.Nonce = nonceTwo
	if err := store.PersistActionLeaseOutcome(wrongNonce); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("wrong nonce error = %v", err)
	}

	aborted := syntheticActionLeaseOutcome(lease, policy.LeaseAborted, policy.LeaseOutcomeStaleGeneration)
	if err := store.PersistActionLeaseOutcome(aborted); err != nil {
		t.Fatalf("stale abort outcome: %v", err)
	}
}

func syntheticStoredActionLease(domain policy.Domain) policy.ActionLease {
	return policy.ActionLease{
		Schema: policy.ActionLeaseSchema, ActionID: actionOne, Domain: domain,
		Capability:       policy.CapabilityOperatorResume,
		BundleGeneration: 9, DomainPolicyGeneration: 5, ControlStateGeneration: 17,
		Target: "synthetic-target", PlanSHA256: policy.SHA256Hex([]byte("synthetic-plan")),
		IssuedAt: "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-01T00:00:30Z",
		IssuedMonotonicNS: int64(10 * time.Second), ExpiresMonotonicNS: int64(40 * time.Second),
		BootID: "123e4567-e89b-42d3-a456-426614174000",
		Nonce:  nonceOne, Status: policy.LeasePending,
	}
}

func installActionLeaseActivePolicy(
	t *testing.T,
	store *Store,
	domain policy.Domain,
) policy.ActionLease {
	t.Helper()
	fixture := newStartupFixture(t, domain, 1)
	fixture.pointer.ConfirmedAt = "2030-01-01T00:05:00Z"
	installStartupFixture(t, store, fixture)
	lease := syntheticStoredActionLease(domain)
	lease.BundleGeneration = fixture.generation.Bundle
	lease.DomainPolicyGeneration = fixture.generation.Policy
	return lease
}

func syntheticActionLeaseOutcome(
	lease policy.ActionLease,
	status policy.LeaseStatus,
	reason policy.LeaseOutcomeReason,
) policy.ActionLeaseOutcome {
	return policy.ActionLeaseOutcome{
		Schema:   policy.ActionLeaseOutcomeSchema,
		ActionID: lease.ActionID, Domain: lease.Domain, Nonce: lease.Nonce,
		Status: status, Reason: reason, ResolvedAt: "2030-01-01T00:00:20Z",
	}
}

func syntheticActionLeaseExecutionClaim(
	lease policy.ActionLease,
) policy.ActionLeaseExecutionClaim {
	return policy.ActionLeaseExecutionClaim{
		Schema:   policy.ActionLeaseExecutionSchema,
		ActionID: lease.ActionID, Domain: lease.Domain, Nonce: lease.Nonce,
		AttemptID: attemptOne, BootID: lease.BootID,
		ClaimedAt: "2030-01-01T00:00:10Z",
	}
}
