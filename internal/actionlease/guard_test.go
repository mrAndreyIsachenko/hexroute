package actionlease

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type guardStore struct {
	mu      sync.Mutex
	lease   policy.ActionLease
	claim   *policy.ActionLeaseExecutionClaim
	outcome *policy.ActionLeaseOutcome
}

func (store *guardStore) ReadActionLeaseExecutionClaim(
	actionID metadata.UUID,
) (policy.ActionLeaseExecutionClaim, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actionID != store.lease.ActionID || store.claim == nil {
		return policy.ActionLeaseExecutionClaim{}, errors.New("not found")
	}
	return *store.claim, nil
}

func (store *guardStore) PersistActionLeaseExecutionClaim(
	claim policy.ActionLeaseExecutionClaim,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.outcome != nil {
		return errors.New("resolved")
	}
	if store.claim != nil {
		if *store.claim != claim {
			return errors.New("execution claimed")
		}
		return nil
	}
	store.claim = &claim
	return nil
}

func (store *guardStore) ReadActionLeaseState(
	actionID metadata.UUID,
) (policy.ActionLease, *policy.ActionLeaseOutcome, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actionID != store.lease.ActionID {
		return policy.ActionLease{}, nil, errors.New("not found")
	}
	if store.outcome == nil {
		return store.lease, nil, nil
	}
	outcome := *store.outcome
	return store.lease, &outcome, nil
}

func (store *guardStore) PersistActionLeaseOutcome(outcome policy.ActionLeaseOutcome) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.outcome != nil {
		return errors.New("resolved")
	}
	if outcome.Status == policy.LeaseCommitted && store.claim == nil {
		return errors.New("execution not claimed")
	}
	store.outcome = &outcome
	return nil
}

func TestGuardRechecksEveryStepAndConsumesLeaseAtCommit(t *testing.T) {
	store := &guardStore{lease: guardLease()}
	guard, err := NewGuard(store, store.lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	current := guardCurrent(store.lease)
	if err := guard.BeforeStep(current); err != nil {
		t.Fatalf("first step: %v", err)
	}
	if err := guard.BeforeStep(current); err != nil {
		t.Fatalf("second step: %v", err)
	}
	if store.claim == nil {
		t.Fatal("execution claim was not persisted")
	}
	if err := guard.Commit(current); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if store.outcome == nil || store.outcome.Status != policy.LeaseCommitted ||
		store.outcome.Reason != policy.LeaseOutcomeCompleted {
		t.Fatalf("outcome = %+v", store.outcome)
	}
	if err := guard.BeforeStep(current); !errors.Is(err, ErrLeaseReplay) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestSecondGuardCannotResumeClaimedLease(t *testing.T) {
	lease := guardLease()
	store := &guardStore{lease: lease}
	first, err := newGuard(store, lease.ActionID, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := newGuard(store, lease.ActionID, bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	current := guardCurrent(lease)
	if err := first.BeforeStep(current); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := second.BeforeStep(current); !errors.Is(err, ErrLeaseReplay) {
		t.Fatalf("second guard error = %v", err)
	}
	if store.outcome != nil {
		t.Fatalf("claim conflict must not overwrite outcome: %+v", store.outcome)
	}
}

func TestGuardStopsWhenGenerationChangesBetweenSteps(t *testing.T) {
	lease := guardLease()
	store := &guardStore{lease: lease}
	guard, err := NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	current := guardCurrent(lease)
	if err := guard.BeforeStep(current); err != nil {
		t.Fatalf("first step: %v", err)
	}
	current.ControlStateGeneration++
	current.ObservedAt = current.ObservedAt.Add(time.Second)
	current.MonotonicNS += int64(time.Second)
	if err := guard.BeforeStep(current); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("second step error = %v", err)
	}
	if store.outcome == nil || store.outcome.Status != policy.LeaseAborted ||
		store.outcome.Reason != policy.LeaseOutcomeStaleGeneration {
		t.Fatalf("outcome = %+v", store.outcome)
	}
	if err := guard.Commit(current); !errors.Is(err, ErrLeaseReplay) {
		t.Fatalf("commit after stale step error = %v", err)
	}
}

func TestGuardRechecksGenerationImmediatelyBeforeCommit(t *testing.T) {
	lease := guardLease()
	store := &guardStore{lease: lease}
	guard, err := NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	current := guardCurrent(lease)
	if err := guard.BeforeStep(current); err != nil {
		t.Fatalf("step: %v", err)
	}
	current.BundleGeneration++
	current.ObservedAt = current.ObservedAt.Add(time.Second)
	current.MonotonicNS += int64(time.Second)
	if err := guard.Commit(current); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("commit error = %v", err)
	}
	if store.outcome == nil || store.outcome.Status != policy.LeaseAborted ||
		store.outcome.Reason != policy.LeaseOutcomeStaleGeneration {
		t.Fatalf("outcome = %+v", store.outcome)
	}
}

func TestGuardDurablyClassifiesInvalidLeaseBeforeStep(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*CurrentAuthorization)
		wantErr    error
		wantStatus policy.LeaseStatus
		wantReason policy.LeaseOutcomeReason
	}{
		{
			name: "expired", mutate: func(value *CurrentAuthorization) {
				value.MonotonicNS = int64(40 * time.Second)
			},
			wantErr: ErrLeaseExpired, wantStatus: policy.LeaseExpired,
			wantReason: policy.LeaseOutcomeTTLExpired,
		},
		{
			name: "boot mismatch", mutate: func(value *CurrentAuthorization) {
				value.BootID = "223e4567-e89b-42d3-a456-426614174000"
			},
			wantErr: ErrLeaseExpired, wantStatus: policy.LeaseExpired,
			wantReason: policy.LeaseOutcomeBootMismatch,
		},
		{
			name: "stale generation", mutate: func(value *CurrentAuthorization) {
				value.ControlStateGeneration++
			},
			wantErr: ErrLeaseStale, wantStatus: policy.LeaseAborted,
			wantReason: policy.LeaseOutcomeStaleGeneration,
		},
		{
			name: "plan mismatch", mutate: func(value *CurrentAuthorization) {
				value.PlanSHA256 = policy.SHA256Hex([]byte("different-plan"))
			},
			wantErr: ErrBindingMismatch, wantStatus: policy.LeaseAborted,
			wantReason: policy.LeaseOutcomeBindingMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lease := guardLease()
			store := &guardStore{lease: lease}
			guard, err := NewGuard(store, lease.ActionID)
			if err != nil {
				t.Fatal(err)
			}
			current := guardCurrent(lease)
			test.mutate(&current)
			if err := guard.BeforeStep(current); !errors.Is(err, test.wantErr) {
				t.Fatalf("BeforeStep() error = %v", err)
			}
			if store.outcome == nil || store.outcome.Status != test.wantStatus ||
				store.outcome.Reason != test.wantReason {
				t.Fatalf("outcome = %+v", store.outcome)
			}
			if err := guard.Commit(current); !errors.Is(err, ErrLeaseReplay) {
				t.Fatalf("resolved commit error = %v", err)
			}
		})
	}
}

