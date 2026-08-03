package actionlease

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
)

type ExecutionStore interface {
	ReadActionLeaseState(
		metadata.UUID,
	) (policy.ActionLease, *policy.ActionLeaseOutcome, error)
	ReadActionLeaseExecutionClaim(metadata.UUID) (policy.ActionLeaseExecutionClaim, error)
	PersistActionLeaseExecutionClaim(policy.ActionLeaseExecutionClaim) error
	PersistActionLeaseOutcome(policy.ActionLeaseOutcome) error
}

type CurrentAuthorization struct {
	Domain                 policy.Domain
	Capability             policy.Capability
	BundleGeneration       uint64
	DomainPolicyGeneration uint64
	ControlStateGeneration uint64
	Target                 string
	PlanSHA256             string
	BootID                 metadata.UUID
	MonotonicNS            int64
	ObservedAt             time.Time
}

type Guard struct {
	store     ExecutionStore
	actionID  metadata.UUID
	attemptID metadata.UUID
	claimMu   sync.Mutex
	claim     *policy.ActionLeaseExecutionClaim
}

var (
	ErrInvalidGuard    = errors.New("invalid action lease guard")
	ErrLeaseReplay     = errors.New("action lease replay")
	ErrLeaseExpired    = errors.New("action lease expired")
	ErrLeaseStale      = errors.New("action lease generation is stale")
	ErrBindingMismatch = errors.New("action lease binding mismatch")
	ErrLeaseClock      = errors.New("action lease clock anomaly")
)

func NewGuard(store ExecutionStore, actionID metadata.UUID) (*Guard, error) {
	return newGuard(store, actionID, rand.Reader)
}

func newGuard(store ExecutionStore, actionID metadata.UUID, random io.Reader) (*Guard, error) {
	if store == nil {
		return nil, ErrInvalidGuard
	}
	if _, err := metadata.ParseUUID(string(actionID)); err != nil {
		return nil, ErrInvalidGuard
	}
	attemptID, err := metadata.NewUUID(random)
	if err != nil {
		return nil, ErrInvalidGuard
	}
	return &Guard{store: store, actionID: actionID, attemptID: attemptID}, nil
}

// BeforeStep must be called immediately before each mutation step. It reloads
// durable lease state on every call and resolves invalid leases before return.
func (guard *Guard) BeforeStep(current CurrentAuthorization) error {
	_, err := guard.validate(current)
	return err
}

// Commit repeats the full check and durably consumes the lease.
func (guard *Guard) Commit(current CurrentAuthorization) error {
	lease, err := guard.validate(current)
	if err != nil {
		return err
	}
	outcome := newOutcome(
		lease,
		policy.LeaseCommitted,
		policy.LeaseOutcomeCompleted,
		current.ObservedAt,
	)
	return guard.persistOutcome(outcome)
}

func (guard *Guard) Abort(observedAt time.Time) error {
	if guard == nil || guard.store == nil || !validObservedAt(observedAt) {
		return ErrInvalidGuard
	}
	lease, outcome, err := guard.store.ReadActionLeaseState(guard.actionID)
	if err != nil {
		return err
	}
	if outcome != nil {
		return ErrLeaseReplay
	}
	return guard.persistOutcome(newOutcome(
		lease,
		policy.LeaseAborted,
		policy.LeaseOutcomeCanceled,
		observedAt,
	))
}

func (guard *Guard) validate(
	current CurrentAuthorization,
) (policy.ActionLease, error) {
	if guard == nil || guard.store == nil || current.Validate() != nil {
		return policy.ActionLease{}, ErrInvalidGuard
	}
	lease, outcome, err := guard.store.ReadActionLeaseState(guard.actionID)
	if err != nil {
		return policy.ActionLease{}, err
	}
	if outcome != nil {
		return policy.ActionLease{}, ErrLeaseReplay
	}
	status, reason, validationErr := classifyCurrent(lease, current)
	if validationErr == nil {
		if err := guard.ensureExecutionClaim(lease, current); err != nil {
			return policy.ActionLease{}, err
		}
		return lease, nil
	}
	if err := guard.persistOutcome(newOutcome(
		lease,
		status,
		reason,
		current.ObservedAt,
	)); err != nil {
		return policy.ActionLease{}, err
	}
	return policy.ActionLease{}, validationErr
}

