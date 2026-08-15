package reconciler

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const SyntheticTraceSchema = "hexroute.reconciler-synthetic-trace.v1"

type SyntheticTraceScenario string

const (
	TraceNoop                    SyntheticTraceScenario = "noop"
	TraceAcknowledgementAccept   SyntheticTraceScenario = "acknowledgement_accept"
	TraceAcknowledgementBlock    SyntheticTraceScenario = "acknowledgement_block"
	TraceAcknowledgementDeny     SyntheticTraceScenario = "acknowledgement_deny"
	TraceOperationResumeAccept   SyntheticTraceScenario = "operation_resume_accept"
	TraceOperationResumeReject   SyntheticTraceScenario = "operation_resume_reject"
	TraceExpiry                  SyntheticTraceScenario = "expiry"
	TraceCancellationBeforeApply SyntheticTraceScenario = "cancellation_before_apply"
	TraceCancellationAfterApply  SyntheticTraceScenario = "cancellation_after_apply"
	TraceCompensation            SyntheticTraceScenario = "compensation"
	TraceGenerationChange        SyntheticTraceScenario = "generation_change"
	TraceCrashAfterClaim         SyntheticTraceScenario = "crash_after_claim"
	TraceCrashAfterApply         SyntheticTraceScenario = "crash_after_apply"
	TraceVerificationMismatch    SyntheticTraceScenario = "verification_mismatch"
	TraceMissingStateRehydrate   SyntheticTraceScenario = "missing_state_rehydrate"
	TraceForeignConflict         SyntheticTraceScenario = "foreign_conflict"
)

type SyntheticTrace struct {
	Schema              string                        `json:"schema"`
	Scenario            SyntheticTraceScenario        `json:"scenario"`
	Records             []ActionRecord                `json:"records"`
	AttemptJournal      []AttemptJournalEntry         `json:"attempt_journal,omitempty"`
	Checkpoints         []OperationCheckpointEnvelope `json:"checkpoints,omitempty"`
	ReplayContinuations []ReplayContinuationRecord    `json:"replay_continuations,omitempty"`
	ResumeDecision      *OperationResumeDecision      `json:"resume_decision,omitempty"`
	StartupRecovery     *StartupRecovery              `json:"startup_recovery,omitempty"`
	SyntheticDiff       *SyntheticDiff                `json:"synthetic_diff,omitempty"`
	Expected            SyntheticTraceExpectation     `json:"expected"`
	TraceSHA256         string                        `json:"trace_sha256"`
}

type SyntheticTraceExpectation struct {
	Readiness            ReadinessStatus  `json:"readiness,omitempty"`
	Acknowledgement      AckClass         `json:"acknowledgement,omitempty"`
	Reason               Reason           `json:"reason"`
	RetryClass           RetryClass       `json:"retry_class,omitempty"`
	AttemptState         AttemptState     `json:"attempt_state,omitempty"`
	TerminalOutcome      TerminalOutcome  `json:"terminal_outcome,omitempty"`
	SessionLifecycle     SessionLifecycle `json:"session_lifecycle,omitempty"`
	PlanSHA256           string           `json:"plan_sha256,omitempty"`
	ActionStateUnchanged bool             `json:"action_state_unchanged,omitempty"`
}

type syntheticTraceDigestInput struct {
	Schema              string                        `json:"schema"`
	Scenario            SyntheticTraceScenario        `json:"scenario"`
	Records             []ActionRecord                `json:"records"`
	AttemptJournal      []AttemptJournalEntry         `json:"attempt_journal,omitempty"`
	Checkpoints         []OperationCheckpointEnvelope `json:"checkpoints,omitempty"`
	ReplayContinuations []ReplayContinuationRecord    `json:"replay_continuations,omitempty"`
	ResumeDecision      *OperationResumeDecision      `json:"resume_decision,omitempty"`
	StartupRecovery     *StartupRecovery              `json:"startup_recovery,omitempty"`
	SyntheticDiff       *SyntheticDiff                `json:"synthetic_diff,omitempty"`
	Expected            SyntheticTraceExpectation     `json:"expected"`
}

type traceBuilder struct {
	scenario SyntheticTraceScenario
	binding  SnapshotBinding
	sequence int
	now      time.Time
}

var ErrSyntheticTrace = errors.New("invalid synthetic trace")

