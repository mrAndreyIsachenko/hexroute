package policystore

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

const actionNonceClaimSchema = "hexroute.action-nonce-claim.v1"

var ErrActionLeaseResolved = errors.New("action lease already has a durable outcome")

type actionNonceClaim struct {
	Schema    string        `json:"schema"`
	Domain    policy.Domain `json:"domain"`
	ActionID  metadata.UUID `json:"action_id"`
	Nonce     metadata.UUID `json:"nonce"`
	ClaimedAt string        `json:"claimed_at"`
}

func (claim actionNonceClaim) Validate() error {
	if claim.Schema != actionNonceClaimSchema || !claim.Domain.Valid() ||
		!validTransactionID(claim.ActionID) || !validTransactionID(claim.Nonce) ||
		!validCanonicalUTC(claim.ClaimedAt) {
		return ErrInvalidRecord
	}
	return nil
}

func (store *Store) PersistActionLease(lease policy.ActionLease) error {
	if lease.Validate() != nil || lease.Status != policy.LeasePending ||
		storeDomain(store) != lease.Domain {
		return ErrInvalidRecord
	}
	claim := actionNonceClaim{
		Schema: actionNonceClaimSchema, Domain: lease.Domain,
		ActionID: lease.ActionID, Nonce: lease.Nonce, ClaimedAt: lease.IssuedAt,
	}
	claimEncoded, err := marshalRecord(claim)
	if err != nil {
		return err
	}
	leaseEncoded, err := marshalRecord(lease)
	if err != nil {
		return err
	}
	claimName, err := transactionRecordFilename(recordActionNonce, lease.Nonce)
	if err != nil {
		return err
	}
	leaseName, err := transactionRecordFilename(recordActionLease, lease.ActionID)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	outcomeName, _ := transactionRecordFilename(recordActionOutcome, lease.ActionID)
	if _, err := store.readRecordLocked(outcomeName); err == nil {
		return ErrActionLeaseResolved
	} else if !errors.Is(err, ErrRecordNotFound) {
		return err
	}
	pointerEncoded, err := store.readRecordLocked(activePointerFilename)
	if err != nil {
		return err
	}
	pointer, err := decodeRecord[ActivePointer](pointerEncoded)
	if err != nil || pointer.Validate() != nil || pointer.Domain != lease.Domain ||
		pointer.ConfirmedAt == "" || pointer.BundleGeneration != lease.BundleGeneration ||
		pointer.PolicyGeneration != lease.DomainPolicyGeneration {
		return ErrRecordConflict
	}
	// Claiming the nonce first makes an interrupted write fail closed: an
	// orphaned claim can deny reuse but can never authorize an action.
	if err := store.persistImmutableRecordLocked(recordActionNonce, claimName, claimEncoded); err != nil {
		return err
	}
	return store.persistImmutableRecordLocked(recordActionLease, leaseName, leaseEncoded)
}

func (store *Store) ReadActionLease(actionID metadata.UUID) (policy.ActionLease, error) {
	lease, _, err := store.ReadActionLeaseState(actionID)
	return lease, err
}

