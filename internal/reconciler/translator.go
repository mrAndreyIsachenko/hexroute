package reconciler

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type AdapterMetadata struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type ProposalBinding struct {
	RequestID       metadata.UUID   `json:"request_id"`
	ActionID        metadata.UUID   `json:"action_id,omitempty"`
	Target          string          `json:"target"`
	Domain          policy.Domain   `json:"domain"`
	CapabilityID    CapabilityID    `json:"capability_id"`
	ProposalSHA256  string          `json:"proposal_sha256"`
	DiffSHA256      string          `json:"diff_sha256"`
	SnapshotSHA256  string          `json:"snapshot_sha256"`
	ReadinessSHA256 string          `json:"readiness_sha256"`
	SnapshotBinding SnapshotBinding `json:"snapshot_binding"`
}

type TranslationStep struct {
	ID                 string         `json:"id"`
	Operation          OperationClass `json:"operation"`
	InputSHA256        string         `json:"input_sha256"`
	BeforeSHA256       string         `json:"before_sha256"`
	AppliedSHA256      string         `json:"applied_sha256"`
	VerificationSHA256 string         `json:"verification_sha256"`
	CompensationSHA256 string         `json:"compensation_sha256"`
}

type TranslationInput struct {
	Proposal               ProposalBinding   `json:"proposal"`
	CurrentSnapshotBinding SnapshotBinding   `json:"current_snapshot_binding"`
	Readiness              ReadinessRecord   `json:"readiness"`
	Registry               Registry          `json:"-"`
	Adapter                AdapterMetadata   `json:"adapter"`
	SemanticNoop           bool              `json:"semantic_noop"`
	Steps                  []TranslationStep `json:"steps"`
}

type TranslationResult struct {
	Acknowledgement AcknowledgementRecord `json:"acknowledgement"`
	Plan            *ActionPlanRecord     `json:"plan,omitempty"`
}

var ErrInvalidTranslationInput = errors.New("invalid proposal translation input")

func TranslateProposal(input TranslationInput) (TranslationResult, error) {
	if input.validateCommon() != nil {
		return TranslationResult{}, ErrInvalidTranslationInput
	}
	descriptor, exists := input.Registry.Lookup(input.Proposal.CapabilityID)
	if !exists {
		return denied(input.Proposal.RequestID, ReasonCapability), nil
	}
	if descriptor.AdapterID != input.Adapter.ID ||
		(!input.SemanticNoop && descriptor.OperationClass != input.operationClass()) {
		return denied(input.Proposal.RequestID, ReasonCapability), nil
	}
	if input.CurrentSnapshotBinding != input.Proposal.SnapshotBinding {
		return denied(input.Proposal.RequestID, ReasonGeneration), nil
	}
	if input.Readiness.Target != input.Proposal.Target {
		return denied(input.Proposal.RequestID, ReasonTarget), nil
	}
	switch input.Readiness.Status {
	case ReadinessDenied:
		return denied(input.Proposal.RequestID, input.Readiness.Reason), nil
	case ReadinessTemporarilyBlocked:
		return TranslationResult{
			Acknowledgement: AcknowledgementRecord{
				RequestID:         input.Proposal.RequestID,
				Class:             AckTemporarilyRejected,
				Reason:            input.Readiness.Reason,
				RetryClass:        RetryAfterHint,
				RetryAfterSeconds: input.Readiness.RetryAfterSeconds,
			},
		}, nil
	}
	if input.SemanticNoop {
		return TranslationResult{
			Acknowledgement: AcknowledgementRecord{
				RequestID:  input.Proposal.RequestID,
				Class:      AckAccepted,
				Reason:     ReasonNoAction,
				RetryClass: RetryNone,
				NoAction:   true,
			},
		}, nil
	}
	if !validUUID(input.Proposal.ActionID) || !validTranslationSteps(input.Steps) {
		return TranslationResult{}, ErrInvalidTranslationInput
	}
	plan, err := buildActionPlan(input, descriptor)
	if err != nil {
		return TranslationResult{}, err
	}
	return TranslationResult{
		Acknowledgement: AcknowledgementRecord{
			RequestID:  input.Proposal.RequestID,
			Class:      AckAccepted,
			Reason:     ReasonAccepted,
			RetryClass: RetryNone,
			ActionID:   input.Proposal.ActionID,
		},
		Plan: &plan,
	}, nil
}

