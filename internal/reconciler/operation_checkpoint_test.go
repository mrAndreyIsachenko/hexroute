package reconciler

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestOperationCheckpointLifecycleAndResumeClaim(t *testing.T) {
	store := NewMemoryOperationCheckpointStore()
	first, err := store.AppendCheckpoint(checkpointInput(SessionRunning))
	if err != nil {
		t.Fatalf("AppendCheckpoint(first) error = %v", err)
	}
	if first.Sequence != 1 || first.Checkpoint.ParentCheckpointSHA256 != "" ||
		first.Session.Lifecycle != SessionRunning {
		t.Fatalf("first checkpoint = %+v", first)
	}

	request := resumeRequest(first)
	decision, err := store.ClaimResume(request)
	if err != nil {
		t.Fatalf("ClaimResume() error = %v", err)
	}
	if !decision.Accepted || decision.Reason != ReasonAccepted {
		t.Fatalf("decision = %+v", decision)
	}
	if decision, err = store.ClaimResume(request); err != nil || !decision.Accepted {
		t.Fatalf("idempotent ClaimResume() decision=%+v error=%v", decision, err)
	}
	competing := request
	competing.ResumeID = testRecordID
	decision, err = store.ClaimResume(competing)
	if err != nil {
		t.Fatalf("competing ClaimResume() error = %v", err)
	}
	if decision.Accepted || decision.Reason != ReasonOwnership {
		t.Fatalf("competing decision = %+v", decision)
	}

	suspended, err := store.AppendCheckpoint(checkpointInput(SessionSuspended))
	if err != nil {
		t.Fatalf("AppendCheckpoint(suspended) error = %v", err)
	}
	request = resumeRequest(suspended)
	decision, err = store.ValidateResume(request)
	if err != nil {
		t.Fatalf("ValidateResume(suspended) error = %v", err)
	}
	if decision.Accepted || decision.Reason != ReasonResumeDenied {
		t.Fatalf("suspended decision = %+v", decision)
	}
	request.AllowSuspended = true
	decision, err = store.ValidateResume(request)
	if err != nil {
		t.Fatalf("ValidateResume(allow suspended) error = %v", err)
	}
	if !decision.Accepted || decision.Lifecycle != SessionSuspended {
		t.Fatalf("allow suspended decision = %+v", decision)
	}
}

