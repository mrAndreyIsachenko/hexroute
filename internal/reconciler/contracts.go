package reconciler

import (
	"errors"
	"regexp"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	ActionRecordSchema     = "hexroute.reconciler-action-record.v1"
	ActionProvenanceSchema = "hexroute.reconciler-action-provenance.v1"
	MaxRecordBytes         = 16 * 1024
	MaxIdentifierBytes     = 96
	MaxDigestReferences    = 16
	MaxPlanSteps           = 16
	MaxResources           = 16
	MaxRetryAfterSeconds   = 3600
)

type RecordKind string

const (
	RecordReadiness        RecordKind = "readiness"
	RecordAcknowledgement  RecordKind = "acknowledgement"
	RecordActionPlan       RecordKind = "action_plan"
	RecordOperationSession RecordKind = "operation_session"
	RecordCheckpoint       RecordKind = "checkpoint"
	RecordAttempt          RecordKind = "attempt"
	RecordStep             RecordKind = "step"
	RecordResource         RecordKind = "resource"
	RecordOutcome          RecordKind = "outcome"
	RecordIncident         RecordKind = "incident"
)

type Producer string

const (
	ProducerRootDaemon Producer = "root_daemon"
	ProducerUserDaemon Producer = "user_daemon"
	ProducerSynthetic  Producer = "synthetic_engine"
)

type ReadinessStatus string

const (
	ReadinessReady              ReadinessStatus = "ready"
	ReadinessTemporarilyBlocked ReadinessStatus = "temporarily_blocked"
	ReadinessDenied             ReadinessStatus = "denied"
)

type AckClass string

const (
	AckAccepted            AckClass = "accepted"
	AckTemporarilyRejected AckClass = "temporarily_rejected"
	AckDenied              AckClass = "denied"
)

type RetryClass string

const (
	RetryNone      RetryClass = "none"
	RetryAfterHint RetryClass = "retry_after"
	RetryNever     RetryClass = "never"
)

type Reason string

const (
	ReasonNone              Reason = "none"
	ReasonNoAction          Reason = "no_action"
	ReasonAccepted          Reason = "accepted"
	ReasonFreshness         Reason = "freshness"
	ReasonThreshold         Reason = "threshold"
	ReasonBudget            Reason = "budget"
	ReasonBackoff           Reason = "backoff"
	ReasonCooldown          Reason = "cooldown"
	ReasonPolicy            Reason = "policy"
	ReasonCapability        Reason = "capability"
	ReasonOwnership         Reason = "ownership"
	ReasonSchema            Reason = "schema"
	ReasonGeneration        Reason = "generation"
	ReasonTarget            Reason = "target"
	ReasonLineage           Reason = "lineage"
	ReasonResumeDenied      Reason = "resume_denied"
	ReasonVerification      Reason = "verification"
	ReasonCompensation      Reason = "compensation"
	ReasonCleanup           Reason = "cleanup"
	ReasonExpired           Reason = "expired"
	ReasonCancelled         Reason = "cancelled"
	ReasonSafeMode          Reason = "safe_mode"
	ReasonTelemetryPending  Reason = "telemetry_pending"
	ReasonTelemetryRejected Reason = "telemetry_rejected"
	ReasonGapUnrecoverable  Reason = "telemetry_gap_unrecoverable"
)

type AttemptState string

const (
	AttemptPending    AttemptState = "pending"
	AttemptClaimed    AttemptState = "claimed"
	AttemptRunning    AttemptState = "running"
	AttemptVerifying  AttemptState = "verifying"
	AttemptCommitted  AttemptState = "committed"
	AttemptExpired    AttemptState = "expired"
	AttemptDenied     AttemptState = "denied"
	AttemptCancelled  AttemptState = "cancelled"
	AttemptRolledBack AttemptState = "rolled_back"
	AttemptFailed     AttemptState = "failed"
	AttemptSafeMode   AttemptState = "safe_mode"
)

type TerminalOutcome string

const (
	OutcomeCommitted  TerminalOutcome = "committed"
	OutcomeExpired    TerminalOutcome = "expired"
	OutcomeDenied     TerminalOutcome = "denied"
	OutcomeCancelled  TerminalOutcome = "cancelled"
	OutcomeRolledBack TerminalOutcome = "rolled_back"
	OutcomeFailed     TerminalOutcome = "failed"
	OutcomeSafeMode   TerminalOutcome = "safe_mode"
)

