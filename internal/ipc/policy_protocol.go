package ipc

import (
	"encoding/hex"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// PolicyStatusRequest is intentionally empty. The authenticated socket fixes
// the daemon domain, so callers cannot select a different policy namespace.
type PolicyStatusRequest struct{}

// PolicyTransactionIdentity binds every activation operation to one complete
// signed bundle. It contains no caller-controlled path or policy content.
type PolicyTransactionIdentity struct {
	TransactionID        metadata.UUID `json:"transaction_id"`
	BundleGeneration     uint64        `json:"bundle_generation"`
	RootPolicyGeneration uint64        `json:"root_policy_generation"`
	UserPolicyGeneration uint64        `json:"user_policy_generation"`
	ManifestSHA256       string        `json:"manifest_sha256"`
	RootPayloadSHA256    string        `json:"root_payload_sha256"`
	UserPayloadSHA256    string        `json:"user_payload_sha256"`
	ApprovalSHA256       string        `json:"approval_sha256"`
}

type PreparePolicyRequest struct {
	Transaction PolicyTransactionIdentity `json:"transaction"`
}

type CommitPolicyPhase string

const (
	CommitPolicyStage    CommitPolicyPhase = "stage"
	CommitPolicyActivate CommitPolicyPhase = "activate"
	CommitPolicyConfirm  CommitPolicyPhase = "confirm"
)

type CommitPolicyRequest struct {
	Transaction PolicyTransactionIdentity `json:"transaction"`
	Phase       CommitPolicyPhase         `json:"phase"`
}

type AbortPolicyRequest struct {
	Transaction PolicyTransactionIdentity `json:"transaction"`
}

type PolicyStatusResult struct {
	Status                  policy.Status                  `json:"status"`
	AuthorizationSuspension policy.AuthorizationSuspension `json:"authorization_suspension"`
	ExistingState           *policy.ExistingStateStatus    `json:"existing_state,omitempty"`
}

// PreparePolicyResult is the bounded receipt returned after independent local
// verification. Domain and local payload identity are selected by the daemon.
type PreparePolicyResult struct {
	TransactionID    metadata.UUID `json:"transaction_id"`
	Domain           policy.Domain `json:"domain"`
	BundleGeneration uint64        `json:"bundle_generation"`
	PolicyGeneration uint64        `json:"policy_generation"`
	ManifestSHA256   string        `json:"manifest_sha256"`
	PayloadSHA256    string        `json:"payload_sha256"`
	ApprovalSHA256   string        `json:"approval_sha256"`
}

type CommitPolicyResult struct {
	TransactionID metadata.UUID     `json:"transaction_id"`
	Phase         CommitPolicyPhase `json:"phase"`
	Status        policy.Status     `json:"status"`
}

type AbortPolicyResult struct {
	TransactionID metadata.UUID `json:"transaction_id"`
	Status        policy.Status `json:"status"`
}

func (request PreparePolicyRequest) Validate() error {
	return request.Transaction.Validate()
}

func (request CommitPolicyRequest) Validate() error {
	if request.Transaction.Validate() != nil || !request.Phase.Valid() {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func (request AbortPolicyRequest) Validate() error {
	return request.Transaction.Validate()
}

func (identity PolicyTransactionIdentity) Validate() error {
	if !validTransactionID(identity.TransactionID) ||
		identity.BundleGeneration == 0 ||
		identity.RootPolicyGeneration == 0 ||
		identity.UserPolicyGeneration == 0 ||
		!validPolicyDigest(identity.ManifestSHA256) ||
		!validPolicyDigest(identity.RootPayloadSHA256) ||
		!validPolicyDigest(identity.UserPayloadSHA256) ||
		!validPolicyDigest(identity.ApprovalSHA256) {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func (result PolicyStatusResult) Validate() error {
	if result.Status.Validate() != nil || result.AuthorizationSuspension.Validate() != nil {
		return ErrInvalidPolicyMessage
	}
	if result.ExistingState != nil &&
		(result.ExistingState.Validate() != nil ||
			result.ExistingState.Domain != result.Status.Domain ||
			result.Status.State == policy.PolicyNone) {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func (result PreparePolicyResult) Validate() error {
	if !validTransactionID(result.TransactionID) ||
		!result.Domain.Valid() ||
		result.BundleGeneration == 0 ||
		result.PolicyGeneration == 0 ||
		!validPolicyDigest(result.ManifestSHA256) ||
		!validPolicyDigest(result.PayloadSHA256) ||
		!validPolicyDigest(result.ApprovalSHA256) {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func (result CommitPolicyResult) Validate() error {
	if !validTransactionID(result.TransactionID) || !result.Phase.Valid() ||
		result.Status.Validate() != nil {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func (phase CommitPolicyPhase) Valid() bool {
	switch phase {
	case CommitPolicyStage, CommitPolicyActivate, CommitPolicyConfirm:
		return true
	default:
		return false
	}
}

func (result AbortPolicyResult) Validate() error {
	if !validTransactionID(result.TransactionID) || result.Status.Validate() != nil {
		return ErrInvalidPolicyMessage
	}
	return nil
}

func validTransactionID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func validPolicyDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