func CanonicalSyntheticTraces() ([]SyntheticTrace, error) {
	scenarios := []SyntheticTraceScenario{
		TraceNoop,
		TraceAcknowledgementAccept,
		TraceAcknowledgementBlock,
		TraceAcknowledgementDeny,
		TraceOperationResumeAccept,
		TraceOperationResumeReject,
		TraceExpiry,
		TraceCancellationBeforeApply,
		TraceCancellationAfterApply,
		TraceCompensation,
		TraceGenerationChange,
		TraceCrashAfterClaim,
		TraceCrashAfterApply,
		TraceVerificationMismatch,
		TraceMissingStateRehydrate,
		TraceForeignConflict,
	}
	traces := make([]SyntheticTrace, 0, len(scenarios))
	for _, scenario := range scenarios {
		trace, err := BuildCanonicalSyntheticTrace(scenario)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, nil
}

func BuildCanonicalSyntheticTrace(scenario SyntheticTraceScenario) (SyntheticTrace, error) {
	if !scenario.Valid() {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	builder := newTraceBuilder(scenario)
	switch scenario {
	case TraceNoop:
		return builder.noop()
	case TraceAcknowledgementAccept:
		return builder.acceptedAction()
	case TraceAcknowledgementBlock:
		return builder.temporaryAcknowledgement()
	case TraceAcknowledgementDeny:
		return builder.deniedAcknowledgement(ReasonPolicy)
	case TraceOperationResumeAccept:
		return builder.operationResume(true)
	case TraceOperationResumeReject:
		return builder.operationResume(false)
	case TraceExpiry:
		return builder.expiredAttempt()
	case TraceCancellationBeforeApply:
		return builder.cancelBeforeApply()
	case TraceCancellationAfterApply:
		return builder.cancelAfterApply()
	case TraceCompensation:
		return builder.compensation()
	case TraceGenerationChange:
		return builder.generationChange()
	case TraceCrashAfterClaim:
		return builder.crashAfterClaim()
	case TraceCrashAfterApply:
		return builder.crashAfterApply()
	case TraceVerificationMismatch:
		return builder.verificationMismatch()
	case TraceMissingStateRehydrate:
		return builder.missingStateRehydrate()
	case TraceForeignConflict:
		return builder.foreignConflict()
	default:
		return SyntheticTrace{}, ErrSyntheticTrace
	}
}

func (trace SyntheticTrace) Validate() error {
	if trace.Schema != SyntheticTraceSchema ||
		!trace.Scenario.Valid() ||
		len(trace.Records) == 0 ||
		trace.Expected.Validate() != nil ||
		!validDigest(trace.TraceSHA256) {
		return ErrSyntheticTrace
	}
	for _, record := range trace.Records {
		if _, _, err := EncodeActionRecord(record); err != nil {
			return ErrSyntheticTrace
		}
	}
	for _, entry := range trace.AttemptJournal {
		if entry.validate() != nil {
			return ErrSyntheticTrace
		}
	}
	if len(trace.AttemptJournal) > 0 && verifyAttemptEntries(trace.AttemptJournal) != nil {
		return ErrSyntheticTrace
	}
	for _, checkpoint := range trace.Checkpoints {
		if checkpoint.validate() != nil {
			return ErrSyntheticTrace
		}
	}
	if len(trace.Checkpoints) > 0 && verifyOperationCheckpoints(trace.Checkpoints) != nil {
		return ErrSyntheticTrace
	}
	for _, continuation := range trace.ReplayContinuations {
		if continuation.Validate() != nil {
			return ErrSyntheticTrace
		}
	}
	if trace.ResumeDecision != nil &&
		(!trace.ResumeDecision.Reason.Valid() || !trace.ResumeDecision.Lifecycle.Valid()) {
		return ErrSyntheticTrace
	}
	if trace.StartupRecovery != nil &&
		(!trace.StartupRecovery.Class.Valid() || !trace.StartupRecovery.Reason.Valid()) {
		return ErrSyntheticTrace
	}
	if trace.SyntheticDiff != nil && trace.SyntheticDiff.validate() != nil {
		return ErrSyntheticTrace
	}
	digest, err := syntheticTraceDigest(trace)
	if err != nil || digest != trace.TraceSHA256 {
		return ErrSyntheticTrace
	}
	return nil
}

func (expectation SyntheticTraceExpectation) Validate() error {
	if !expectation.Reason.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.Readiness != "" && !expectation.Readiness.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.Acknowledgement != "" && !expectation.Acknowledgement.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.RetryClass != "" && !expectation.RetryClass.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.AttemptState != "" && !expectation.AttemptState.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.TerminalOutcome != "" && !expectation.TerminalOutcome.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.SessionLifecycle != "" && !expectation.SessionLifecycle.Valid() {
		return ErrSyntheticTrace
	}
	if expectation.PlanSHA256 != "" && !validDigest(expectation.PlanSHA256) {
		return ErrSyntheticTrace
	}
	return nil
}

func (scenario SyntheticTraceScenario) Valid() bool {
	switch scenario {
	case TraceNoop, TraceAcknowledgementAccept, TraceAcknowledgementBlock, TraceAcknowledgementDeny,
		TraceOperationResumeAccept, TraceOperationResumeReject, TraceExpiry,
		TraceCancellationBeforeApply, TraceCancellationAfterApply, TraceCompensation, TraceGenerationChange,
		TraceCrashAfterClaim, TraceCrashAfterApply, TraceVerificationMismatch, TraceMissingStateRehydrate,
		TraceForeignConflict:
		return true
	default:
		return false
	}
}

func (class StartupRecoveryClass) Valid() bool {
	return class == RecoveryTerminal ||
		class == RecoveryUntouched ||
		class == RecoveryVerifyOwned ||
		class == RecoverySafeMode ||
		class == RecoveryBlocked
}

func newTraceBuilder(scenario SyntheticTraceScenario) traceBuilder {
	return traceBuilder{
		scenario: scenario,
		binding: SnapshotBinding{
			BootID:                 deterministicTraceUUID(scenario, "boot"),
			BundleGeneration:       2,
			DomainPolicyGeneration: 2,
			ControlGeneration:      1,
			SnapshotGeneration:     1,
			SourceWatermark:        10,
		},
		now: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
	}
}

func (builder *traceBuilder) noop() (SyntheticTrace, error) {
	readiness := ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	}
	acknowledgement := AcknowledgementRecord{
		RequestID: deterministicTraceUUID(builder.scenario, "request"),
		Class:     AckAccepted, Reason: ReasonNoAction, RetryClass: RetryNone, NoAction: true,
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordReadiness, payload: readiness},
		payloadEnvelope{kind: RecordAcknowledgement, payload: acknowledgement},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:   SyntheticTraceSchema,
		Scenario: builder.scenario,
		Records:  records,
		Expected: SyntheticTraceExpectation{
			Readiness: ReadinessReady, Acknowledgement: AckAccepted,
			Reason: ReasonNoAction, RetryClass: RetryNone, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) acceptedAction() (SyntheticTrace, error) {
	translation, err := builder.translation(ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	}, false)
	if err != nil {
		return SyntheticTrace{}, err
	}
	binding := builder.attemptBinding(translation.Plan.PlanSHA256)
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(
		journal,
		binding,
		[]attemptTransition{
			{to: AttemptPending, reason: ReasonAccepted},
			{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
			{from: AttemptClaimed, to: AttemptRunning, reason: ReasonAccepted},
			{from: AttemptRunning, to: AttemptVerifying, reason: ReasonVerification},
			{from: AttemptVerifying, to: AttemptCommitted, reason: ReasonAccepted},
		},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	step := translation.Step
	outcome := OutcomeRecord{
		ActionID: binding.ActionID, AttemptID: binding.AttemptID,
		Outcome: OutcomeCommitted, Reason: ReasonAccepted, ReportDelivery: ReportPending,
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordReadiness, payload: translation.Readiness},
		payloadEnvelope{kind: RecordAcknowledgement, payload: translation.Result.Acknowledgement},
		payloadEnvelope{kind: RecordActionPlan, payload: *translation.Plan},
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[2].Attempt},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepApplied, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}},
		payloadEnvelope{kind: RecordAttempt, payload: entries[3].Attempt},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepVerified, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}},
		payloadEnvelope{kind: RecordAttempt, payload: entries[4].Attempt},
		payloadEnvelope{kind: RecordOutcome, payload: outcome},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:         SyntheticTraceSchema,
		Scenario:       builder.scenario,
		Records:        records,
		AttemptJournal: entries,
		Expected: SyntheticTraceExpectation{
			Readiness: ReadinessReady, Acknowledgement: AckAccepted,
			Reason: ReasonAccepted, RetryClass: RetryNone, AttemptState: AttemptCommitted,
			TerminalOutcome: OutcomeCommitted, PlanSHA256: translation.Plan.PlanSHA256,
		},
	})
}

