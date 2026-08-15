package reconciler

import (
	"errors"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type OperationCheckpointInput struct {
	OperationID     metadata.UUID    `json:"operation_id"`
	Domain          policy.Domain    `json:"domain"`
	Binding         SnapshotBinding  `json:"binding"`
	Workflow        WorkflowKind     `json:"workflow"`
	Lifecycle       SessionLifecycle `json:"lifecycle"`
	ContractVersion string           `json:"contract_version"`
	RuntimeVersion  string           `json:"runtime_version"`
	ManifestSHA256  string           `json:"manifest_sha256"`
	ReducerSHA256   string           `json:"reducer_sha256"`
	AdapterSHA256   string           `json:"adapter_sha256"`
	ChildActionIDs  []metadata.UUID  `json:"child_action_ids,omitempty"`
	AttemptIDs      []metadata.UUID  `json:"attempt_ids,omitempty"`
	EvidenceDigests []string         `json:"evidence_digests"`
}

type OperationCheckpointEnvelope struct {
	Sequence         uint64                 `json:"sequence"`
	Domain           policy.Domain          `json:"domain"`
	Binding          SnapshotBinding        `json:"binding"`
	Session          OperationSessionRecord `json:"session"`
	Checkpoint       CheckpointRecord       `json:"checkpoint"`
	CheckpointSHA256 string                 `json:"checkpoint_sha256"`
}

type operationCheckpointDigestInput struct {
	Sequence   uint64                 `json:"sequence"`
	Domain     policy.Domain          `json:"domain"`
	Binding    SnapshotBinding        `json:"binding"`
	Session    OperationSessionRecord `json:"session"`
	Checkpoint CheckpointRecord       `json:"checkpoint"`
}

type OperationResumeRequest struct {
	OperationID              metadata.UUID   `json:"operation_id"`
	ResumeID                 metadata.UUID   `json:"resume_id"`
	Domain                   policy.Domain   `json:"domain"`
	Binding                  SnapshotBinding `json:"binding"`
	ManifestSHA256           string          `json:"manifest_sha256"`
	ContractVersion          string          `json:"contract_version"`
	RuntimeVersion           string          `json:"runtime_version"`
	ExpectedSequence         uint64          `json:"expected_sequence"`
	ExpectedCheckpointSHA256 string          `json:"expected_checkpoint_sha256"`
	OwnerAttemptID           metadata.UUID   `json:"owner_attempt_id,omitempty"`
	AllowSuspended           bool            `json:"allow_suspended"`
}

type OperationResumeDecision struct {
	Accepted  bool             `json:"accepted"`
	Reason    Reason           `json:"reason"`
	Lifecycle SessionLifecycle `json:"lifecycle"`
}

type ReplayGateOutcome string

const (
	ReplayApproved    ReplayGateOutcome = "approved"
	ReplayRejected    ReplayGateOutcome = "rejected"
	ReplayTimeout     ReplayGateOutcome = "timeout"
	ReplayChangedPlan ReplayGateOutcome = "changed_plan"
)

type ReplayContinuationRecord struct {
	OperationID          metadata.UUID     `json:"operation_id"`
	RequestID            metadata.UUID     `json:"request_id"`
	CheckpointSHA256     string            `json:"checkpoint_sha256"`
	PlanSHA256           string            `json:"plan_sha256"`
	Outcome              ReplayGateOutcome `json:"outcome"`
	Reason               Reason            `json:"reason"`
	SessionLifecycle     SessionLifecycle  `json:"session_lifecycle"`
	ActionStateUnchanged bool              `json:"action_state_unchanged"`
}

type resumeClaim struct {
	resumeID         metadata.UUID
	checkpointSHA256 string
	sequence         uint64
}

type MemoryOperationCheckpointStore struct {
	mu           sync.Mutex
	checkpoints  map[metadata.UUID][]OperationCheckpointEnvelope
	resumeClaims map[metadata.UUID]resumeClaim
}

var (
	ErrOperationCheckpoint       = errors.New("invalid operation checkpoint")
	ErrOperationCheckpointCAS    = errors.New("operation checkpoint compare-and-swap failed")
	ErrOperationCheckpointTamper = errors.New("operation checkpoint tamper")
	ErrOperationResume           = errors.New("invalid operation resume request")
)

func NewMemoryOperationCheckpointStore() *MemoryOperationCheckpointStore {
	return &MemoryOperationCheckpointStore{
		checkpoints:  make(map[metadata.UUID][]OperationCheckpointEnvelope),
		resumeClaims: make(map[metadata.UUID]resumeClaim),
	}
}

func (store *MemoryOperationCheckpointStore) AppendCheckpoint(input OperationCheckpointInput) (OperationCheckpointEnvelope, error) {
	if store == nil || input.validate() != nil {
		return OperationCheckpointEnvelope{}, ErrOperationCheckpoint
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries := store.checkpoints[input.OperationID]
	if len(entries) > 0 {
		if err := verifyOperationCheckpoints(entries); err != nil {
			return OperationCheckpointEnvelope{}, err
		}
		latest := entries[len(entries)-1]
		if latest.Domain != input.Domain ||
			latest.Binding != input.Binding ||
			latest.Session.OperationID != input.OperationID ||
			!allowedSessionTransition(latest.Session.Lifecycle, input.Lifecycle) {
			return OperationCheckpointEnvelope{}, ErrOperationCheckpointCAS
		}
	} else if input.Lifecycle != SessionRunning {
		return OperationCheckpointEnvelope{}, ErrOperationCheckpointCAS
	}
	sequence := uint64(len(entries) + 1)
	parent := ""
	if len(entries) > 0 {
		parent = entries[len(entries)-1].CheckpointSHA256
	}
	envelope, err := newOperationCheckpointEnvelope(input, sequence, parent)
	if err != nil {
		return OperationCheckpointEnvelope{}, err
	}
	store.checkpoints[input.OperationID] = append(entries, envelope)
	return envelope, nil
}

func (store *MemoryOperationCheckpointStore) LatestCheckpoint(
	operationID metadata.UUID,
) (OperationCheckpointEnvelope, bool, error) {
	if store == nil || !validUUID(operationID) {
		return OperationCheckpointEnvelope{}, false, ErrOperationCheckpoint
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries := store.checkpoints[operationID]
	if len(entries) == 0 {
		return OperationCheckpointEnvelope{}, false, nil
	}
	if err := verifyOperationCheckpoints(entries); err != nil {
		return OperationCheckpointEnvelope{}, false, err
	}
	return entries[len(entries)-1], true, nil
}

func (store *MemoryOperationCheckpointStore) ValidateResume(request OperationResumeRequest) (OperationResumeDecision, error) {
	if store == nil || request.validate() != nil {
		return OperationResumeDecision{}, ErrOperationResume
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.validateResumeLocked(request)
}

func (store *MemoryOperationCheckpointStore) ClaimResume(request OperationResumeRequest) (OperationResumeDecision, error) {
	if store == nil || request.validate() != nil {
		return OperationResumeDecision{}, ErrOperationResume
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	decision, err := store.validateResumeLocked(request)
	if err != nil || !decision.Accepted {
		return decision, err
	}
	claim, exists := store.resumeClaims[request.OperationID]
	next := resumeClaim{
		resumeID:         request.ResumeID,
		checkpointSHA256: request.ExpectedCheckpointSHA256,
		sequence:         request.ExpectedSequence,
	}
	if exists && claim != next {
		return OperationResumeDecision{
			Accepted: false, Reason: ReasonOwnership, Lifecycle: decision.Lifecycle,
		}, nil
	}
	store.resumeClaims[request.OperationID] = next
	return decision, nil
}

func NewReplayContinuationRecord(
	operationID metadata.UUID,
	requestID metadata.UUID,
	checkpointSHA256 string,
	planSHA256 string,
	outcome ReplayGateOutcome,
) (ReplayContinuationRecord, error) {
	record := ReplayContinuationRecord{
		OperationID:          operationID,
		RequestID:            requestID,
		CheckpointSHA256:     checkpointSHA256,
		PlanSHA256:           planSHA256,
		Outcome:              outcome,
		ActionStateUnchanged: true,
	}
	switch outcome {
	case ReplayApproved:
		record.Reason = ReasonAccepted
		record.SessionLifecycle = SessionRunning
	case ReplayRejected:
		record.Reason = ReasonResumeDenied
		record.SessionLifecycle = SessionCancelled
	case ReplayTimeout:
		record.Reason = ReasonExpired
		record.SessionLifecycle = SessionFailed
	case ReplayChangedPlan:
		record.Reason = ReasonLineage
		record.SessionLifecycle = SessionSuspended
	default:
		return ReplayContinuationRecord{}, ErrOperationResume
	}
	if record.Validate() != nil {
		return ReplayContinuationRecord{}, ErrOperationResume
	}
	return record, nil
}

func (store *MemoryOperationCheckpointStore) validateResumeLocked(
	request OperationResumeRequest,
) (OperationResumeDecision, error) {
	entries := store.checkpoints[request.OperationID]
	if len(entries) == 0 {
		return OperationResumeDecision{Accepted: false, Reason: ReasonLineage}, nil
	}
	if err := verifyOperationCheckpoints(entries); err != nil {
		return OperationResumeDecision{}, err
	}
	latest := entries[len(entries)-1]
	decision := OperationResumeDecision{Lifecycle: latest.Session.Lifecycle}
	switch {
	case latest.Session.Lifecycle == SessionSuspended && !request.AllowSuspended:
		decision.Reason = ReasonResumeDenied
	case terminalSessionLifecycle(latest.Session.Lifecycle):
		decision.Reason = ReasonResumeDenied
	case latest.Domain != request.Domain || latest.Binding != request.Binding:
		decision.Reason = ReasonGeneration
	case latest.Session.ManifestSHA256 != request.ManifestSHA256 ||
		latest.Session.ContractVersion != request.ContractVersion ||
		latest.Session.RuntimeVersion != request.RuntimeVersion:
		decision.Reason = ReasonLineage
	case latest.Sequence != request.ExpectedSequence ||
		latest.CheckpointSHA256 != request.ExpectedCheckpointSHA256:
		decision.Reason = ReasonLineage
	case len(latest.Checkpoint.AttemptIDs) > 0 &&
		(request.OwnerAttemptID == "" || !uuidInSlice(request.OwnerAttemptID, latest.Checkpoint.AttemptIDs)):
		decision.Reason = ReasonOwnership
	default:
		decision.Accepted = true
		decision.Reason = ReasonAccepted
	}
	return decision, nil
}

func newOperationCheckpointEnvelope(
	input OperationCheckpointInput,
	sequence uint64,
	parentCheckpointSHA256 string,
) (OperationCheckpointEnvelope, error) {
	session := OperationSessionRecord{
		OperationID:     input.OperationID,
		Workflow:        input.Workflow,
		Lifecycle:       input.Lifecycle,
		ContractVersion: input.ContractVersion,
		RuntimeVersion:  input.RuntimeVersion,
		ManifestSHA256:  input.ManifestSHA256,
		ChildActionIDs:  append([]metadata.UUID(nil), input.ChildActionIDs...),
	}
	checkpoint := CheckpointRecord{
		OperationID:            input.OperationID,
		Sequence:               sequence,
		ParentCheckpointSHA256: parentCheckpointSHA256,
		ReducerSHA256:          input.ReducerSHA256,
		AdapterSHA256:          input.AdapterSHA256,
		ChildActionIDs:         append([]metadata.UUID(nil), input.ChildActionIDs...),
		AttemptIDs:             append([]metadata.UUID(nil), input.AttemptIDs...),
		EvidenceDigests:        append([]string(nil), input.EvidenceDigests...),
	}
	envelope := OperationCheckpointEnvelope{
		Sequence: sequence, Domain: input.Domain, Binding: input.Binding,
		Session: session, Checkpoint: checkpoint,
	}
	digest, err := operationCheckpointDigest(envelope)
	if err != nil {
		return OperationCheckpointEnvelope{}, err
	}
	envelope.CheckpointSHA256 = digest
	if envelope.validate() != nil {
		return OperationCheckpointEnvelope{}, ErrOperationCheckpoint
	}
	return envelope, nil
}

func operationCheckpointDigest(envelope OperationCheckpointEnvelope) (string, error) {
	if envelope.Sequence == 0 ||
		!envelope.Domain.Valid() ||
		envelope.Binding.validate() != nil ||
		envelope.Session.Validate() != nil ||
		envelope.Checkpoint.Validate() != nil ||
		envelope.Session.OperationID != envelope.Checkpoint.OperationID ||
		envelope.Sequence != envelope.Checkpoint.Sequence {
		return "", ErrOperationCheckpoint
	}
	encoded, err := policy.MarshalCanonical(operationCheckpointDigestInput{
		Sequence:   envelope.Sequence,
		Domain:     envelope.Domain,
		Binding:    envelope.Binding,
		Session:    envelope.Session,
		Checkpoint: envelope.Checkpoint,
	})
	if err != nil {
		return "", ErrOperationCheckpoint
	}
	return policy.SHA256Hex(encoded), nil
}

func verifyOperationCheckpoints(entries []OperationCheckpointEnvelope) error {
	var previous *OperationCheckpointEnvelope
	for index := range entries {
		entry := entries[index]
		if entry.validate() != nil {
			return ErrOperationCheckpointTamper
		}
		if previous == nil {
			if entry.Sequence != 1 || entry.Checkpoint.ParentCheckpointSHA256 != "" ||
				entry.Session.Lifecycle != SessionRunning {
				return ErrOperationCheckpointTamper
			}
		} else if entry.Sequence != previous.Sequence+1 ||
			entry.Checkpoint.ParentCheckpointSHA256 != previous.CheckpointSHA256 ||
			entry.Domain != previous.Domain ||
			entry.Binding != previous.Binding ||
			entry.Session.OperationID != previous.Session.OperationID ||
			!allowedSessionTransition(previous.Session.Lifecycle, entry.Session.Lifecycle) {
			return ErrOperationCheckpointTamper
		}
		previous = &entries[index]
	}
	return nil
}

func (input OperationCheckpointInput) validate() error {
	if !validUUID(input.OperationID) ||
		!input.Domain.Valid() ||
		input.Binding.validate() != nil ||
		!input.Workflow.Valid() ||
		!input.Lifecycle.Valid() ||
		!versionPattern.MatchString(input.ContractVersion) ||
		!versionPattern.MatchString(input.RuntimeVersion) ||
		!validDigest(input.ManifestSHA256) ||
		!validDigest(input.ReducerSHA256) ||
		!validDigest(input.AdapterSHA256) ||
		!validUUIDList(input.ChildActionIDs, MaxDigestReferences) ||
		!validUUIDList(input.AttemptIDs, MaxDigestReferences) ||
		!validDigestList(input.EvidenceDigests, 1, MaxDigestReferences) {
		return ErrOperationCheckpoint
	}
	return nil
}

func (envelope OperationCheckpointEnvelope) validate() error {
	if envelope.Sequence == 0 ||
		!envelope.Domain.Valid() ||
		envelope.Binding.validate() != nil ||
		envelope.Session.Validate() != nil ||
		envelope.Checkpoint.Validate() != nil ||
		!validDigest(envelope.CheckpointSHA256) ||
		envelope.Sequence != envelope.Checkpoint.Sequence ||
		envelope.Session.OperationID != envelope.Checkpoint.OperationID ||
		!sameUUIDSet(envelope.Session.ChildActionIDs, envelope.Checkpoint.ChildActionIDs) {
		return ErrOperationCheckpoint
	}
	digest, err := operationCheckpointDigest(envelope)
	if err != nil || digest != envelope.CheckpointSHA256 {
		return ErrOperationCheckpoint
	}
	return nil
}

func (request OperationResumeRequest) validate() error {
	if !validUUID(request.OperationID) ||
		!validUUID(request.ResumeID) ||
		!request.Domain.Valid() ||
		request.Binding.validate() != nil ||
		!validDigest(request.ManifestSHA256) ||
		!versionPattern.MatchString(request.ContractVersion) ||
		!versionPattern.MatchString(request.RuntimeVersion) ||
		request.ExpectedSequence == 0 ||
		!validDigest(request.ExpectedCheckpointSHA256) ||
		(request.OwnerAttemptID != "" && !validUUID(request.OwnerAttemptID)) {
		return ErrOperationResume
	}
	return nil
}

func (record ReplayContinuationRecord) Validate() error {
	if !validUUID(record.OperationID) ||
		!validUUID(record.RequestID) ||
		!validDigest(record.CheckpointSHA256) ||
		!validDigest(record.PlanSHA256) ||
		!record.Outcome.Valid() ||
		!record.Reason.Valid() ||
		!record.SessionLifecycle.Valid() ||
		!record.ActionStateUnchanged {
		return ErrOperationResume
	}
	return nil
}

func (outcome ReplayGateOutcome) Valid() bool {
	return outcome == ReplayApproved ||
		outcome == ReplayRejected ||
		outcome == ReplayTimeout ||
		outcome == ReplayChangedPlan
}

func allowedSessionTransition(from, to SessionLifecycle) bool {
	if terminalSessionLifecycle(from) {
		return false
	}
	switch from {
	case SessionRunning:
		return to == SessionRunning ||
			to == SessionSuspended ||
			to == SessionCancelled ||
			to == SessionFailed ||
			to == SessionCompleted
	case SessionSuspended:
		return to == SessionRunning ||
			to == SessionCancelled ||
			to == SessionFailed
	default:
		return false
	}
}

func terminalSessionLifecycle(lifecycle SessionLifecycle) bool {
	return lifecycle == SessionCancelled ||
		lifecycle == SessionFailed ||
		lifecycle == SessionCompleted
}

func uuidInSlice(value metadata.UUID, values []metadata.UUID) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sameUUIDSet(left, right []metadata.UUID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