func buildActionPlan(input TranslationInput, descriptor CapabilityDescriptor) (ActionPlanRecord, error) {
	stepDigests := make([]string, len(input.Steps))
	verificationDigests := make([]string, len(input.Steps))
	compensationDigests := make([]string, len(input.Steps))
	for index, step := range input.Steps {
		if step.Operation != descriptor.OperationClass {
			return ActionPlanRecord{}, ErrInvalidTranslationInput
		}
		stepDigests[index] = policy.SHA256Hex([]byte(step.ID + ":" + step.InputSHA256 + ":" + step.BeforeSHA256 + ":" + step.AppliedSHA256))
		verificationDigests[index] = step.VerificationSHA256
		compensationDigests[index] = step.CompensationSHA256
	}
	plan := ActionPlanRecord{
		Target:                 input.Proposal.Target,
		CapabilityID:           input.Proposal.CapabilityID,
		AdapterVersion:         input.Adapter.Version,
		AdapterSHA256:          input.Adapter.SHA256,
		ProposalSHA256:         input.Proposal.ProposalSHA256,
		DiffSHA256:             input.Proposal.DiffSHA256,
		SnapshotSHA256:         input.Proposal.SnapshotSHA256,
		ReadinessSHA256:        input.Proposal.ReadinessSHA256,
		BundleGeneration:       input.Proposal.SnapshotBinding.BundleGeneration,
		DomainPolicyGeneration: input.Proposal.SnapshotBinding.DomainPolicyGeneration,
		ControlGeneration:      input.Proposal.SnapshotBinding.ControlGeneration,
		SnapshotGeneration:     input.Proposal.SnapshotBinding.SnapshotGeneration,
		StepDigests:            stepDigests,
		VerificationDigests:    verificationDigests,
		CompensationDigests:    compensationDigests,
	}
	digest, err := actionPlanDigest(plan)
	if err != nil {
		return ActionPlanRecord{}, ErrInvalidTranslationInput
	}
	plan.PlanSHA256 = digest
	if plan.Validate() != nil {
		return ActionPlanRecord{}, ErrInvalidTranslationInput
	}
	return plan, nil
}

func actionPlanDigest(plan ActionPlanRecord) (string, error) {
	plan.PlanSHA256 = ""
	encoded, err := policy.MarshalCanonical(plan)
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(encoded), nil
}

func (input TranslationInput) validateCommon() error {
	if input.Proposal.validate() != nil ||
		input.CurrentSnapshotBinding.validate() != nil ||
		input.Readiness.Validate() != nil ||
		input.Adapter.validate() != nil ||
		!input.Registry.SyntheticOnly() {
		return ErrInvalidTranslationInput
	}
	return nil
}

func (proposal ProposalBinding) validate() error {
	if !validUUID(proposal.RequestID) ||
		(proposal.ActionID != "" && !validUUID(proposal.ActionID)) ||
		!validIdentifier(proposal.Target) ||
		!proposal.Domain.Valid() ||
		!capabilityIDPattern.MatchString(string(proposal.CapabilityID)) ||
		hasProductionFragment(string(proposal.CapabilityID)) ||
		!validDigest(proposal.ProposalSHA256) ||
		!validDigest(proposal.DiffSHA256) ||
		!validDigest(proposal.SnapshotSHA256) ||
		!validDigest(proposal.ReadinessSHA256) ||
		proposal.SnapshotBinding.validate() != nil {
		return ErrInvalidTranslationInput
	}
	return nil
}

func (metadata AdapterMetadata) validate() error {
	if !adapterIDPattern.MatchString(metadata.ID) ||
		hasProductionFragment(metadata.ID) ||
		!versionPattern.MatchString(metadata.Version) ||
		!validDigest(metadata.SHA256) {
		return ErrInvalidTranslationInput
	}
	return nil
}

func validTranslationSteps(steps []TranslationStep) bool {
	if len(steps) == 0 || len(steps) > MaxPlanSteps {
		return false
	}
	seen := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		if !validIdentifier(step.ID) ||
			!step.Operation.valid() ||
			!validDigest(step.InputSHA256) ||
			!validDigest(step.BeforeSHA256) ||
			!validDigest(step.AppliedSHA256) ||
			!validDigest(step.VerificationSHA256) ||
			!validDigest(step.CompensationSHA256) {
			return false
		}
		if _, exists := seen[step.ID]; exists {
			return false
		}
		seen[step.ID] = struct{}{}
	}
	return true
}

func (input TranslationInput) operationClass() OperationClass {
	if len(input.Steps) == 0 {
		return ""
	}
	return input.Steps[0].Operation
}

func denied(requestID metadata.UUID, reason Reason) TranslationResult {
	if !reason.Valid() {
		reason = ReasonSchema
	}
	return TranslationResult{
		Acknowledgement: AcknowledgementRecord{
			RequestID:  requestID,
			Class:      AckDenied,
			Reason:     reason,
			RetryClass: RetryNever,
		},
	}
}
