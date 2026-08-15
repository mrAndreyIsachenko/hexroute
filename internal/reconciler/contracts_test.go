package reconciler

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	testActionID    metadata.UUID = "11111111-1111-4111-8111-111111111111"
	testAttemptID   metadata.UUID = "22222222-2222-4222-8222-222222222222"
	testBootID      metadata.UUID = "33333333-3333-4333-8333-333333333333"
	testRecordID    metadata.UUID = "44444444-4444-4444-8444-444444444444"
	testRequestID   metadata.UUID = "55555555-5555-4555-8555-555555555555"
	testNonce       metadata.UUID = "66666666-6666-4666-8666-666666666666"
	testOperationID metadata.UUID = "77777777-7777-4777-8777-777777777777"
)

func TestVersionedContractPayloadsValidate(t *testing.T) {
	for name, test := range map[string]struct {
		kind    RecordKind
		payload any
	}{
		"readiness": {
			RecordReadiness,
			ReadinessRecord{Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone},
		},
		"acknowledgement": {
			RecordAcknowledgement,
			AcknowledgementRecord{RequestID: testRequestID, Class: AckAccepted, Reason: ReasonNoAction, RetryClass: RetryNone, NoAction: true},
		},
		"action plan": {
			RecordActionPlan,
			ActionPlanRecord{
				PlanSHA256:             testDigest("plan"),
				Target:                 "synthetic.target",
				CapabilityID:           CapabilitySyntheticMemory,
				AdapterVersion:         "v1.0.0",
				AdapterSHA256:          testDigest("adapter"),
				ProposalSHA256:         testDigest("proposal"),
				DiffSHA256:             testDigest("diff"),
				SnapshotSHA256:         testDigest("snapshot"),
				ReadinessSHA256:        testDigest("readiness"),
				BundleGeneration:       2,
				DomainPolicyGeneration: 2,
				ControlGeneration:      1,
				SnapshotGeneration:     1,
				StepDigests:            []string{testDigest("step")},
				VerificationDigests:    []string{testDigest("verify")},
				CompensationDigests:    []string{testDigest("compensate")},
			},
		},
		"operation session": {
			RecordOperationSession,
			OperationSessionRecord{
				OperationID: testOperationID, Workflow: WorkflowSyntheticQualification, Lifecycle: SessionRunning,
				ContractVersion: "v1.0.0", RuntimeVersion: "v1.0.0", ManifestSHA256: testDigest("manifest"),
				ChildActionIDs: []metadata.UUID{testActionID},
			},
		},
		"checkpoint": {
			RecordCheckpoint,
			CheckpointRecord{
				OperationID: testOperationID, Sequence: 1, ReducerSHA256: testDigest("reducer"), AdapterSHA256: testDigest("adapter"),
				ChildActionIDs: []metadata.UUID{testActionID}, AttemptIDs: []metadata.UUID{testAttemptID},
				EvidenceDigests: []string{testDigest("evidence")},
			},
		},
		"attempt": {
			RecordAttempt,
			AttemptRecord{ActionID: testActionID, AttemptID: testAttemptID, Nonce: testNonce, State: AttemptPending, PlanSHA256: testDigest("plan")},
		},
		"step": {
			RecordStep,
			StepRecord{StepID: "synthetic.step", State: StepApplied, Operation: OperationSyntheticState, InputSHA256: testDigest("input"), BeforeSHA256: testDigest("before"), AppliedSHA256: testDigest("after")},
		},
		"resource": {
			RecordResource,
			ResourceRecord{ResourceID: "synthetic.resource", Kind: ResourceSyntheticHelper, State: ResourceRegistered, OwnerSHA256: testDigest("owner")},
		},
		"outcome": {
			RecordOutcome,
			OutcomeRecord{ActionID: testActionID, AttemptID: testAttemptID, Outcome: OutcomeCommitted, Reason: ReasonAccepted, ReportDelivery: ReportPending},
		},
		"incident": {
			RecordIncident,
			IncidentRecord{IncidentID: testRecordID, Severity: SeverityWarning, Reason: ReasonSafeMode, Target: "synthetic.target"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePayload(test.kind, test.payload); err != nil {
				t.Fatalf("validatePayload() error = %v", err)
			}
			record, err := NewActionRecord(testProvenance(test.kind), test.payload)
			if err != nil {
				t.Fatalf("NewActionRecord() error = %v", err)
			}
			if _, _, err := EncodeActionRecord(record); err != nil {
				t.Fatalf("EncodeActionRecord() error = %v", err)
			}
		})
	}
}