type ReportDeliveryState string

const (
	ReportPending            ReportDeliveryState = "pending"
	ReportAcknowledged       ReportDeliveryState = "acknowledged"
	ReportTerminallyRejected ReportDeliveryState = "terminally_rejected"
)

type SessionLifecycle string

const (
	SessionRunning   SessionLifecycle = "running"
	SessionSuspended SessionLifecycle = "suspended"
	SessionCancelled SessionLifecycle = "cancelled"
	SessionFailed    SessionLifecycle = "failed"
	SessionCompleted SessionLifecycle = "completed"
)

type StepState string

const (
	StepPending     StepState = "pending"
	StepApplied     StepState = "applied"
	StepVerified    StepState = "verified"
	StepCompensated StepState = "compensated"
	StepFailed      StepState = "failed"
	StepSkippedNoop StepState = "skipped_noop"
)

type ResourceKind string

const (
	ResourceSyntheticHelper ResourceKind = "synthetic_helper"
	ResourceSyntheticFile   ResourceKind = "synthetic_file"
	ResourceSyntheticLease  ResourceKind = "synthetic_lease"
)

type ResourceState string

const (
	ResourceRegistered ResourceState = "registered"
	ResourceClosed     ResourceState = "closed"
	ResourceFailed     ResourceState = "failed"
)

type WorkflowKind string

const (
	WorkflowSyntheticQualification WorkflowKind = "synthetic_qualification"
	WorkflowShadowComparison       WorkflowKind = "shadow_comparison"
	WorkflowCrashRecoveryDrill     WorkflowKind = "crash_recovery_drill"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type ActionProvenance struct {
	Schema                 string        `json:"schema"`
	RecordID               metadata.UUID `json:"record_id"`
	Kind                   RecordKind    `json:"kind"`
	ParentActionID         metadata.UUID `json:"parent_action_id,omitempty"`
	RootActionID           metadata.UUID `json:"root_action_id,omitempty"`
	Producer               Producer      `json:"producer"`
	Domain                 policy.Domain `json:"domain"`
	BootID                 metadata.UUID `json:"boot_id"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	DomainPolicyGeneration uint64        `json:"domain_policy_generation"`
	ControlGeneration      uint64        `json:"control_generation"`
	SnapshotGeneration     uint64        `json:"snapshot_generation"`
	SourceSHA256           string        `json:"source_sha256"`
	InputSHA256            string        `json:"input_sha256"`
	OutputSHA256           string        `json:"output_sha256"`
	ObservedAt             string        `json:"observed_at"`
	SourceMonotonicNS      int64         `json:"source_monotonic_ns"`
}

type ReadinessRecord struct {
	Target            string          `json:"target"`
	Status            ReadinessStatus `json:"status"`
	Reason            Reason          `json:"reason"`
	RetryClass        RetryClass      `json:"retry_class"`
	RetryAfterSeconds uint32          `json:"retry_after_seconds,omitempty"`
}

type AcknowledgementRecord struct {
	RequestID         metadata.UUID `json:"request_id"`
	Class             AckClass      `json:"class"`
	Reason            Reason        `json:"reason"`
	RetryClass        RetryClass    `json:"retry_class"`
	RetryAfterSeconds uint32        `json:"retry_after_seconds,omitempty"`
	ActionID          metadata.UUID `json:"action_id,omitempty"`
	NoAction          bool          `json:"no_action,omitempty"`
}

type ActionPlanRecord struct {
	PlanSHA256          string       `json:"plan_sha256"`
	Target              string       `json:"target"`
	CapabilityID        CapabilityID `json:"capability_id"`
	AdapterVersion      string       `json:"adapter_version"`
	StepDigests         []string     `json:"step_digests"`
	VerificationDigests []string     `json:"verification_digests"`
	CompensationDigests []string     `json:"compensation_digests"`
}

type OperationSessionRecord struct {
	OperationID     metadata.UUID    `json:"operation_id"`
	Workflow        WorkflowKind     `json:"workflow"`
	Lifecycle       SessionLifecycle `json:"lifecycle"`
	ContractVersion string           `json:"contract_version"`
	RuntimeVersion  string           `json:"runtime_version"`
	ManifestSHA256  string           `json:"manifest_sha256"`
	ChildActionIDs  []metadata.UUID  `json:"child_action_ids,omitempty"`
}

type CheckpointRecord struct {
	OperationID            metadata.UUID   `json:"operation_id"`
	Sequence               uint64          `json:"sequence"`
	ParentCheckpointSHA256 string          `json:"parent_checkpoint_sha256,omitempty"`
	ReducerSHA256          string          `json:"reducer_sha256"`
	AdapterSHA256          string          `json:"adapter_sha256"`
	ChildActionIDs         []metadata.UUID `json:"child_action_ids,omitempty"`
	AttemptIDs             []metadata.UUID `json:"attempt_ids,omitempty"`
	EvidenceDigests        []string        `json:"evidence_digests"`
}

type AttemptRecord struct {
	ActionID   metadata.UUID `json:"action_id"`
	AttemptID  metadata.UUID `json:"attempt_id"`
	Nonce      metadata.UUID `json:"nonce"`
	State      AttemptState  `json:"state"`
	PlanSHA256 string        `json:"plan_sha256"`
}

type StepRecord struct {
	StepID        string         `json:"step_id"`
	State         StepState      `json:"state"`
	Operation     OperationClass `json:"operation"`
	InputSHA256   string         `json:"input_sha256"`
	BeforeSHA256  string         `json:"before_sha256"`
	AppliedSHA256 string         `json:"applied_sha256"`
}

type ResourceRecord struct {
	ResourceID  string        `json:"resource_id"`
	Kind        ResourceKind  `json:"kind"`
	State       ResourceState `json:"state"`
	OwnerSHA256 string        `json:"owner_sha256"`
}

type OutcomeRecord struct {
	ActionID       metadata.UUID       `json:"action_id"`
	AttemptID      metadata.UUID       `json:"attempt_id"`
	Outcome        TerminalOutcome     `json:"outcome"`
	Reason         Reason              `json:"reason"`
	ReportDelivery ReportDeliveryState `json:"report_delivery"`
}

type IncidentRecord struct {
	IncidentID metadata.UUID `json:"incident_id"`
	Severity   Severity      `json:"severity"`
	Reason     Reason        `json:"reason"`
	Target     string        `json:"target"`
}

var (
	ErrInvalidContract = errors.New("invalid reconciliation contract")
	identifierPattern  = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,95}$`)
	versionPattern     = regexp.MustCompile(`^v[0-9]+(\.[0-9]+){0,2}$`)
)

