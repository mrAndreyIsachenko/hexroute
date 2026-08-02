package policystore

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const actionNonceClaimSchema = "hexroute.action-nonce-claim.v1"

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
	leaseName, err := transactionRecordFilename(recordActionLease, actionID)
	if err != nil {
		return policy.ActionLease{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return policy.ActionLease{}, err
	}
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