func TestConcurrentCommitConsumesLeaseExactlyOnce(t *testing.T) {
	lease := guardLease()
	store := &guardStore{lease: lease}
	guard, err := NewGuard(store, lease.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	current := guardCurrent(lease)
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- guard.Commit(current)
		}()
	}
	wait.Wait()
	close(results)
	succeeded := 0
	replayed := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrLeaseReplay):
			replayed++
		default:
			t.Fatalf("commit error = %v", err)
		}
	}
	if succeeded != 1 || replayed != 1 {
		t.Fatalf("succeeded=%d replayed=%d", succeeded, replayed)
	}
}

func guardLease() policy.ActionLease {
	return policy.ActionLease{
		Schema:   policy.ActionLeaseSchema,
		ActionID: "323e4567-e89b-42d3-a456-426614174000",
		Domain:   policy.DomainUser, Capability: policy.CapabilityOperatorResume,
		BundleGeneration: 9, DomainPolicyGeneration: 5, ControlStateGeneration: 17,
		Target: "synthetic-target", PlanSHA256: policy.SHA256Hex([]byte("synthetic-plan")),
		IssuedAt: "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-01T00:00:30Z",
		IssuedMonotonicNS:  int64(10 * time.Second),
		ExpiresMonotonicNS: int64(40 * time.Second),
		BootID:             "123e4567-e89b-42d3-a456-426614174000",
		Nonce:              "523e4567-e89b-42d3-a456-426614174000", Status: policy.LeasePending,
	}
}

func guardCurrent(lease policy.ActionLease) CurrentAuthorization {
	return CurrentAuthorization{
		Domain: lease.Domain, Capability: lease.Capability,
		BundleGeneration:       lease.BundleGeneration,
		DomainPolicyGeneration: lease.DomainPolicyGeneration,
		ControlStateGeneration: lease.ControlStateGeneration,
		Target:                 lease.Target, PlanSHA256: lease.PlanSHA256,
		BootID: lease.BootID, MonotonicNS: int64(20 * time.Second),
		ObservedAt: time.Date(2030, time.January, 1, 0, 0, 10, 0, time.UTC),
	}
}
