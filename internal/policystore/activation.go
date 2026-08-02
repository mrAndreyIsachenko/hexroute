package policystore

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

type CommitCandidateResult struct {
	Pointer      ActivePointer
	PolicySchema uint16
}

func (store *Store) CommitCandidate(
	input PrepareCandidateInput,
	now time.Time,
) (CommitCandidateResult, error) {
	if input.Validate() != nil || now.IsZero() {
		return CommitCandidateResult{}, ErrCandidateIdentity
	}
	receipt, err := store.ReadPrepareReceipt(input.TransactionID)
	if err != nil || !receiptMatchesCandidateInput(receipt, input, storeDomain(store)) {
		return CommitCandidateResult{}, ErrRecordConflict
	}

	generation := Generation{Bundle: receipt.BundleGeneration, Policy: receipt.PolicyGeneration}
	manifestEncoded, err := store.ReadArtifact(generation, ArtifactManifest)
	if err != nil {
		return CommitCandidateResult{}, err
	}
	manifest, manifestDigest, err := policy.DecodeManifestArtifact(manifestEncoded)
	if err != nil || manifestDigest != input.ManifestSHA256 {
		return CommitCandidateResult{}, ErrRecordConflict
	}
	approvalEncoded, err := store.ReadArtifact(generation, ArtifactApproval)
	if err != nil {
		return CommitCandidateResult{}, err
	}
	approval, err := policyapproval.DecodeApprovalArtifact(approvalEncoded)
	if err != nil {
		return CommitCandidateResult{}, err
	}
	if !approvalMatchesCandidateInput(approval, input) {
		return CommitCandidateResult{}, ErrRecordConflict
	}

	intent, err := store.ReadCommitIntent(input.TransactionID)
	switch {
	case err == nil:
		if !commitIntentMatchesCandidateInput(intent, input) || intent.Approval != approval {
			return CommitCandidateResult{}, ErrRecordConflict
		}
	case errors.Is(err, ErrRecordNotFound):
		intent = CommitIntent{
			Schema: CommitIntentSchema, TransactionID: input.TransactionID,
			BundleGeneration:     input.BundleGeneration,
			RootPolicyGeneration: input.RootPolicyGeneration,
			UserPolicyGeneration: input.UserPolicyGeneration,
			ManifestSHA256:       input.ManifestSHA256,
			RootPayloadSHA256:    input.RootPayloadSHA256,
			UserPayloadSHA256:    input.UserPayloadSHA256,
			ApprovalSHA256:       input.ApprovalSHA256, Approval: approval,
			CreatedAt: now.UTC().Format(time.RFC3339Nano),
		}
		if err := store.PersistCommitIntent(intent); err != nil {
			return CommitCandidateResult{}, err
		}
	case err != nil:
		return CommitCandidateResult{}, err
	}

	intentDigest, err := CommitIntentSHA256(intent)
	if err != nil {
		return CommitCandidateResult{}, err
	}
	pointer, err := store.ReadActivePointer()
	switch {
	case err == nil:
		if activePointerMatchesCandidateInput(pointer, input, store.domain) &&
			pointer.CommitIntentSHA256 == intentDigest && pointer.Approval == approval {
			break
		}
		if pointer.BundleGeneration >= input.BundleGeneration {
			return CommitCandidateResult{}, ErrStaleActivePointer
		}
		fallthrough
	case errors.Is(err, ErrRecordNotFound):
		pointer = ActivePointer{
			Schema: ActivePointerSchema, TransactionID: input.TransactionID,
			Domain: store.domain, BundleGeneration: receipt.BundleGeneration,
			PolicyGeneration:   receipt.PolicyGeneration,
			ManifestSHA256:     receipt.ManifestSHA256,
			PayloadSHA256:      receipt.PayloadSHA256,
			ApprovalSHA256:     receipt.ApprovalSHA256,
			CommitIntentSHA256: intentDigest, Approval: approval,
			ActivatedAt: intent.CreatedAt,
		}
		if err := store.PersistActivePointer(pointer); err != nil {
			return CommitCandidateResult{}, err
		}
	case err != nil:
		return CommitCandidateResult{}, err
	}

	_, err = store.ResolveGeneration(ResolutionRecord{
		Schema: ResolutionSchema, TransactionID: input.TransactionID,
		Domain: store.domain, State: policy.PolicyActive,
		BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256,
		PayloadSHA256:    receipt.PayloadSHA256,
		ResolvedAt:       pointer.ActivatedAt, Reason: policy.ReasonNone,
	}, now.UTC())
	if err != nil {
		return CommitCandidateResult{}, err
	}
	return CommitCandidateResult{Pointer: pointer, PolicySchema: manifest.PolicySchema}, nil
}