func (provenance ActionProvenance) Validate(kind RecordKind) error {
	if provenance.Schema != ActionProvenanceSchema ||
		provenance.Kind != kind ||
		!kind.Valid() ||
		!validUUID(provenance.RecordID) ||
		(provenance.ParentActionID != "" && !validUUID(provenance.ParentActionID)) ||
		(provenance.RootActionID != "" && !validUUID(provenance.RootActionID)) ||
		!provenance.Producer.Valid() ||
		!provenance.Domain.Valid() ||
		!validUUID(provenance.BootID) ||
		provenance.BundleGeneration == 0 ||
		provenance.DomainPolicyGeneration == 0 ||
		provenance.ControlGeneration == 0 ||
		provenance.SnapshotGeneration == 0 ||
		!validDigest(provenance.SourceSHA256) ||
		!validDigest(provenance.InputSHA256) ||
		!validDigest(provenance.OutputSHA256) ||
		!canonicalTime(provenance.ObservedAt) ||
		provenance.SourceMonotonicNS < 0 {
		return ErrInvalidContract
	}
	if kind != RecordOperationSession && provenance.RootActionID == "" {
		return ErrInvalidContract
	}
	return nil
}

func (payload ReadinessRecord) Validate() error {
	if !validIdentifier(payload.Target) || !payload.Status.Valid() ||
		!payload.Reason.Valid() || !payload.RetryClass.Valid() ||
		!validRetry(payload.RetryClass, payload.RetryAfterSeconds) {
		return ErrInvalidContract
	}
	return nil
}