func TestAllowlistedEnumsRejectRawRuntimeStrings(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"reason", ReadinessRecord{Target: "synthetic.target", Status: ReadinessDenied, Reason: Reason("dial tcp timeout"), RetryClass: RetryNever}.Validate()},
		{"retry", ReadinessRecord{Target: "synthetic.target", Status: ReadinessTemporarilyBlocked, Reason: ReasonCooldown, RetryClass: RetryClass("http_503"), RetryAfterSeconds: 30}.Validate()},
		{"outcome", OutcomeRecord{ActionID: testActionID, AttemptID: testAttemptID, Outcome: TerminalOutcome("panic"), Reason: ReasonVerification, ReportDelivery: ReportPending}.Validate()},
		{"ack", AcknowledgementRecord{RequestID: testRequestID, Class: AckClass("ok"), Reason: ReasonAccepted, RetryClass: RetryNone}.Validate()},
	}
	for _, test := range tests {
		if !errors.Is(test.err, ErrInvalidContract) {
			t.Fatalf("%s validation error = %v, want %v", test.name, test.err, ErrInvalidContract)
		}
	}
}

func TestBoundsRejectOversizedCountsAndStrings(t *testing.T) {
	tooManyDigests := make([]string, MaxPlanSteps+1)
	for index := range tooManyDigests {
		tooManyDigests[index] = testDigest(string(rune('a' + index)))
	}
	if err := (ActionPlanRecord{
		PlanSHA256:             testDigest("plan"),
		Target:                 "synthetic.target",
		CapabilityID:           CapabilitySyntheticMemory,
		AdapterVersion:         "v1.0.0",
		AdapterSHA256:          testDigest("adapter"),
		ProposalSHA256:         testDigest("proposal"),
		DiffSHA256:             testDigest("diff"),
		SnapshotSHA256:         testDigest("snapshot"),
		ReadinessSHA256:        testDigest("readiness"),
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		StepDigests:            tooManyDigests,
		VerificationDigests:    tooManyDigests,
		CompensationDigests:    tooManyDigests,
	}).Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("oversized plan error = %v, want %v", err, ErrInvalidContract)
	}
	if err := (ReadinessRecord{
		Target: stringsOf("a", MaxIdentifierBytes+1), Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
	}).Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("oversized target error = %v, want %v", err, ErrInvalidContract)
	}
	if err := (ReadinessRecord{
		Target: "synthetic.target", Status: ReadinessTemporarilyBlocked, Reason: ReasonCooldown,
		RetryClass: RetryAfterHint, RetryAfterSeconds: MaxRetryAfterSeconds + 1,
	}).Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("oversized retry error = %v, want %v", err, ErrInvalidContract)
	}
}

func testProvenance(kind RecordKind) ActionProvenance {
	root := testActionID
	if kind == RecordOperationSession {
		root = ""
	}
	return ActionProvenance{
		Schema:                 ActionProvenanceSchema,
		RecordID:               testRecordID,
		Kind:                   kind,
		RootActionID:           root,
		Producer:               ProducerSynthetic,
		Domain:                 policy.DomainRoot,
		BootID:                 testBootID,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		SourceSHA256:           testDigest("source"),
		InputSHA256:            testDigest("input"),
		OutputSHA256:           testDigest("output"),
		ObservedAt:             "2026-08-15T12:00:00Z",
		SourceMonotonicNS:      1,
	}
}

func testDigest(value string) string {
	return policy.SHA256Hex([]byte(value))
}

func stringsOf(value string, count int) string {
	var out string
	for index := 0; index < count; index++ {
		out += value
	}
	return out
}
