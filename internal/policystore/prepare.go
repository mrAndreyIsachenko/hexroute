package policystore

import (
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

type PrepareCandidateInput struct {
	TransactionID        metadata.UUID
	BundleGeneration     uint64
	RootPolicyGeneration uint64
	UserPolicyGeneration uint64
	ManifestSHA256       string
	RootPayloadSHA256    string
	UserPayloadSHA256    string
	ApprovalSHA256       string
}

var ErrCandidateIdentity = errors.New("policy candidate identity mismatch")

// PrepareCandidate revalidates immutable, generation-addressed local artifacts
// before durably recording a receipt. The input cannot select a path or carry
// policy content.
func (store *Store) PrepareCandidate(
	input PrepareCandidateInput,
	installed policy.InstalledCompatibility,
	pinnedPublicKey ed25519.PublicKey,
	now time.Time,
) (PrepareReceipt, error) {
	if input.Validate() != nil || installed.Validate() != nil ||
		installed.Domain != storeDomain(store) ||
		len(pinnedPublicKey) != ed25519.PublicKeySize || now.IsZero() {
		return PrepareReceipt{}, ErrCandidateIdentity
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return PrepareReceipt{}, err
	}

	policyGeneration := input.RootPolicyGeneration
	payloadSHA256 := input.RootPayloadSHA256
	if store.domain == policy.DomainUser {
		policyGeneration = input.UserPolicyGeneration
		payloadSHA256 = input.UserPayloadSHA256
	}
	generation := Generation{Bundle: input.BundleGeneration, Policy: policyGeneration}
	manifestEncoded, err := store.readArtifactLocked(generation, ArtifactManifest)
	if err != nil {
		return PrepareReceipt{}, err
	}
	payloadEncoded, err := store.readArtifactLocked(generation, ArtifactPayload)
	if err != nil {
		return PrepareReceipt{}, err
	}
	reviewEncoded, err := store.readArtifactLocked(generation, ArtifactReview)
	if err != nil {
		return PrepareReceipt{}, err
	}
	approvalEncoded, err := store.readArtifactLocked(generation, ArtifactApproval)
	if err != nil {
		return PrepareReceipt{}, err
	}

	manifest, manifestSHA256, err := policy.DecodeManifestArtifact(manifestEncoded)
	if err != nil {
		return PrepareReceipt{}, err
	}
	payload, decodedPayloadSHA256, err := policy.DecodeDomainPayloadArtifact(payloadEncoded)
	if err != nil {
		return PrepareReceipt{}, err
	}
	review, err := policyapproval.DecodeReviewArtifact(reviewEncoded)
	if err != nil {
		return PrepareReceipt{}, err
	}
	approval, err := policyapproval.DecodeApprovalArtifact(approvalEncoded)
	if err != nil {
		return PrepareReceipt{}, err
	}
	approvalSHA256, err := policyapproval.ApprovalSHA256(approval)
	if err != nil || manifest.BundleGeneration != input.BundleGeneration ||
		manifest.Root.Generation != input.RootPolicyGeneration ||
		manifest.User.Generation != input.UserPolicyGeneration ||
		manifestSHA256 != input.ManifestSHA256 ||
		manifest.Root.PayloadSHA256 != input.RootPayloadSHA256 ||
		manifest.User.PayloadSHA256 != input.UserPayloadSHA256 ||
		payload.Domain != store.domain || payload.PolicyGeneration != policyGeneration ||
		decodedPayloadSHA256 != payloadSHA256 || approvalSHA256 != input.ApprovalSHA256 {
		return PrepareReceipt{}, ErrCandidateIdentity
	}
	if err := policyapproval.VerifyDomainCandidate(
		manifest,
		manifestSHA256,
		payload,
		review,
		approval,
		pinnedPublicKey,
		now.UTC(),
	); err != nil {
		return PrepareReceipt{}, err
	}
	if err := policy.CheckCandidateCompatibility(manifest, payload, installed); err != nil {
		return PrepareReceipt{}, err
	}

	name, err := transactionRecordFilename(recordPrepare, input.TransactionID)
	if err != nil {
		return PrepareReceipt{}, err
	}
	existing, err := store.readRecordLocked(name)
	if err == nil {
		receipt, decodeErr := decodeRecord[PrepareReceipt](existing)
		if decodeErr != nil || receipt.Validate() != nil ||
			!receiptMatchesCandidateInput(receipt, input, store.domain) {
			return PrepareReceipt{}, ErrRecordConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, ErrRecordNotFound) {
		return PrepareReceipt{}, err
	}

	receipt := PrepareReceipt{
		Schema: PrepareReceiptSchema, TransactionID: input.TransactionID,
		Domain: store.domain, BundleGeneration: input.BundleGeneration,
		PolicyGeneration: policyGeneration, ManifestSHA256: manifestSHA256,
		PayloadSHA256: decodedPayloadSHA256, ApprovalSHA256: approvalSHA256,
		PreparedAt: now.UTC().Format(time.RFC3339Nano),
	}
	encoded, err := marshalRecord(receipt)
	if err != nil {
		return PrepareReceipt{}, err
	}
	if err := store.persistImmutableRecordLocked(recordPrepare, name, encoded); err != nil {
		return PrepareReceipt{}, err
	}
	return receipt, nil
}

func (input PrepareCandidateInput) Validate() error {
	if !validTransactionID(input.TransactionID) ||
		input.BundleGeneration == 0 ||
		input.RootPolicyGeneration == 0 ||
		input.UserPolicyGeneration == 0 ||
		!validDigest(input.ManifestSHA256) ||
		!validDigest(input.RootPayloadSHA256) ||
		!validDigest(input.UserPayloadSHA256) ||
		!validDigest(input.ApprovalSHA256) {
		return ErrCandidateIdentity
	}
	return nil
}