func (builder *traceBuilder) temporaryAcknowledgement() (SyntheticTrace, error) {
	readiness := ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessTemporarilyBlocked,
		Reason: ReasonCooldown, RetryClass: RetryAfterHint, RetryAfterSeconds: 120,
	}
	translation, err := builder.translation(readiness, false)
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordReadiness, payload: readiness},
		payloadEnvelope{kind: RecordAcknowledgement, payload: translation.Result.Acknowledgement},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:   SyntheticTraceSchema,
		Scenario: builder.scenario,
		Records:  records,
		Expected: SyntheticTraceExpectation{
			Readiness: ReadinessTemporarilyBlocked, Acknowledgement: AckTemporarilyRejected,
			Reason: ReasonCooldown, RetryClass: RetryAfterHint, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) deniedAcknowledgement(reason Reason) (SyntheticTrace, error) {
	readiness := ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessDenied, Reason: reason, RetryClass: RetryNever,
	}
	translation, err := builder.translation(readiness, false)
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordReadiness, payload: readiness},
		payloadEnvelope{kind: RecordAcknowledgement, payload: translation.Result.Acknowledgement},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:   SyntheticTraceSchema,
		Scenario: builder.scenario,
		Records:  records,
		Expected: SyntheticTraceExpectation{
			Readiness: ReadinessDenied, Acknowledgement: AckDenied,
			Reason: reason, RetryClass: RetryNever, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) operationResume(accepted bool) (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	store := NewMemoryOperationCheckpointStore()
	checkpoint, err := store.AppendCheckpoint(OperationCheckpointInput{
		OperationID:     deterministicTraceUUID(builder.scenario, "operation"),
		Domain:          policy.DomainRoot,
		Binding:         builder.binding,
		Workflow:        WorkflowSyntheticQualification,
		Lifecycle:       SessionRunning,
		ContractVersion: "v1.0.0",
		RuntimeVersion:  "v1.0.0",
		ManifestSHA256:  traceDigest(builder.scenario, "manifest"),
		ReducerSHA256:   traceDigest(builder.scenario, "reducer"),
		AdapterSHA256:   traceDigest(builder.scenario, "adapter"),
		ChildActionIDs:  []metadata.UUID{binding.ActionID},
		AttemptIDs:      []metadata.UUID{binding.AttemptID},
		EvidenceDigests: []string{traceDigest(builder.scenario, "evidence")},
	})
	if err != nil {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	request := OperationResumeRequest{
		OperationID:              checkpoint.Session.OperationID,
		ResumeID:                 deterministicTraceUUID(builder.scenario, "resume"),
		Domain:                   policy.DomainRoot,
		Binding:                  builder.binding,
		ManifestSHA256:           checkpoint.Session.ManifestSHA256,
		ContractVersion:          checkpoint.Session.ContractVersion,
		RuntimeVersion:           checkpoint.Session.RuntimeVersion,
		ExpectedSequence:         checkpoint.Sequence,
		ExpectedCheckpointSHA256: checkpoint.CheckpointSHA256,
		OwnerAttemptID:           binding.AttemptID,
	}
	if !accepted {
		request.ExpectedSequence++
	}
	decision, err := store.ValidateResume(request)
	if err != nil {
		return SyntheticTrace{}, err
	}
	continuationOutcome := ReplayApproved
	if !accepted {
		continuationOutcome = ReplayChangedPlan
	}
	continuation, err := NewReplayContinuationRecord(
		checkpoint.Session.OperationID,
		deterministicTraceUUID(builder.scenario, "continuation-request"),
		checkpoint.CheckpointSHA256,
		binding.PlanSHA256,
		continuationOutcome,
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordOperationSession, payload: checkpoint.Session},
		payloadEnvelope{kind: RecordCheckpoint, payload: checkpoint.Checkpoint},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	expectedReason := ReasonAccepted
	expectedLifecycle := SessionRunning
	if !accepted {
		expectedReason = ReasonLineage
		expectedLifecycle = SessionSuspended
	}
	return builder.finalize(SyntheticTrace{
		Schema:              SyntheticTraceSchema,
		Scenario:            builder.scenario,
		Records:             records,
		Checkpoints:         []OperationCheckpointEnvelope{checkpoint},
		ReplayContinuations: []ReplayContinuationRecord{continuation},
		ResumeDecision:      &decision,
		Expected: SyntheticTraceExpectation{
			Reason: expectedReason, SessionLifecycle: expectedLifecycle,
			PlanSHA256: binding.PlanSHA256, ActionStateUnchanged: !accepted,
		},
	})
}

func (builder *traceBuilder) expiredAttempt() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptExpired, reason: ReasonExpired},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	outcome := OutcomeRecord{
		ActionID: binding.ActionID, AttemptID: binding.AttemptID,
		Outcome: OutcomeExpired, Reason: ReasonExpired, ReportDelivery: ReportPending,
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
		payloadEnvelope{kind: RecordOutcome, payload: outcome},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:         SyntheticTraceSchema,
		Scenario:       builder.scenario,
		Records:        records,
		AttemptJournal: entries,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonExpired, AttemptState: AttemptExpired, TerminalOutcome: OutcomeExpired,
		},
	})
}

