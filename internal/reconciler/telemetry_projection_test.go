package reconciler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActionEvidenceProjectionIsRedactedAndBounded(t *testing.T) {
	record, err := NewActionRecord(testProvenance(RecordOutcome), OutcomeRecord{
		ActionID:       testActionID,
		AttemptID:      testAttemptID,
		Outcome:        OutcomeRolledBack,
		Reason:         ReasonCancelled,
		ReportDelivery: ReportPending,
	})
	if err != nil {
		t.Fatalf("NewActionRecord() error = %v", err)
	}
	projection, err := ProjectActionEvidence(record, FreshnessCurrent)
	if err != nil {
		t.Fatalf("ProjectActionEvidence() error = %v", err)
	}
	if projection.RecordKind != RecordOutcome ||
		projection.Outcome != OutcomeRolledBack ||
		projection.Reason != ReasonCancelled ||
		projection.ReportDelivery != ReportPending ||
		projection.CorrelationSHA256 != record.RecordSHA256 {
		t.Fatalf("projection = %+v", projection)
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal(projection) error = %v", err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"action_id", "attempt_id", "command", "argument", "credential",
		"endpoint", "keychain", "path", "process_output", "selector",
		"session", "target", "topology", "totp",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("projection leaked forbidden field fragment %q: %s", forbidden, encoded)
		}
	}
}

func TestReportDeliveryPersistsIndependentlyFromOutcome(t *testing.T) {
	outcome := OutcomeRecord{
		ActionID:       testActionID,
		AttemptID:      testAttemptID,
		Outcome:        OutcomeCommitted,
		Reason:         ReasonAccepted,
		ReportDelivery: ReportPending,
	}
	store := &MemoryReportDeliveryStore{}
	if err := store.RecordPending(outcome); err != nil {
		t.Fatalf("RecordPending() error = %v", err)
	}
	acknowledged, err := store.SetDelivery(
		testActionID,
		testAttemptID,
		ReportAcknowledged,
	)
	if err != nil {
		t.Fatalf("SetDelivery(acknowledged) error = %v", err)
	}
	if acknowledged.Outcome != outcome.Outcome ||
		acknowledged.Reason != outcome.Reason ||
		acknowledged.ReportDelivery != ReportAcknowledged {
		t.Fatalf("acknowledged delivery = %+v", acknowledged)
	}
	if outcome.ReportDelivery != ReportPending {
		t.Fatalf("local outcome mutated: %+v", outcome)
	}
	rejected, err := store.SetDelivery(
		testActionID,
		testAttemptID,
		ReportTerminallyRejected,
	)
	if err != nil {
		t.Fatalf("SetDelivery(terminally rejected) error = %v", err)
	}
	if rejected.Outcome != OutcomeCommitted ||
		rejected.Reason != ReasonAccepted ||
		rejected.ReportDelivery != ReportTerminallyRejected {
		t.Fatalf("terminal delivery = %+v", rejected)
	}
}
