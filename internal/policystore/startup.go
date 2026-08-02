package policystore

import (
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

type RevalidatedActive struct {
	Domain         policy.Domain
	Generation     Generation
	ManifestSHA256 string
	PayloadSHA256  string
	ActivatedAt    string
	ConfirmedAt    string
	Manifest       policy.Manifest
	Payload        policy.DomainPayload
	Intent         CommitIntent
}

var ErrActivePointerConsistency = errors.New("active policy evidence is inconsistent")

func (store *Store) RevalidateActive(
	installed policy.InstalledCompatibility,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
) (RevalidatedActive, error) {
	return store.revalidateActive(installed, pinnedPublicKey, now, false)
}

// RecoverActive derives only the dynamic current-generation fields from fully
// revalidated active evidence. Static bounds, compiler trust and the signer key
// remain pinned by installed configuration.
func (store *Store) RecoverActive(
	installed policy.InstalledCompatibility,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
) (RevalidatedActive, error) {
	return store.revalidateActive(installed, pinnedPublicKey, now, true)
}

func (store *Store) revalidateActive(
	installed policy.InstalledCompatibility,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
	recoverDynamic bool,
) (RevalidatedActive, error) {
	if installed.Validate() != nil || installed.Domain != storeDomain(store) ||
		len(pinnedPublicKey) != ed25519.PublicKeySize || now.IsZero() {
		return RevalidatedActive{}, policy.ErrInvalidCompatibility
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return RevalidatedActive{}, err
	}
	pointer, err := store.readStartupPointerLocked()
	if err != nil {
		return RevalidatedActive{}, err
	}
	intent, resolutionMissing, err := store.validateStartupRecordsLocked(pointer)
	if err != nil {
		return RevalidatedActive{}, err
	}
	if resolutionMissing && !recoverDynamic {
		return RevalidatedActive{}, ErrActivePointerConsistency
	}

	generation := Generation{Bundle: pointer.BundleGeneration, Policy: pointer.PolicyGeneration}
	manifestEncoded, err := store.readArtifactLocked(generation, ArtifactManifest)
	if err != nil {
		return RevalidatedActive{}, err
	}
	payloadEncoded, err := store.readArtifactLocked(generation, ArtifactPayload)
	if err != nil {
		return RevalidatedActive{}, err
	}
	reviewEncoded, err := store.readArtifactLocked(generation, ArtifactReview)
	if err != nil {
		return RevalidatedActive{}, err
	}
	approvalEncoded, err := store.readArtifactLocked(generation, ArtifactApproval)
	if err != nil {
		return RevalidatedActive{}, err
	}

	manifest, manifestDigest, err := policy.DecodeManifestArtifact(manifestEncoded)
	if err != nil {
		return RevalidatedActive{}, err
	}
	payload, payloadDigest, err := policy.DecodeDomainPayloadArtifact(payloadEncoded)
	if err != nil {
		return RevalidatedActive{}, err
	}
	review, err := policyapproval.DecodeReviewArtifact(reviewEncoded)
	if err != nil {
		return RevalidatedActive{}, err
	}
	approval, err := policyapproval.DecodeApprovalArtifact(approvalEncoded)
	if err != nil {
		return RevalidatedActive{}, err
	}
	approvalDigest, err := approvalSHA256(approval)
	if err != nil || manifestDigest != pointer.ManifestSHA256 ||
		payloadDigest != pointer.PayloadSHA256 || approvalDigest != pointer.ApprovalSHA256 ||
		approval != pointer.Approval || !activePointerTimeConsistent(pointer, manifest, now.UTC()) {
		return RevalidatedActive{}, ErrActivePointerConsistency
	}
	if err := policyapproval.VerifyDomainCandidate(
		manifest,
		manifestDigest,
		payload,
		review,
		approval,
		pinnedPublicKey,
		now.UTC(),
	); err != nil {
		return RevalidatedActive{}, err
	}
	activeCompatibility := installed
	if recoverDynamic {
		activeCompatibility.CurrentPolicySchema = manifest.PolicySchema
		activeCompatibility.CurrentBundleGeneration = manifest.BundleGeneration
		activeCompatibility.CurrentPolicyGeneration = payload.PolicyGeneration
		activeCompatibility.CurrentPayloadSHA256 = payloadDigest
	}
	if err := policy.CheckActiveCompatibility(manifest, payload, activeCompatibility); err != nil {
		return RevalidatedActive{}, err
	}
	if resolutionMissing {
		resolution := ResolutionRecord{
			Schema: ResolutionSchema, TransactionID: pointer.TransactionID,
			Domain: pointer.Domain, State: policy.PolicyActive,
			BundleGeneration: pointer.BundleGeneration,
			PolicyGeneration: pointer.PolicyGeneration,
			ManifestSHA256:   pointer.ManifestSHA256,
			PayloadSHA256:    pointer.PayloadSHA256,
			ResolvedAt:       pointer.ActivatedAt, Reason: policy.ReasonNone,
		}
		encoded, marshalErr := marshalRecord(resolution)
		name, nameErr := transactionRecordFilename(recordResolution, pointer.TransactionID)
		if marshalErr != nil || nameErr != nil ||
			store.validateResolutionTransitionLocked(resolution) != nil ||
			store.persistImmutableRecordLocked(recordResolution, name, encoded) != nil {
			return RevalidatedActive{}, ErrActivePointerConsistency
		}
		if _, err := store.applyRetentionLocked(now.UTC()); err != nil {
			return RevalidatedActive{}, err
		}
	}
	return RevalidatedActive{
		Domain: store.domain, Generation: generation,
		ManifestSHA256: manifestDigest, PayloadSHA256: payloadDigest,
		ActivatedAt: pointer.ActivatedAt, ConfirmedAt: pointer.ConfirmedAt,
		Manifest: manifest, Payload: payload, Intent: intent,
	}, nil
}

func activePointerTimeConsistent(
	pointer ActivePointer,
	manifest policy.Manifest,
	now time.Time,
) bool {
	activatedAt, activatedErr := time.Parse(time.RFC3339Nano, pointer.ActivatedAt)
	notBefore, notBeforeErr := time.Parse(time.RFC3339Nano, manifest.NotBefore)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, manifest.ExpiresAt)
	confirmedOK := true
	if pointer.ConfirmedAt != "" {
		confirmedAt, confirmedErr := time.Parse(time.RFC3339Nano, pointer.ConfirmedAt)
		confirmedOK = confirmedErr == nil && !confirmedAt.After(now)
	}
	return activatedErr == nil && notBeforeErr == nil && expiresErr == nil && confirmedOK &&
		!activatedAt.Before(notBefore) && activatedAt.Before(expiresAt) && !activatedAt.After(now)
}