func (builder *traceBuilder) cancelBeforeApply() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	store := NewMemoryCancellationIntentStore()
	registry := NewMemorySyntheticResourceRegistry()
	if _, err := RequestCancellation(journal, store, binding.ActionID, deterministicTraceUUID(builder.scenario, "cancel")); err != nil {
		return SyntheticTrace{}, err
	}
	resolution, err := ResolveCancellation(journal, store, registry, nil, binding, nil)
	if err != nil {
		return SyntheticTrace{}, err
	}
	entries = append(entries, resolution.Attempt)
	records, err := builder.records(
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: resolution.Attempt.Attempt},
		payloadEnvelope{kind: RecordOutcome, payload: resolution.Outcome},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:         SyntheticTraceSchema,
		Scenario:       builder.scenario,
		Records:        records,
		AttemptJournal: entries,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonCancelled, AttemptState: AttemptCancelled, TerminalOutcome: OutcomeCancelled,
			ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) cancelAfterApply() (SyntheticTrace, error) {
	return builder.cancelWithAppliedPrefix(false)
}

func (builder *traceBuilder) compensation() (SyntheticTrace, error) {
	return builder.cancelWithAppliedPrefix(true)
}

func (builder *traceBuilder) cancelWithAppliedPrefix(twoSteps bool) (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	adapter, err := NewMemorySyntheticAdapter(nil)
	if err != nil {
		return SyntheticTrace{}, err
	}
	desired := []SyntheticDesiredResource{{
		ID: "synthetic.alpha", Operation: OperationSyntheticState,
		InputSHA256: traceDigest(builder.scenario, "input-alpha"),
		StateSHA256: traceDigest(builder.scenario, "state-alpha"),
	}}
	if twoSteps {
		desired = append(desired, SyntheticDesiredResource{
			ID: "synthetic.beta", Operation: OperationSyntheticState,
			InputSHA256: traceDigest(builder.scenario, "input-beta"),
			StateSHA256: traceDigest(builder.scenario, "state-beta"),
		})
	}
	diff, err := adapter.SemanticCompare(SyntheticDesiredState{
		Fresh: true, Authorized: true, Owner: binding, Resources: desired,
	})
	if err != nil || len(diff.Steps) != len(desired) {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	for _, step := range diff.Steps {
		if _, err := adapter.Apply(step); err != nil {
			return SyntheticTrace{}, err
		}
		if err := adapter.Verify(step); err != nil {
			return SyntheticTrace{}, err
		}
	}
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
		{from: AttemptClaimed, to: AttemptRunning, reason: ReasonAccepted},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	store := NewMemoryCancellationIntentStore()
	registry := NewMemorySyntheticResourceRegistry()
	if _, err := RequestCancellation(journal, store, binding.ActionID, deterministicTraceUUID(builder.scenario, "cancel")); err != nil {
		return SyntheticTrace{}, err
	}
	resolution, err := ResolveCancellation(journal, store, registry, adapter, binding, diff.Steps)
	if err != nil {
		return SyntheticTrace{}, err
	}
	entries = append(entries, resolution.Attempt)
	payloads := []payloadEnvelope{
		{kind: RecordAttempt, payload: entries[0].Attempt},
		{kind: RecordAttempt, payload: entries[1].Attempt},
		{kind: RecordAttempt, payload: entries[2].Attempt},
	}
	for _, step := range diff.Steps {
		payloads = append(payloads, payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepApplied, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}})
	}
	for index := len(diff.Steps) - 1; index >= 0; index-- {
		step := diff.Steps[index]
		payloads = append(payloads, payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepCompensated, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}})
	}
	payloads = append(payloads,
		payloadEnvelope{kind: RecordAttempt, payload: resolution.Attempt.Attempt},
		payloadEnvelope{kind: RecordOutcome, payload: resolution.Outcome},
	)
	records, err := builder.records(payloads...)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:         SyntheticTraceSchema,
		Scenario:       builder.scenario,
		Records:        records,
		AttemptJournal: entries,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonCompensation, AttemptState: AttemptRolledBack, TerminalOutcome: OutcomeRolledBack,
		},
	})
}