func (payload AcknowledgementRecord) Validate() error {
	if !validUUID(payload.RequestID) || !payload.Class.Valid() ||
		!payload.Reason.Valid() || !payload.RetryClass.Valid() ||
		!validRetry(payload.RetryClass, payload.RetryAfterSeconds) ||
		(payload.ActionID != "" && !validUUID(payload.ActionID)) {
		return ErrInvalidContract
	}
	switch payload.Class {
	case AckAccepted:
		if payload.RetryClass != RetryNone || payload.RetryAfterSeconds != 0 ||
			(!payload.NoAction && payload.ActionID == "") {
			return ErrInvalidContract
		}
	case AckTemporarilyRejected:
		if payload.RetryClass != RetryAfterHint || payload.ActionID != "" || payload.NoAction {
			return ErrInvalidContract
		}
	case AckDenied:
		if payload.RetryClass != RetryNever || payload.RetryAfterSeconds != 0 ||
			payload.ActionID != "" || payload.NoAction {
			return ErrInvalidContract
		}
	}
	return nil
}

func (payload ActionPlanRecord) Validate() error {
	if !validDigest(payload.PlanSHA256) || !validIdentifier(payload.Target) ||
		!capabilityIDPattern.MatchString(string(payload.CapabilityID)) ||
		hasProductionFragment(string(payload.CapabilityID)) ||
		!versionPattern.MatchString(payload.AdapterVersion) ||
		!validDigestList(payload.StepDigests, 1, MaxPlanSteps) ||
		!validDigestList(payload.VerificationDigests, 1, MaxPlanSteps) ||
		!validDigestList(payload.CompensationDigests, 1, MaxPlanSteps) ||
		len(payload.StepDigests) != len(payload.VerificationDigests) ||
		len(payload.StepDigests) != len(payload.CompensationDigests) {
		return ErrInvalidContract
	}
	return nil
}

func (payload OperationSessionRecord) Validate() error {
	if !validUUID(payload.OperationID) || !payload.Workflow.Valid() ||
		!payload.Lifecycle.Valid() || !versionPattern.MatchString(payload.ContractVersion) ||
		!versionPattern.MatchString(payload.RuntimeVersion) ||
		!validDigest(payload.ManifestSHA256) ||
		!validUUIDList(payload.ChildActionIDs, MaxDigestReferences) {
		return ErrInvalidContract
	}
	return nil
}

func (payload CheckpointRecord) Validate() error {
	if !validUUID(payload.OperationID) || payload.Sequence == 0 ||
		(payload.ParentCheckpointSHA256 != "" && !validDigest(payload.ParentCheckpointSHA256)) ||
		!validDigest(payload.ReducerSHA256) ||
		!validDigest(payload.AdapterSHA256) ||
		!validUUIDList(payload.ChildActionIDs, MaxDigestReferences) ||
		!validUUIDList(payload.AttemptIDs, MaxDigestReferences) ||
		!validDigestList(payload.EvidenceDigests, 1, MaxDigestReferences) {
		return ErrInvalidContract
	}
	return nil
}

func (payload AttemptRecord) Validate() error {
	if !validUUID(payload.ActionID) || !validUUID(payload.AttemptID) ||
		!validUUID(payload.Nonce) || !payload.State.Valid() ||
		!validDigest(payload.PlanSHA256) ||
		payload.ActionID == payload.AttemptID ||
		payload.ActionID == payload.Nonce ||
		payload.AttemptID == payload.Nonce {
		return ErrInvalidContract
	}
	return nil
}

func (payload StepRecord) Validate() error {
	if !validIdentifier(payload.StepID) || !payload.State.Valid() ||
		!payload.Operation.valid() || !validDigest(payload.InputSHA256) ||
		!validDigest(payload.BeforeSHA256) || !validDigest(payload.AppliedSHA256) {
		return ErrInvalidContract
	}
	return nil
}

func (payload ResourceRecord) Validate() error {
	if !validIdentifier(payload.ResourceID) || !payload.Kind.Valid() ||
		!payload.State.Valid() || !validDigest(payload.OwnerSHA256) {
		return ErrInvalidContract
	}
	return nil
}

func (payload OutcomeRecord) Validate() error {
	if !validUUID(payload.ActionID) || !validUUID(payload.AttemptID) ||
		payload.ActionID == payload.AttemptID || !payload.Outcome.Valid() ||
		!payload.Reason.Valid() || !payload.ReportDelivery.Valid() {
		return ErrInvalidContract
	}
	return nil
}

func (payload IncidentRecord) Validate() error {
	if !validUUID(payload.IncidentID) || !payload.Severity.Valid() ||
		!payload.Reason.Valid() || !validIdentifier(payload.Target) {
		return ErrInvalidContract
	}
	return nil
}