func (store *Store) AbortCandidate(
	input PrepareCandidateInput,
	now time.Time,
) error {
	if input.Validate() != nil || now.IsZero() {
		return ErrCandidateIdentity
	}
	receipt, err := store.ReadPrepareReceipt(input.TransactionID)
	if err != nil || !receiptMatchesCandidateInput(receipt, input, storeDomain(store)) {
		return ErrRecordConflict
	}
	generation := Generation{Bundle: receipt.BundleGeneration, Policy: receipt.PolicyGeneration}
	approvalEncoded, err := store.ReadArtifact(generation, ArtifactApproval)
	if err != nil {
		return err
	}
	approval, err := policyapproval.DecodeApprovalArtifact(approvalEncoded)
	if err != nil || !approvalMatchesCandidateInput(approval, input) {
		return ErrRecordConflict
	}
	_, err = store.ResolveGeneration(ResolutionRecord{
		Schema: ResolutionSchema, TransactionID: input.TransactionID,
		Domain: store.domain, State: policy.PolicyRejected,
		BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256,
		PayloadSHA256:    receipt.PayloadSHA256,
		ResolvedAt:       now.UTC().Format(time.RFC3339Nano),
		Reason:           policy.ReasonOperatorAborted,
	}, now.UTC())
	return err
}

func approvalMatchesCandidateInput(
	approval policyapproval.SignedApproval,
	input PrepareCandidateInput,
) bool {
	digest, err := policyapproval.ApprovalSHA256(approval)
	return err == nil && digest == input.ApprovalSHA256 &&
		approval.Statement.ManifestSHA256 == input.ManifestSHA256 &&
		approval.Statement.RootSHA256 == input.RootPayloadSHA256 &&
		approval.Statement.UserSHA256 == input.UserPayloadSHA256
}

func receiptMatchesCandidateInput(
	receipt PrepareReceipt,
	input PrepareCandidateInput,
	domain policy.Domain,
) bool {
	if receipt.TransactionID != input.TransactionID || receipt.Domain != domain ||
		receipt.BundleGeneration != input.BundleGeneration ||
		receipt.ManifestSHA256 != input.ManifestSHA256 ||
		receipt.ApprovalSHA256 != input.ApprovalSHA256 {
		return false
	}
	if domain == policy.DomainRoot {
		return receipt.PolicyGeneration == input.RootPolicyGeneration &&
			receipt.PayloadSHA256 == input.RootPayloadSHA256
	}
	return receipt.PolicyGeneration == input.UserPolicyGeneration &&
		receipt.PayloadSHA256 == input.UserPayloadSHA256
}

func commitIntentMatchesCandidateInput(
	intent CommitIntent,
	input PrepareCandidateInput,
) bool {
	return intent.TransactionID == input.TransactionID &&
		intent.BundleGeneration == input.BundleGeneration &&
		intent.RootPolicyGeneration == input.RootPolicyGeneration &&
		intent.UserPolicyGeneration == input.UserPolicyGeneration &&
		intent.ManifestSHA256 == input.ManifestSHA256 &&
		intent.RootPayloadSHA256 == input.RootPayloadSHA256 &&
		intent.UserPayloadSHA256 == input.UserPayloadSHA256 &&
		intent.ApprovalSHA256 == input.ApprovalSHA256
}

func activePointerMatchesCandidateInput(
	pointer ActivePointer,
	input PrepareCandidateInput,
	domain policy.Domain,
) bool {
	return pointer.TransactionID == input.TransactionID && pointer.Domain == domain &&
		pointer.BundleGeneration == input.BundleGeneration &&
		pointer.ManifestSHA256 == input.ManifestSHA256 &&
		pointer.ApprovalSHA256 == input.ApprovalSHA256 &&
		receiptMatchesCandidateInput(PrepareReceipt{
			TransactionID: pointer.TransactionID, Domain: pointer.Domain,
			BundleGeneration: pointer.BundleGeneration,
			PolicyGeneration: pointer.PolicyGeneration,
			ManifestSHA256:   pointer.ManifestSHA256,
			PayloadSHA256:    pointer.PayloadSHA256,
			ApprovalSHA256:   pointer.ApprovalSHA256,
		}, input, domain)
}