func (builder *traceBuilder) generationChange() (SyntheticTrace, error) {
	readiness := ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessDenied, Reason: ReasonGeneration, RetryClass: RetryNever,
	}
	input, err := builder.translationInput(readiness, false)
	if err != nil {
		return SyntheticTrace{}, err
	}
	input.CurrentSnapshotBinding.ControlGeneration++
	result, err := TranslateProposal(input)
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordReadiness, payload: readiness},
		payloadEnvelope{kind: RecordAcknowledgement, payload: result.Acknowledgement},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:   SyntheticTraceSchema,
		Scenario: builder.scenario,
		Records:  records,
		Expected: SyntheticTraceExpectation{
			Readiness: ReadinessDenied, Acknowledgement: AckDenied,
			Reason: ReasonGeneration, RetryClass: RetryNever, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) crashAfterClaim() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	recovery, err := ClassifyStartupAttempt(entries[1], StartupObservation{
		BootID: binding.BootID, AttemptID: binding.AttemptID, TargetState: TargetUntouched,
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:          SyntheticTraceSchema,
		Scenario:        builder.scenario,
		Records:         records,
		AttemptJournal:  entries,
		StartupRecovery: &recovery,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonAccepted, AttemptState: AttemptClaimed, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) crashAfterApply() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	step := builder.syntheticStep(binding, "alpha", syntheticMissingStateSHA256(), traceDigest(builder.scenario, "after-alpha"))
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
		{from: AttemptClaimed, to: AttemptRunning, reason: ReasonAccepted},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	recovery, err := ClassifyStartupAttempt(entries[2], StartupObservation{
		BootID: binding.BootID, AttemptID: binding.AttemptID, TargetState: TargetUncertain,
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[2].Attempt},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepApplied, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:          SyntheticTraceSchema,
		Scenario:        builder.scenario,
		Records:         records,
		AttemptJournal:  entries,
		StartupRecovery: &recovery,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonSafeMode, AttemptState: AttemptRunning, ActionStateUnchanged: true,
		},
	})
}