func (store *Store) readStartupPointerLocked() (ActivePointer, error) {
	encoded, err := store.readRecordLocked(activePointerFilename)
	if err != nil {
		return ActivePointer{}, err
	}
	pointer, err := decodeRecord[ActivePointer](encoded)
	if err != nil || pointer.Validate() != nil || pointer.Domain != store.domain {
		return ActivePointer{}, ErrActivePointerConsistency
	}
	return pointer, nil
}

func (store *Store) validateStartupRecordsLocked(
	pointer ActivePointer,
) (CommitIntent, bool, error) {
	receiptName, _ := transactionRecordFilename(recordPrepare, pointer.TransactionID)
	receiptEncoded, err := store.readRecordLocked(receiptName)
	if err != nil {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}
	receipt, err := decodeRecord[PrepareReceipt](receiptEncoded)
	if err != nil || receipt.Validate() != nil || !receiptMatchesPointer(receipt, pointer) {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}

	intentName, _ := transactionRecordFilename(recordCommit, pointer.TransactionID)
	intentEncoded, err := store.readRecordLocked(intentName)
	if err != nil {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}
	intent, err := decodeRecord[CommitIntent](intentEncoded)
	if err != nil || intent.Validate() != nil || !pointerMatchesIntent(pointer, intent) ||
		intent.Approval != pointer.Approval {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}

	resolutionName, _ := transactionRecordFilename(recordResolution, pointer.TransactionID)
	resolutionEncoded, err := store.readRecordLocked(resolutionName)
	if errors.Is(err, ErrRecordNotFound) {
		return intent, true, nil
	}
	if err != nil {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}
	resolution, err := decodeRecord[ResolutionRecord](resolutionEncoded)
	if err != nil || resolution.Validate() != nil || resolution.State != policy.PolicyActive ||
		!resolutionMatchesPointer(resolution, pointer) {
		return CommitIntent{}, false, ErrActivePointerConsistency
	}
	return intent, false, nil
}

func receiptMatchesPointer(receipt PrepareReceipt, pointer ActivePointer) bool {
	return receipt.TransactionID == pointer.TransactionID && receipt.Domain == pointer.Domain &&
		receipt.BundleGeneration == pointer.BundleGeneration &&
		receipt.PolicyGeneration == pointer.PolicyGeneration &&
		receipt.ManifestSHA256 == pointer.ManifestSHA256 &&
		receipt.PayloadSHA256 == pointer.PayloadSHA256 &&
		receipt.ApprovalSHA256 == pointer.ApprovalSHA256
}