func (store *Store) PersistActionLeaseExecutionClaim(
	claim policy.ActionLeaseExecutionClaim,
) error {
	if claim.Validate() != nil || storeDomain(store) != claim.Domain {
		return ErrInvalidRecord
	}
	leaseName, err := transactionRecordFilename(recordActionLease, claim.ActionID)
	if err != nil {
		return err
	}
	executionName, err := transactionRecordFilename(recordActionExecution, claim.ActionID)
	if err != nil {
		return err
	}
	encoded, err := marshalRecord(claim)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	lease, err := store.readActionLeaseLocked(leaseName, claim.ActionID)
	if err != nil {
		return err
	}
	if !executionMatchesLease(claim, lease) {
		return ErrRecordConflict
	}
	outcomeName, _ := transactionRecordFilename(recordActionOutcome, claim.ActionID)
	if _, err := store.readRecordLocked(outcomeName); err == nil {
		return ErrActionLeaseResolved
	} else if !errors.Is(err, ErrRecordNotFound) {
		return err
	}
	pointerEncoded, err := store.readRecordLocked(activePointerFilename)
	if err != nil {
		return err
	}
	pointer, err := decodeRecord[ActivePointer](pointerEncoded)
	if err != nil || pointer.Validate() != nil || pointer.ConfirmedAt == "" ||
		pointer.Domain != lease.Domain || pointer.BundleGeneration != lease.BundleGeneration ||
		pointer.PolicyGeneration != lease.DomainPolicyGeneration {
		return ErrRecordConflict
	}
	if existingEncoded, err := store.readRecordLocked(executionName); err == nil {
		existing, decodeErr := decodeRecord[policy.ActionLeaseExecutionClaim](existingEncoded)
		if decodeErr != nil || existing.Validate() != nil || existing != claim {
			return ErrRecordConflict
		}
		return nil
	} else if !errors.Is(err, ErrRecordNotFound) {
		return err
	}
	return store.persistImmutableRecordLocked(recordActionExecution, executionName, encoded)
}

func (store *Store) ReadActionLeaseExecutionClaim(
	actionID metadata.UUID,
) (policy.ActionLeaseExecutionClaim, error) {
	leaseName, err := transactionRecordFilename(recordActionLease, actionID)
	if err != nil {
		return policy.ActionLeaseExecutionClaim{}, err
	}
	executionName, err := transactionRecordFilename(recordActionExecution, actionID)
	if err != nil {
		return policy.ActionLeaseExecutionClaim{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return policy.ActionLeaseExecutionClaim{}, err
	}
	lease, err := store.readActionLeaseLocked(leaseName, actionID)
	if err != nil {
		return policy.ActionLeaseExecutionClaim{}, err
	}
	encoded, err := store.readRecordLocked(executionName)
	if err != nil {
		return policy.ActionLeaseExecutionClaim{}, err
	}
	claim, err := decodeRecord[policy.ActionLeaseExecutionClaim](encoded)
	if err != nil || claim.Validate() != nil || !executionMatchesLease(claim, lease) {
		return policy.ActionLeaseExecutionClaim{}, ErrRecordConflict
	}
	return claim, nil
}

func (store *Store) ReadActionLeaseState(
	actionID metadata.UUID,
) (policy.ActionLease, *policy.ActionLeaseOutcome, error) {
	leaseName, err := transactionRecordFilename(recordActionLease, actionID)
	if err != nil {
		return policy.ActionLease{}, nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return policy.ActionLease{}, nil, err
	}
	lease, err := store.readActionLeaseLocked(leaseName, actionID)
	if err != nil {
		return policy.ActionLease{}, nil, err
	}
	outcomeName, _ := transactionRecordFilename(recordActionOutcome, actionID)
	outcomeEncoded, err := store.readRecordLocked(outcomeName)
	if errors.Is(err, ErrRecordNotFound) {
		return lease, nil, nil
	}
	if err != nil {
		return policy.ActionLease{}, nil, err
	}
	outcome, err := decodeRecord[policy.ActionLeaseOutcome](outcomeEncoded)
	if err != nil || outcome.Validate() != nil || !outcomeMatchesLease(outcome, lease) {
		return policy.ActionLease{}, nil, ErrRecordConflict
	}
	return lease, &outcome, nil
}

func (store *Store) PersistActionLeaseOutcome(outcome policy.ActionLeaseOutcome) error {
	if outcome.Validate() != nil || storeDomain(store) != outcome.Domain {
		return ErrInvalidRecord
	}
	leaseName, err := transactionRecordFilename(recordActionLease, outcome.ActionID)
	if err != nil {
		return err
	}
	outcomeName, err := transactionRecordFilename(recordActionOutcome, outcome.ActionID)
	if err != nil {
		return err
	}
	encoded, err := marshalRecord(outcome)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	lease, err := store.readActionLeaseLocked(leaseName, outcome.ActionID)
	if err != nil {
		return err
	}
	if !outcomeMatchesLease(outcome, lease) {
		return ErrRecordConflict
	}
	if _, err := store.readRecordLocked(outcomeName); err == nil {
		return ErrActionLeaseResolved
	} else if !errors.Is(err, ErrRecordNotFound) {
		return err
	}
	if outcome.Status == policy.LeaseCommitted {
		executionName, _ := transactionRecordFilename(recordActionExecution, outcome.ActionID)
		executionEncoded, err := store.readRecordLocked(executionName)
		if err != nil {
			return err
		}
		execution, err := decodeRecord[policy.ActionLeaseExecutionClaim](executionEncoded)
		if err != nil || execution.Validate() != nil || !executionMatchesLease(execution, lease) {
			return ErrRecordConflict
		}
		pointerEncoded, err := store.readRecordLocked(activePointerFilename)
		if err != nil {
			return err
		}
		pointer, err := decodeRecord[ActivePointer](pointerEncoded)
		if err != nil || pointer.Validate() != nil || pointer.ConfirmedAt == "" ||
			pointer.Domain != lease.Domain || pointer.BundleGeneration != lease.BundleGeneration ||
			pointer.PolicyGeneration != lease.DomainPolicyGeneration {
			return ErrRecordConflict
		}
	}
	if err := store.persistRecordLocked(recordActionOutcome, outcomeName, encoded, false); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return ErrActionLeaseResolved
		}
		return err
	}
	return nil
}