func (builder *traceBuilder) verificationMismatch() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	step := builder.syntheticStep(binding, "alpha", syntheticMissingStateSHA256(), traceDigest(builder.scenario, "after-alpha"))
	journal := NewMemoryAttemptJournal()
	entries, err := appendAttemptStates(journal, binding, []attemptTransition{
		{to: AttemptPending, reason: ReasonAccepted},
		{from: AttemptPending, to: AttemptClaimed, reason: ReasonAccepted},
		{from: AttemptClaimed, to: AttemptRunning, reason: ReasonAccepted},
		{from: AttemptRunning, to: AttemptVerifying, reason: ReasonVerification},
		{from: AttemptVerifying, to: AttemptFailed, reason: ReasonVerification},
	})
	if err != nil {
		return SyntheticTrace{}, err
	}
	outcome := OutcomeRecord{
		ActionID: binding.ActionID, AttemptID: binding.AttemptID,
		Outcome: OutcomeFailed, Reason: ReasonVerification, ReportDelivery: ReportPending,
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordAttempt, payload: entries[0].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[1].Attempt},
		payloadEnvelope{kind: RecordAttempt, payload: entries[2].Attempt},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepApplied, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: step.AppliedSHA256,
		}},
		payloadEnvelope{kind: RecordAttempt, payload: entries[3].Attempt},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: step.ID, State: StepFailed, Operation: step.Operation,
			InputSHA256: step.InputSHA256, BeforeSHA256: step.BeforeSHA256, AppliedSHA256: traceDigest(builder.scenario, "unexpected-after"),
		}},
		payloadEnvelope{kind: RecordAttempt, payload: entries[4].Attempt},
		payloadEnvelope{kind: RecordOutcome, payload: outcome},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:         SyntheticTraceSchema,
		Scenario:       builder.scenario,
		Records:        records,
		AttemptJournal: entries,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonVerification, AttemptState: AttemptFailed, TerminalOutcome: OutcomeFailed,
		},
	})
}

func (builder *traceBuilder) missingStateRehydrate() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan-placeholder"))
	adapter, err := NewMemorySyntheticAdapter(nil)
	if err != nil {
		return SyntheticTrace{}, err
	}
	diff, err := adapter.SemanticCompare(SyntheticDesiredState{
		Fresh: true, Authorized: true, Owner: binding,
		Resources: []SyntheticDesiredResource{{
			ID: "synthetic.alpha", Operation: OperationSyntheticState,
			InputSHA256: traceDigest(builder.scenario, "input-alpha"),
			StateSHA256: traceDigest(builder.scenario, "state-alpha"),
		}},
	})
	if err != nil || len(diff.Steps) != 1 || len(diff.Conflicts) != 0 ||
		diff.Steps[0].BeforeSHA256 != syntheticMissingStateSHA256() {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	input, err := builder.translationInput(ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	}, false)
	if err != nil {
		return SyntheticTrace{}, err
	}
	input.Steps = []TranslationStep{diff.Steps[0].TranslationStep()}
	result, err := TranslateProposal(input)
	if err != nil || result.Plan == nil {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	records, err := builder.records(
		payloadEnvelope{kind: RecordAcknowledgement, payload: result.Acknowledgement},
		payloadEnvelope{kind: RecordActionPlan, payload: *result.Plan},
		payloadEnvelope{kind: RecordStep, payload: StepRecord{
			StepID: diff.Steps[0].ID, State: StepPending, Operation: diff.Steps[0].Operation,
			InputSHA256: diff.Steps[0].InputSHA256, BeforeSHA256: diff.Steps[0].BeforeSHA256,
			AppliedSHA256: diff.Steps[0].AppliedSHA256,
		}},
	)
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:        SyntheticTraceSchema,
		Scenario:      builder.scenario,
		Records:       records,
		SyntheticDiff: &diff,
		Expected: SyntheticTraceExpectation{
			Acknowledgement: AckAccepted, Reason: ReasonAccepted, RetryClass: RetryNone,
			PlanSHA256: result.Plan.PlanSHA256,
		},
	})
}