func (guard *Guard) ensureExecutionClaim(
	lease policy.ActionLease,
	current CurrentAuthorization,
) error {
	guard.claimMu.Lock()
	if guard.claim == nil {
		claim := policy.ActionLeaseExecutionClaim{
			Schema:   policy.ActionLeaseExecutionSchema,
			ActionID: lease.ActionID, Domain: lease.Domain, Nonce: lease.Nonce,
			AttemptID: guard.attemptID, BootID: current.BootID,
			ClaimedAt: current.ObservedAt.UTC().Format(time.RFC3339Nano),
		}
		guard.claim = &claim
	}
	claim := *guard.claim
	guard.claimMu.Unlock()

	if err := guard.store.PersistActionLeaseExecutionClaim(claim); err == nil {
		return nil
	} else {
		_, outcome, stateErr := guard.store.ReadActionLeaseState(guard.actionID)
		if stateErr == nil && outcome != nil {
			return ErrLeaseReplay
		}
		existing, readErr := guard.store.ReadActionLeaseExecutionClaim(guard.actionID)
		if readErr == nil && existing.AttemptID != guard.attemptID {
			return ErrLeaseReplay
		}
		return err
	}
}

func (current CurrentAuthorization) Validate() error {
	if !current.Domain.Valid() || !current.Capability.Valid() ||
		current.BundleGeneration == 0 || current.DomainPolicyGeneration == 0 ||
		current.ControlStateGeneration == 0 || current.Target == "" ||
		current.PlanSHA256 == "" || current.MonotonicNS < 0 ||
		!validObservedAt(current.ObservedAt) {
		return ErrInvalidGuard
	}
	if _, err := metadata.ParseUUID(string(current.BootID)); err != nil {
		return ErrInvalidGuard
	}
	return nil
}

func classifyCurrent(
	lease policy.ActionLease,
	current CurrentAuthorization,
) (policy.LeaseStatus, policy.LeaseOutcomeReason, error) {
	clockErr := policyclock.ValidatePendingLease(lease, policyclock.LeaseSample{
		MonotonicNS: current.MonotonicNS,
		BootID:      current.BootID,
	})
	switch {
	case errors.Is(clockErr, policyclock.ErrLeaseExpired):
		return policy.LeaseExpired, policy.LeaseOutcomeTTLExpired, ErrLeaseExpired
	case errors.Is(clockErr, policyclock.ErrLeaseBootMismatch):
		return policy.LeaseExpired, policy.LeaseOutcomeBootMismatch, ErrLeaseExpired
	case errors.Is(clockErr, policyclock.ErrClockAnomaly):
		return policy.LeaseAborted, policy.LeaseOutcomeClockAnomaly, ErrLeaseClock
	case clockErr != nil:
		return policy.LeaseAborted, policy.LeaseOutcomeClockAnomaly, ErrInvalidGuard
	}
	if current.BundleGeneration != lease.BundleGeneration ||
		current.DomainPolicyGeneration != lease.DomainPolicyGeneration ||
		current.ControlStateGeneration != lease.ControlStateGeneration {
		return policy.LeaseAborted, policy.LeaseOutcomeStaleGeneration, ErrLeaseStale
	}
	if current.Domain != lease.Domain || current.Capability != lease.Capability ||
		current.Target != lease.Target || current.PlanSHA256 != lease.PlanSHA256 {
		return policy.LeaseAborted, policy.LeaseOutcomeBindingMismatch, ErrBindingMismatch
	}
	return policy.LeasePending, "", nil
}

func (guard *Guard) persistOutcome(outcome policy.ActionLeaseOutcome) error {
	if err := guard.store.PersistActionLeaseOutcome(outcome); err == nil {
		return nil
	} else {
		_, existing, readErr := guard.store.ReadActionLeaseState(guard.actionID)
		if readErr == nil && existing != nil {
			return ErrLeaseReplay
		}
		return err
	}
}

func newOutcome(
	lease policy.ActionLease,
	status policy.LeaseStatus,
	reason policy.LeaseOutcomeReason,
	observedAt time.Time,
) policy.ActionLeaseOutcome {
	return policy.ActionLeaseOutcome{
		Schema:   policy.ActionLeaseOutcomeSchema,
		ActionID: lease.ActionID, Domain: lease.Domain, Nonce: lease.Nonce,
		Status: status, Reason: reason,
		ResolvedAt: observedAt.UTC().Format(time.RFC3339Nano),
	}
}

func validObservedAt(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}