func (store *Store) readActionLeaseLocked(
	leaseName string,
	actionID metadata.UUID,
) (policy.ActionLease, error) {
	encoded, err := store.readRecordLocked(leaseName)
	if err != nil {
		return policy.ActionLease{}, err
	}
	lease, err := decodeRecord[policy.ActionLease](encoded)
	if err != nil || lease.Validate() != nil || lease.Status != policy.LeasePending ||
		lease.Domain != store.domain || lease.ActionID != actionID {
		return policy.ActionLease{}, ErrInvalidRecord
	}
	claimName, _ := transactionRecordFilename(recordActionNonce, lease.Nonce)
	claimEncoded, err := store.readRecordLocked(claimName)
	if err != nil {
		return policy.ActionLease{}, err
	}
	claim, err := decodeRecord[actionNonceClaim](claimEncoded)
	if err != nil || claim.Validate() != nil || claim.Domain != lease.Domain ||
		claim.ActionID != lease.ActionID || claim.Nonce != lease.Nonce ||
		claim.ClaimedAt != lease.IssuedAt {
		return policy.ActionLease{}, ErrRecordConflict
	}
	return lease, nil
}

func outcomeMatchesLease(
	outcome policy.ActionLeaseOutcome,
	lease policy.ActionLease,
) bool {
	resolvedAt, resolvedErr := time.Parse(time.RFC3339Nano, outcome.ResolvedAt)
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, lease.IssuedAt)
	return resolvedErr == nil && issuedErr == nil && !resolvedAt.Before(issuedAt) &&
		outcome.ActionID == lease.ActionID && outcome.Domain == lease.Domain &&
		outcome.Nonce == lease.Nonce
}

func executionMatchesLease(
	claim policy.ActionLeaseExecutionClaim,
	lease policy.ActionLease,
) bool {
	claimedAt, claimedErr := time.Parse(time.RFC3339Nano, claim.ClaimedAt)
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, lease.IssuedAt)
	return claimedErr == nil && issuedErr == nil && !claimedAt.Before(issuedAt) &&
		claim.ActionID == lease.ActionID && claim.Domain == lease.Domain &&
		claim.Nonce == lease.Nonce && claim.BootID == lease.BootID
}