func (builder *traceBuilder) foreignConflict() (SyntheticTrace, error) {
	binding := builder.attemptBinding(traceDigest(builder.scenario, "plan"))
	desired := SyntheticDesiredState{
		Fresh: true, Authorized: true, Owner: binding,
		Resources: []SyntheticDesiredResource{{
			ID: "synthetic.alpha", Operation: OperationSyntheticState,
			InputSHA256: traceDigest(builder.scenario, "input-alpha"),
			StateSHA256: traceDigest(builder.scenario, "state-alpha"),
		}},
	}
	current := SyntheticState{Resources: []SyntheticResource{{
		ID: "synthetic.alpha", Operation: OperationSyntheticState,
		StateSHA256: traceDigest(builder.scenario, "state-foreign"),
		Ownership:   SyntheticForeign,
	}}}
	diff, err := DiffSyntheticState(current, desired)
	if err != nil || len(diff.Steps) != 0 || len(diff.Conflicts) != 1 ||
		diff.Conflicts[0].Reason != ReasonOwnership {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	incident := IncidentRecord{
		IncidentID: deterministicTraceUUID(builder.scenario, "incident"),
		Severity:   SeverityWarning,
		Reason:     ReasonOwnership,
		Target:     "synthetic.target",
	}
	records, err := builder.records(payloadEnvelope{kind: RecordIncident, payload: incident})
	if err != nil {
		return SyntheticTrace{}, err
	}
	return builder.finalize(SyntheticTrace{
		Schema:        SyntheticTraceSchema,
		Scenario:      builder.scenario,
		Records:       records,
		SyntheticDiff: &diff,
		Expected: SyntheticTraceExpectation{
			Reason: ReasonOwnership, ActionStateUnchanged: true,
		},
	})
}

type traceTranslation struct {
	Readiness ReadinessRecord
	Result    TranslationResult
	Plan      *ActionPlanRecord
	Step      TranslationStep
}

func (builder *traceBuilder) translation(readiness ReadinessRecord, noop bool) (traceTranslation, error) {
	input, err := builder.translationInput(readiness, noop)
	if err != nil {
		return traceTranslation{}, err
	}
	result, err := TranslateProposal(input)
	if err != nil {
		return traceTranslation{}, err
	}
	return traceTranslation{
		Readiness: readiness, Result: result, Plan: result.Plan, Step: input.Steps[0],
	}, nil
}

func (builder *traceBuilder) translationInput(readiness ReadinessRecord, noop bool) (TranslationInput, error) {
	step := TranslationStep{
		ID:                 "synthetic.step",
		Operation:          OperationSyntheticState,
		InputSHA256:        traceDigest(builder.scenario, "input"),
		BeforeSHA256:       traceDigest(builder.scenario, "before"),
		AppliedSHA256:      traceDigest(builder.scenario, "after"),
		VerificationSHA256: traceDigest(builder.scenario, "verify"),
		CompensationSHA256: traceDigest(builder.scenario, "compensate"),
	}
	input := TranslationInput{
		DaemonDomain: policy.DomainRoot,
		Proposal: ProposalBinding{
			RequestID:       deterministicTraceUUID(builder.scenario, "request"),
			ActionID:        deterministicTraceUUID(builder.scenario, "action"),
			Target:          "synthetic.target",
			Domain:          policy.DomainRoot,
			CapabilityID:    CapabilitySyntheticMemory,
			ProposalSHA256:  traceDigest(builder.scenario, "proposal"),
			DiffSHA256:      traceDigest(builder.scenario, "diff"),
			SnapshotSHA256:  traceDigest(builder.scenario, "snapshot"),
			ReadinessSHA256: traceDigest(builder.scenario, "readiness"),
			SnapshotBinding: builder.binding,
		},
		CurrentSnapshotBinding: builder.binding,
		Readiness:              readiness,
		Registry:               DefaultSyntheticRegistry(),
		Adapter: AdapterMetadata{
			ID: "synthetic.adapter.memory", Version: "v1.0.0", SHA256: traceDigest(builder.scenario, "adapter"),
		},
		SemanticNoop: noop,
		Steps:        []TranslationStep{step},
	}
	if noop {
		input.Proposal.ActionID = ""
		input.Steps = nil
	}
	if input.validateCommon() != nil {
		return TranslationInput{}, ErrSyntheticTrace
	}
	return input, nil
}

func (builder *traceBuilder) attemptBinding(planSHA256 string) AttemptBinding {
	return AttemptBinding{
		ActionID:               deterministicTraceUUID(builder.scenario, "action"),
		AttemptID:              deterministicTraceUUID(builder.scenario, "attempt"),
		Nonce:                  deterministicTraceUUID(builder.scenario, "nonce"),
		Domain:                 policy.DomainRoot,
		Target:                 "synthetic.target",
		BootID:                 builder.binding.BootID,
		BundleGeneration:       builder.binding.BundleGeneration,
		DomainPolicyGeneration: builder.binding.DomainPolicyGeneration,
		ControlGeneration:      builder.binding.ControlGeneration,
		SnapshotGeneration:     builder.binding.SnapshotGeneration,
		PlanSHA256:             planSHA256,
	}
}

func (builder *traceBuilder) syntheticStep(
	binding AttemptBinding,
	label string,
	beforeSHA256 string,
	appliedSHA256 string,
) SyntheticPlanStep {
	return SyntheticPlanStep{
		ID:                 "synthetic.step" + label,
		ResourceID:         "synthetic." + label,
		Operation:          OperationSyntheticState,
		Owner:              binding,
		InputSHA256:        traceDigest(builder.scenario, "input-"+label),
		BeforeSHA256:       beforeSHA256,
		AppliedSHA256:      appliedSHA256,
		VerificationSHA256: traceDigest(builder.scenario, "verify-"+label),
		CompensationSHA256: traceDigest(builder.scenario, "compensate-"+label),
	}
}

type payloadEnvelope struct {
	kind    RecordKind
	payload any
}

func (builder *traceBuilder) records(items ...payloadEnvelope) ([]ActionRecord, error) {
	records := make([]ActionRecord, 0, len(items))
	for _, item := range items {
		record, err := NewActionRecord(builder.provenance(item.kind), item.payload)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (builder *traceBuilder) provenance(kind RecordKind) ActionProvenance {
	builder.sequence++
	rootActionID := deterministicTraceUUID(builder.scenario, "action")
	if kind == RecordOperationSession {
		rootActionID = ""
	}
	provenance := ActionProvenance{
		Schema:                 ActionProvenanceSchema,
		RecordID:               deterministicTraceUUID(builder.scenario, fmt.Sprintf("record-%02d-%s", builder.sequence, kind)),
		Kind:                   kind,
		RootActionID:           rootActionID,
		Producer:               ProducerSynthetic,
		Domain:                 policy.DomainRoot,
		BootID:                 builder.binding.BootID,
		BundleGeneration:       builder.binding.BundleGeneration,
		DomainPolicyGeneration: builder.binding.DomainPolicyGeneration,
		ControlGeneration:      builder.binding.ControlGeneration,
		SnapshotGeneration:     builder.binding.SnapshotGeneration,
		SourceSHA256:           traceDigest(builder.scenario, fmt.Sprintf("source-%02d", builder.sequence)),
		InputSHA256:            traceDigest(builder.scenario, fmt.Sprintf("input-%02d", builder.sequence)),
		OutputSHA256:           traceDigest(builder.scenario, fmt.Sprintf("output-%02d", builder.sequence)),
		ObservedAt:             builder.now.Add(time.Duration(builder.sequence) * time.Second).Format(time.RFC3339Nano),
		SourceMonotonicNS:      int64(builder.sequence) * int64(time.Second),
	}
	if kind == RecordStep || kind == RecordOutcome || kind == RecordAttempt || kind == RecordActionPlan {
		provenance.ParentActionID = rootActionID
	}
	return provenance
}

func (builder *traceBuilder) finalize(trace SyntheticTrace) (SyntheticTrace, error) {
	digest, err := syntheticTraceDigest(trace)
	if err != nil {
		return SyntheticTrace{}, err
	}
	trace.TraceSHA256 = digest
	if trace.Validate() != nil {
		return SyntheticTrace{}, ErrSyntheticTrace
	}
	return trace, nil
}

type attemptTransition struct {
	from   AttemptState
	to     AttemptState
	reason Reason
}

func appendAttemptStates(
	journal *MemoryAttemptJournal,
	binding AttemptBinding,
	transitions []attemptTransition,
) ([]AttemptJournalEntry, error) {
	entries := make([]AttemptJournalEntry, 0, len(transitions))
	for index, transition := range transitions {
		var (
			entry AttemptJournalEntry
			err   error
		)
		if index == 0 {
			if transition.to != AttemptPending {
				return nil, ErrSyntheticTrace
			}
			entry, err = journal.AppendPending(binding, transition.reason)
		} else {
			entry, err = journal.CompareAndSwap(binding, transition.from, transition.to, transition.reason)
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func syntheticTraceDigest(trace SyntheticTrace) (string, error) {
	if trace.Schema != SyntheticTraceSchema || !trace.Scenario.Valid() {
		return "", ErrSyntheticTrace
	}
	encoded, err := policy.MarshalCanonical(syntheticTraceDigestInput{
		Schema:              trace.Schema,
		Scenario:            trace.Scenario,
		Records:             trace.Records,
		AttemptJournal:      trace.AttemptJournal,
		Checkpoints:         trace.Checkpoints,
		ReplayContinuations: trace.ReplayContinuations,
		ResumeDecision:      trace.ResumeDecision,
		StartupRecovery:     trace.StartupRecovery,
		SyntheticDiff:       trace.SyntheticDiff,
		Expected:            trace.Expected,
	})
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(encoded), nil
}

func traceDigest(scenario SyntheticTraceScenario, label string) string {
	return policy.SHA256Hex([]byte("hexroute.synthetic.trace." + string(scenario) + "." + label))
}

func deterministicTraceUUID(scenario SyntheticTraceScenario, label string) metadata.UUID {
	sum := sha256.Sum256([]byte("hexroute.synthetic.trace." + string(scenario) + "." + label))
	raw := append([]byte(nil), sum[:16]...)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return metadata.UUID(fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]))
}
