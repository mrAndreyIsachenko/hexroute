package reconciler

import (
	"errors"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const ActionEvidenceProjectionSchema = "hexroute.reconciler-action-evidence-projection.v1"

type FreshnessBucket string

const (
	FreshnessCurrent FreshnessBucket = "current"
	FreshnessStale   FreshnessBucket = "stale"
	FreshnessUnknown FreshnessBucket = "unknown"
)

type ActionEvidenceProjection struct {
	Schema                 string              `json:"schema"`
	RecordKind             RecordKind          `json:"record_kind"`
	BundleGeneration       uint64              `json:"bundle_generation"`
	DomainPolicyGeneration uint64              `json:"domain_policy_generation"`
	ControlGeneration      uint64              `json:"control_generation"`
	SnapshotGeneration     uint64              `json:"snapshot_generation"`
	Freshness              FreshnessBucket     `json:"freshness"`
	CorrelationSHA256      string              `json:"correlation_sha256"`
	RecordSHA256           string              `json:"record_sha256"`
	Reason                 Reason              `json:"reason"`
	RetryClass             RetryClass          `json:"retry_class,omitempty"`
	Readiness              ReadinessStatus     `json:"readiness,omitempty"`
	Acknowledgement        AckClass            `json:"acknowledgement,omitempty"`
	Lifecycle              AttemptState        `json:"lifecycle,omitempty"`
	Outcome                TerminalOutcome     `json:"outcome,omitempty"`
	ReportDelivery         ReportDeliveryState `json:"report_delivery,omitempty"`
}

type ReportDeliveryRecord struct {
	ActionID       metadata.UUID       `json:"action_id"`
	AttemptID      metadata.UUID       `json:"attempt_id"`
	Outcome        TerminalOutcome     `json:"outcome"`
	Reason         Reason              `json:"reason"`
	ReportDelivery ReportDeliveryState `json:"report_delivery"`
}

type MemoryReportDeliveryStore struct {
	mu      sync.Mutex
	records map[string]ReportDeliveryRecord
}

var (
	ErrInvalidActionEvidence = errors.New("invalid action evidence projection")
	ErrReportDeliveryState   = errors.New("invalid report delivery state")
)

func ProjectActionEvidence(
	record ActionRecord,
	freshness FreshnessBucket,
) (ActionEvidenceProjection, error) {
	if record.Schema != ActionRecordSchema ||
		record.Provenance.Validate(record.Provenance.Kind) != nil ||
		!freshness.Valid() ||
		!validDigest(record.RecordSHA256) {
		return ActionEvidenceProjection{}, ErrInvalidActionEvidence
	}
	projection := ActionEvidenceProjection{
		Schema:                 ActionEvidenceProjectionSchema,
		RecordKind:             record.Provenance.Kind,
		BundleGeneration:       record.Provenance.BundleGeneration,
		DomainPolicyGeneration: record.Provenance.DomainPolicyGeneration,
		ControlGeneration:      record.Provenance.ControlGeneration,
		SnapshotGeneration:     record.Provenance.SnapshotGeneration,
		Freshness:              freshness,
		CorrelationSHA256:      record.RecordSHA256,
		RecordSHA256:           record.RecordSHA256,
		Reason:                 ReasonNone,
	}
	switch payload := record.Payload.(type) {
	case ReadinessRecord:
		projection.Readiness = payload.Status
		projection.Reason = payload.Reason
		projection.RetryClass = payload.RetryClass
	case AcknowledgementRecord:
		projection.Acknowledgement = payload.Class
		projection.Reason = payload.Reason
		projection.RetryClass = payload.RetryClass
	case AttemptRecord:
		projection.Lifecycle = payload.State
	case OutcomeRecord:
		projection.Outcome = payload.Outcome
		projection.Reason = payload.Reason
		projection.ReportDelivery = payload.ReportDelivery
	default:
		return ActionEvidenceProjection{}, ErrInvalidActionEvidence
	}
	if projection.Validate() != nil {
		return ActionEvidenceProjection{}, ErrInvalidActionEvidence
	}
	return projection, nil
}

func (projection ActionEvidenceProjection) Validate() error {
	if projection.Schema != ActionEvidenceProjectionSchema ||
		!projection.RecordKind.Valid() ||
		projection.BundleGeneration == 0 ||
		projection.DomainPolicyGeneration == 0 ||
		projection.ControlGeneration == 0 ||
		projection.SnapshotGeneration == 0 ||
		!projection.Freshness.Valid() ||
		!validDigest(projection.CorrelationSHA256) ||
		!validDigest(projection.RecordSHA256) ||
		!projection.Reason.Valid() {
		return ErrInvalidActionEvidence
	}
	switch projection.RecordKind {
	case RecordReadiness:
		if !projection.Readiness.Valid() || !projection.RetryClass.Valid() ||
			projection.Acknowledgement != "" || projection.Lifecycle != "" ||
			projection.Outcome != "" || projection.ReportDelivery != "" {
			return ErrInvalidActionEvidence
		}
	case RecordAcknowledgement:
		if !projection.Acknowledgement.Valid() || !projection.RetryClass.Valid() ||
			projection.Readiness != "" || projection.Lifecycle != "" ||
			projection.Outcome != "" || projection.ReportDelivery != "" {
			return ErrInvalidActionEvidence
		}
	case RecordAttempt:
		if !projection.Lifecycle.Valid() || projection.Readiness != "" ||
			projection.Acknowledgement != "" || projection.Outcome != "" ||
			projection.ReportDelivery != "" || projection.RetryClass != "" {
			return ErrInvalidActionEvidence
		}
	case RecordOutcome:
		if !projection.Outcome.Valid() || !projection.ReportDelivery.Valid() ||
			projection.Readiness != "" || projection.Acknowledgement != "" ||
			projection.Lifecycle != "" || projection.RetryClass != "" {
			return ErrInvalidActionEvidence
		}
	default:
		return ErrInvalidActionEvidence
	}
	return nil
}

func (bucket FreshnessBucket) Valid() bool {
	return bucket == FreshnessCurrent || bucket == FreshnessStale || bucket == FreshnessUnknown
}

func (store *MemoryReportDeliveryStore) RecordPending(outcome OutcomeRecord) error {
	if outcome.Validate() != nil || outcome.ReportDelivery != ReportPending {
		return ErrReportDeliveryState
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.records == nil {
		store.records = make(map[string]ReportDeliveryRecord)
	}
	key := reportDeliveryKey(outcome.ActionID, outcome.AttemptID)
	if _, exists := store.records[key]; exists {
		return ErrReportDeliveryState
	}
	store.records[key] = ReportDeliveryRecord{
		ActionID:       outcome.ActionID,
		AttemptID:      outcome.AttemptID,
		Outcome:        outcome.Outcome,
		Reason:         outcome.Reason,
		ReportDelivery: outcome.ReportDelivery,
	}
	return nil
}

func (store *MemoryReportDeliveryStore) SetDelivery(
	actionID metadata.UUID,
	attemptID metadata.UUID,
	state ReportDeliveryState,
) (ReportDeliveryRecord, error) {
	if !validUUID(actionID) || !validUUID(attemptID) ||
		actionID == attemptID ||
		(state != ReportAcknowledged && state != ReportTerminallyRejected) {
		return ReportDeliveryRecord{}, ErrReportDeliveryState
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, exists := store.records[reportDeliveryKey(actionID, attemptID)]
	if !exists {
		return ReportDeliveryRecord{}, ErrReportDeliveryState
	}
	record.ReportDelivery = state
	store.records[reportDeliveryKey(actionID, attemptID)] = record
	return record, nil
}

func reportDeliveryKey(actionID metadata.UUID, attemptID metadata.UUID) string {
	return string(actionID) + "/" + string(attemptID)
}
