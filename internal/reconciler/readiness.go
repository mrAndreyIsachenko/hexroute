package reconciler

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type RawComponentState string

const (
	RawHealthy  RawComponentState = "healthy"
	RawFailed   RawComponentState = "failed"
	RawMissing  RawComponentState = "missing"
	RawConflict RawComponentState = "conflict"
)

type SnapshotBinding struct {
	BootID                 metadata.UUID `json:"boot_id"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	DomainPolicyGeneration uint64        `json:"domain_policy_generation"`
	ControlGeneration      uint64        `json:"control_generation"`
	SnapshotGeneration     uint64        `json:"snapshot_generation"`
	SourceWatermark        uint64        `json:"source_watermark"`
}

type SourceEvidenceState struct {
	Fresh             bool `json:"fresh"`
	BaselineAvailable bool `json:"baseline_available"`
	Gap               bool `json:"gap"`
	Conflict          bool `json:"conflict"`
	Stale             bool `json:"stale"`
}

type RawComponentObservation struct {
	Target                string            `json:"target"`
	State                 RawComponentState `json:"state"`
	Reason                Reason            `json:"reason"`
	ConsecutiveFailures   uint32            `json:"consecutive_failures"`
	FailedDurationSeconds uint32            `json:"failed_duration_seconds"`
	Owner                 policy.Domain     `json:"owner"`
	ObservedBinding       SnapshotBinding   `json:"observed_binding"`
}

type ReadinessPolicy struct {
	CapabilityAllowed              bool   `json:"capability_allowed"`
	Suspended                      bool   `json:"suspended"`
	RequiredConsecutiveFailures    uint32 `json:"required_consecutive_failures"`
	RequiredFailureDurationSeconds uint32 `json:"required_failure_duration_seconds"`
	BudgetRemaining                uint32 `json:"budget_remaining"`
	CooldownRemainingSeconds       uint32 `json:"cooldown_remaining_seconds"`
	BackoffRemainingSeconds        uint32 `json:"backoff_remaining_seconds"`
}

type ReadinessInput struct {
	Target          string                  `json:"target"`
	CapabilityID    CapabilityID            `json:"capability_id"`
	Domain          policy.Domain           `json:"domain"`
	ExpectedBinding SnapshotBinding         `json:"expected_binding"`
	SnapshotSHA256  string                  `json:"snapshot_sha256"`
	Source          SourceEvidenceState     `json:"source"`
	Raw             RawComponentObservation `json:"raw"`
	Policy          ReadinessPolicy         `json:"policy"`
}

type RawProjection struct {
	Target                string            `json:"target"`
	State                 RawComponentState `json:"state"`
	Reason                Reason            `json:"reason"`
	ConsecutiveFailures   uint32            `json:"consecutive_failures"`
	FailedDurationSeconds uint32            `json:"failed_duration_seconds"`
}

type ReadinessEvaluation struct {
	Raw       RawProjection   `json:"raw"`
	Readiness ReadinessRecord `json:"readiness"`
}

type ReadinessProjection struct {
	Target          string          `json:"target"`
	RawComponent    RawProjection   `json:"raw_component"`
	ActionReadiness ReadinessRecord `json:"action_readiness"`
}

var ErrInvalidReadinessInput = errors.New("invalid readiness input")

func EvaluateReadiness(input ReadinessInput) (ReadinessEvaluation, error) {
	if input.validate() != nil {
		return ReadinessEvaluation{}, ErrInvalidReadinessInput
	}
	raw := RawProjection{
		Target:                input.Raw.Target,
		State:                 input.Raw.State,
		Reason:                input.Raw.Reason,
		ConsecutiveFailures:   input.Raw.ConsecutiveFailures,
		FailedDurationSeconds: input.Raw.FailedDurationSeconds,
	}
	if !input.Policy.CapabilityAllowed {
		return evaluation(raw, ReadinessDenied, ReasonPolicy, RetryNever, 0), nil
	}
	if input.Policy.Suspended {
		return evaluation(raw, ReadinessDenied, ReasonPolicy, RetryNever, 0), nil
	}
	if input.Raw.Owner != input.Domain {
		return evaluation(raw, ReadinessDenied, ReasonOwnership, RetryNever, 0), nil
	}
	if input.Raw.ObservedBinding != input.ExpectedBinding {
		return evaluation(raw, ReadinessDenied, ReasonGeneration, RetryNever, 0), nil
	}
	if !input.Source.Fresh || !input.Source.BaselineAvailable || input.Source.Stale {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonFreshness, RetryAfterHint, 60), nil
	}
	if input.Source.Gap || input.Source.Conflict || input.Raw.State == RawConflict {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonLineage, RetryAfterHint, 60), nil
	}
	if input.Raw.State == RawHealthy {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonNoAction, RetryAfterHint, 60), nil
	}
	if input.Raw.ConsecutiveFailures < input.Policy.RequiredConsecutiveFailures ||
		input.Raw.FailedDurationSeconds < input.Policy.RequiredFailureDurationSeconds {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonThreshold, RetryAfterHint, 60), nil
	}
	if input.Policy.BudgetRemaining == 0 {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonBudget, RetryAfterHint, 300), nil
	}
	if input.Policy.BackoffRemainingSeconds > 0 {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonBackoff, RetryAfterHint, boundedRetry(input.Policy.BackoffRemainingSeconds)), nil
	}
	if input.Policy.CooldownRemainingSeconds > 0 {
		return evaluation(raw, ReadinessTemporarilyBlocked, ReasonCooldown, RetryAfterHint, boundedRetry(input.Policy.CooldownRemainingSeconds)), nil
	}
	return evaluation(raw, ReadinessReady, ReasonAccepted, RetryNone, 0), nil
}

func ProjectReadiness(evaluation ReadinessEvaluation) ReadinessProjection {
	return ReadinessProjection{
		Target:          evaluation.Raw.Target,
		RawComponent:    evaluation.Raw,
		ActionReadiness: evaluation.Readiness,
	}
}

func evaluation(raw RawProjection, status ReadinessStatus, reason Reason, retry RetryClass, retryAfter uint32) ReadinessEvaluation {
	return ReadinessEvaluation{
		Raw: raw,
		Readiness: ReadinessRecord{
			Target:            raw.Target,
			Status:            status,
			Reason:            reason,
			RetryClass:        retry,
			RetryAfterSeconds: retryAfter,
		},
	}
}

func (input ReadinessInput) validate() error {
	if !validIdentifier(input.Target) ||
		input.Raw.Target != input.Target ||
		!capabilityIDPattern.MatchString(string(input.CapabilityID)) ||
		hasProductionFragment(string(input.CapabilityID)) ||
		!input.Domain.Valid() ||
		input.ExpectedBinding.validate() != nil ||
		!validDigest(input.SnapshotSHA256) ||
		input.Source.validate() != nil ||
		input.Raw.validate() != nil ||
		input.Policy.validate() != nil {
		return ErrInvalidReadinessInput
	}
	return nil
}

func (binding SnapshotBinding) validate() error {
	if !validUUID(binding.BootID) ||
		binding.BundleGeneration == 0 ||
		binding.DomainPolicyGeneration == 0 ||
		binding.ControlGeneration == 0 ||
		binding.SnapshotGeneration == 0 ||
		binding.SourceWatermark == 0 {
		return ErrInvalidReadinessInput
	}
	return nil
}

func (source SourceEvidenceState) validate() error {
	if !source.BaselineAvailable && (source.Gap || source.Conflict) {
		return ErrInvalidReadinessInput
	}
	return nil
}

func (raw RawComponentObservation) validate() error {
	if !validIdentifier(raw.Target) ||
		!raw.State.Valid() ||
		!raw.Reason.Valid() ||
		!raw.Owner.Valid() ||
		raw.ObservedBinding.validate() != nil {
		return ErrInvalidReadinessInput
	}
	if raw.State == RawHealthy && (raw.ConsecutiveFailures != 0 || raw.FailedDurationSeconds != 0) {
		return ErrInvalidReadinessInput
	}
	return nil
}

func (policy ReadinessPolicy) validate() error {
	if policy.RequiredConsecutiveFailures == 0 ||
		policy.RequiredFailureDurationSeconds == 0 ||
		policy.CooldownRemainingSeconds > MaxRetryAfterSeconds ||
		policy.BackoffRemainingSeconds > MaxRetryAfterSeconds {
		return ErrInvalidReadinessInput
	}
	return nil
}

func (state RawComponentState) Valid() bool {
	return state == RawHealthy || state == RawFailed || state == RawMissing || state == RawConflict
}

func boundedRetry(value uint32) uint32 {
	if value > MaxRetryAfterSeconds {
		return MaxRetryAfterSeconds
	}
	return value
}