func (kind RecordKind) Valid() bool {
	switch kind {
	case RecordReadiness, RecordAcknowledgement, RecordActionPlan, RecordOperationSession,
		RecordCheckpoint, RecordAttempt, RecordStep, RecordResource, RecordOutcome, RecordIncident:
		return true
	default:
		return false
	}
}

func (producer Producer) Valid() bool {
	return producer == ProducerRootDaemon || producer == ProducerUserDaemon || producer == ProducerSynthetic
}

func (status ReadinessStatus) Valid() bool {
	return status == ReadinessReady || status == ReadinessTemporarilyBlocked || status == ReadinessDenied
}

func (class AckClass) Valid() bool {
	return class == AckAccepted || class == AckTemporarilyRejected || class == AckDenied
}

func (class RetryClass) Valid() bool {
	return class == RetryNone || class == RetryAfterHint || class == RetryNever
}

func (reason Reason) Valid() bool {
	switch reason {
	case ReasonNone, ReasonNoAction, ReasonAccepted, ReasonFreshness, ReasonThreshold, ReasonBudget,
		ReasonBackoff, ReasonCooldown, ReasonPolicy, ReasonCapability, ReasonOwnership, ReasonSchema,
		ReasonGeneration, ReasonTarget, ReasonLineage, ReasonResumeDenied, ReasonVerification,
		ReasonCompensation, ReasonCleanup, ReasonExpired, ReasonCancelled, ReasonSafeMode,
		ReasonTelemetryPending, ReasonTelemetryRejected, ReasonGapUnrecoverable:
		return true
	default:
		return false
	}
}

func (state AttemptState) Valid() bool {
	switch state {
	case AttemptPending, AttemptClaimed, AttemptRunning, AttemptVerifying, AttemptCommitted,
		AttemptExpired, AttemptDenied, AttemptCancelled, AttemptRolledBack, AttemptFailed, AttemptSafeMode:
		return true
	default:
		return false
	}
}

func (outcome TerminalOutcome) Valid() bool {
	switch outcome {
	case OutcomeCommitted, OutcomeExpired, OutcomeDenied, OutcomeCancelled, OutcomeRolledBack, OutcomeFailed, OutcomeSafeMode:
		return true
	default:
		return false
	}
}

func (state ReportDeliveryState) Valid() bool {
	return state == ReportPending || state == ReportAcknowledged || state == ReportTerminallyRejected
}

func (lifecycle SessionLifecycle) Valid() bool {
	return lifecycle == SessionRunning || lifecycle == SessionSuspended ||
		lifecycle == SessionCancelled || lifecycle == SessionFailed || lifecycle == SessionCompleted
}

func (state StepState) Valid() bool {
	return state == StepPending || state == StepApplied || state == StepVerified ||
		state == StepCompensated || state == StepFailed || state == StepSkippedNoop
}

func (kind ResourceKind) Valid() bool {
	return kind == ResourceSyntheticHelper || kind == ResourceSyntheticFile || kind == ResourceSyntheticLease
}

func (state ResourceState) Valid() bool {
	return state == ResourceRegistered || state == ResourceClosed || state == ResourceFailed
}

func (workflow WorkflowKind) Valid() bool {
	return workflow == WorkflowSyntheticQualification ||
		workflow == WorkflowShadowComparison ||
		workflow == WorkflowCrashRecoveryDrill
}

func (severity Severity) Valid() bool {
	return severity == SeverityInfo || severity == SeverityWarning || severity == SeverityCritical
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= MaxIdentifierBytes &&
		identifierPattern.MatchString(value) &&
		!hasProtectedString(value)
}

func validRetry(class RetryClass, seconds uint32) bool {
	switch class {
	case RetryNone, RetryNever:
		return seconds == 0
	case RetryAfterHint:
		return seconds > 0 && seconds <= MaxRetryAfterSeconds
	default:
		return false
	}
}

func validDigestList(values []string, minimum, maximum int) bool {
	if len(values) < minimum || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validDigest(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validUUIDList(values []metadata.UUID, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[metadata.UUID]struct{}, len(values))
	for _, value := range values {
		if !validUUID(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func hasProtectedString(value string) bool {
	normalized := strings.ToLower(value)
	for _, fragment := range []string{
		"hexroute_canary_", "credential", "endpoint", "gitlab", "keychain", "medvidi",
		"otp", "path", "pin", "pritunl", "process", "selector", "session", "totp",
		"topology", "twilight", "vless",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