func TestOperationResumeRejectsDriftAndOwnerMismatch(t *testing.T) {
	store := NewMemoryOperationCheckpointStore()
	checkpoint, err := store.AppendCheckpoint(checkpointInput(SessionRunning))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*OperationResumeRequest)
		reason Reason
	}{
		{
			name: "manifest drift",
			mutate: func(request *OperationResumeRequest) {
				request.ManifestSHA256 = testDigest("different-manifest")
			},
			reason: ReasonLineage,
		},
		{
			name: "generation drift",
			mutate: func(request *OperationResumeRequest) {
				request.Binding.ControlGeneration++
			},
			reason: ReasonGeneration,
		},
		{
			name: "sequence gap",
			mutate: func(request *OperationResumeRequest) {
				request.ExpectedSequence++
			},
			reason: ReasonLineage,
		},
		{
			name: "owner attempt mismatch",
			mutate: func(request *OperationResumeRequest) {
				request.OwnerAttemptID = testRecordID
			},
			reason: ReasonOwnership,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := resumeRequest(checkpoint)
			test.mutate(&request)
			decision, err := store.ValidateResume(request)
			if err != nil {
				t.Fatalf("ValidateResume() error = %v", err)
			}
			if decision.Accepted || decision.Reason != test.reason {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestOperationCheckpointRejectsLifecycleAndTampering(t *testing.T) {
	store := NewMemoryOperationCheckpointStore()
	if _, err := store.AppendCheckpoint(checkpointInput(SessionSuspended)); !errors.Is(err, ErrOperationCheckpointCAS) {
		t.Fatalf("first suspended error = %v, want %v", err, ErrOperationCheckpointCAS)
	}
	if _, err := store.AppendCheckpoint(checkpointInput(SessionRunning)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendCheckpoint(checkpointInput(SessionCompleted)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := store.AppendCheckpoint(checkpointInput(SessionRunning)); !errors.Is(err, ErrOperationCheckpointCAS) {
		t.Fatalf("transition after terminal error = %v, want %v", err, ErrOperationCheckpointCAS)
	}

	tampered := NewMemoryOperationCheckpointStore()
	checkpoint, err := tampered.AppendCheckpoint(checkpointInput(SessionRunning))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tampered.AppendCheckpoint(checkpointInput(SessionRunning)); err != nil {
		t.Fatal(err)
	}
	tampered.mu.Lock()
	tampered.checkpoints[checkpoint.Session.OperationID][1].Checkpoint.ParentCheckpointSHA256 = testDigest("tampered")
	tampered.mu.Unlock()
	if _, _, err := tampered.LatestCheckpoint(checkpoint.Session.OperationID); !errors.Is(err, ErrOperationCheckpointTamper) {
		t.Fatalf("LatestCheckpoint() error = %v, want %v", err, ErrOperationCheckpointTamper)
	}
}

func TestOperationResumeAndReplayGateValidationFailures(t *testing.T) {
	var nilStore *MemoryOperationCheckpointStore
	if _, err := nilStore.AppendCheckpoint(checkpointInput(SessionRunning)); !errors.Is(err, ErrOperationCheckpoint) {
		t.Fatalf("nil AppendCheckpoint() error = %v, want %v", err, ErrOperationCheckpoint)
	}
	if _, err := nilStore.ValidateResume(OperationResumeRequest{}); !errors.Is(err, ErrOperationResume) {
		t.Fatalf("nil ValidateResume() error = %v, want %v", err, ErrOperationResume)
	}

	checkpoint, err := NewMemoryOperationCheckpointStore().AppendCheckpoint(checkpointInput(SessionRunning))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		outcome   ReplayGateOutcome
		lifecycle SessionLifecycle
		reason    Reason
	}{
		{ReplayApproved, SessionRunning, ReasonAccepted},
		{ReplayRejected, SessionCancelled, ReasonResumeDenied},
		{ReplayTimeout, SessionFailed, ReasonExpired},
		{ReplayChangedPlan, SessionSuspended, ReasonLineage},
	} {
		record, err := NewReplayContinuationRecord(
			testOperationID,
			testRequestID,
			checkpoint.CheckpointSHA256,
			testDigest("plan"),
			test.outcome,
		)
		if err != nil {
			t.Fatalf("NewReplayContinuationRecord(%s) error = %v", test.outcome, err)
		}
		if record.SessionLifecycle != test.lifecycle ||
			record.Reason != test.reason ||
			!record.ActionStateUnchanged {
			t.Fatalf("record = %+v", record)
		}
	}
	if _, err := NewReplayContinuationRecord(
		testOperationID, testRequestID, checkpoint.CheckpointSHA256, testDigest("plan"), ReplayGateOutcome("unknown"),
	); !errors.Is(err, ErrOperationResume) {
		t.Fatalf("invalid replay outcome error = %v, want %v", err, ErrOperationResume)
	}
}

func checkpointInput(lifecycle SessionLifecycle) OperationCheckpointInput {
	return OperationCheckpointInput{
		OperationID:     testOperationID,
		Domain:          policy.DomainRoot,
		Binding:         checkpointBinding(),
		Workflow:        WorkflowSyntheticQualification,
		Lifecycle:       lifecycle,
		ContractVersion: "v1.0.0",
		RuntimeVersion:  "v1.0.0",
		ManifestSHA256:  testDigest("manifest"),
		ReducerSHA256:   testDigest("reducer"),
		AdapterSHA256:   testDigest("adapter"),
		ChildActionIDs:  []metadata.UUID{testActionID},
		AttemptIDs:      []metadata.UUID{testAttemptID},
		EvidenceDigests: []string{testDigest("evidence")},
	}
}

func checkpointBinding() SnapshotBinding {
	return SnapshotBinding{
		BootID:                 testBootID,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		SourceWatermark:        10,
	}
}

func resumeRequest(checkpoint OperationCheckpointEnvelope) OperationResumeRequest {
	return OperationResumeRequest{
		OperationID:              checkpoint.Session.OperationID,
		ResumeID:                 testRequestID,
		Domain:                   checkpoint.Domain,
		Binding:                  checkpoint.Binding,
		ManifestSHA256:           checkpoint.Session.ManifestSHA256,
		ContractVersion:          checkpoint.Session.ContractVersion,
		RuntimeVersion:           checkpoint.Session.RuntimeVersion,
		ExpectedSequence:         checkpoint.Sequence,
		ExpectedCheckpointSHA256: checkpoint.CheckpointSHA256,
		OwnerAttemptID:           testAttemptID,
	}
}
