package operator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/resumeplan"
)

type recordingResumePolicyEvaluator struct {
	calls             int
	domain            policy.Domain
	target            string
	controlGeneration uint64
	planSHA256        string
	decision          policy.ActionAuthorizationDecision
}

type recordingResumePolicyExecutor struct {
	calls int
	plan  resumeplan.Plan
	after control.Snapshot
	err   error
}

func (executor *recordingResumePolicyExecutor) ExecuteOperatorResume(
	plan resumeplan.Plan,
) (control.Snapshot, error) {
	executor.calls++
	executor.plan = plan
	return executor.after, executor.err
}

func (evaluator *recordingResumePolicyEvaluator) EvaluateOperatorResume(
	domain policy.Domain,
	target string,
	controlGeneration uint64,
	planSHA256 string,
) policy.ActionAuthorizationDecision {
	evaluator.calls++
	evaluator.domain = domain
	evaluator.target = target
	evaluator.controlGeneration = controlGeneration
	evaluator.planSHA256 = planSHA256
	return evaluator.decision
}

func TestControllerReportsBoundedStatusAndDiagnostics(t *testing.T) {
	snapshot := safeModeSnapshot()
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			t.Fatal("read-only request invoked resume")
			return control.Snapshot{}, nil
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatalf("NewController() error: %v", err)
	}

	status := controller.Handle(ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "status-1",
		Action:    ipc.ActionStatus,
	})
	if !status.OK ||
		status.Status == nil ||
		status.Status.Role != ipc.RoleUser ||
		!status.Status.SafeMode ||
		status.Status.Generation != snapshot.Generation {
		t.Fatalf("status response = %+v", status)
	}

	diagnostics := controller.Handle(ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "diagnostics-1",
		Action:    ipc.ActionExportDiagnostics,
	})
	if !diagnostics.OK ||
		diagnostics.Diagnostics == nil ||
		diagnostics.Diagnostics.Attempts != snapshot.Attempts ||
		diagnostics.Diagnostics.LastReason != control.ReasonRecoveryBudget {
		t.Fatalf("diagnostics response = %+v", diagnostics)
	}
	assertNoSecretBearingFields(t, reflect.TypeOf(*diagnostics.Diagnostics))
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	for _, forbidden := range []string{
		"pin",
		"otp",
		"secret",
		"private_key",
		"profile_id",
		"server_name",
		"address",
		"command",
	} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("diagnostics contains forbidden field %q: %s", forbidden, encoded)
		}
	}
}

func TestControllerResumeRequiresRoleSafeModeAndExactGeneration(t *testing.T) {
	tests := []struct {
		name       string
		role       ipc.DaemonRole
		targets    []control.Component
		request    ipc.Request
		state      control.State
		wantError  ipc.ErrorCode
		wantResume bool
	}{
		{
			name:      "stale generation",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentPritunl, 6),
			state:     control.StateSafeMode,
			wantError: ipc.ErrorStaleGeneration,
		},
		{
			name:      "not in safe mode",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentPritunl, 7),
			state:     control.StateHealthy,
			wantError: ipc.ErrorPrecondition,
		},
		{
			name:      "root target through user socket",
			role:      ipc.RoleUser,
			targets:   []control.Component{control.ComponentPritunl},
			request:   resumeRequest(control.ComponentTunnel, 7),
			state:     control.StateSafeMode,
			wantError: ipc.ErrorUnauthorized,
		},
		{
			name:       "explicit user resume",
			role:       ipc.RoleUser,
			targets:    []control.Component{control.ComponentPritunl},
			request:    resumeRequest(control.ComponentPritunl, 7),
			state:      control.StateSafeMode,
			wantResume: true,
		},
		{
			name:       "explicit root resume",
			role:       ipc.RoleRoot,
			targets:    []control.Component{control.ComponentTunnel},
			request:    resumeRequest(control.ComponentTunnel, 7),
			state:      control.StateSafeMode,
			wantResume: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := safeModeSnapshot()
			snapshot.State = test.state
			if test.state != control.StateSafeMode {
				snapshot.SafeUntil = 0
			}
			resumeCalls := 0
			controller, err := NewController(
				test.role,
				ipc.ModeObserveOnly,
				test.targets,
				snapshot,
				control.ReasonRecoveryBudget,
				func(expected uint64, at control.Tick) (control.Snapshot, error) {
					resumeCalls++
					if expected != snapshot.Generation || at != 100 {
						t.Fatalf("resume(%d, %d)", expected, at)
					}
					updated := snapshot
					updated.State = control.StateDegraded
					updated.Generation++
					updated.Attempts = 0
					updated.RecoveringSince = 0
					updated.NextActionAt = at
					updated.SafeUntil = 0
					return updated, nil
				},
				func() control.Tick { return 100 },
			)
			if err != nil {
				t.Fatalf("NewController() error: %v", err)
			}

			response := controller.Handle(test.request)
			if response.Error != test.wantError {
				t.Fatalf("response error = %q, want %q", response.Error, test.wantError)
			}
			if test.wantResume {
				if !response.OK ||
					response.Resume == nil ||
					response.Resume.PreviousState != control.StateSafeMode ||
					response.Resume.State != control.StateDegraded ||
					response.Resume.Generation != 8 ||
					resumeCalls != 1 {
					t.Fatalf("resume response = %+v calls=%d", response, resumeCalls)
				}
				status := controller.Handle(ipc.Request{
					Version:   ipc.ProtocolVersion,
					RequestID: "status-after",
					Action:    ipc.ActionStatus,
				})
				if status.Status == nil || status.Status.SafeMode {
					t.Fatalf("status after resume = %+v", status)
				}
			} else if resumeCalls != 0 {
				t.Fatalf("rejected request invoked resume %d times", resumeCalls)
			}
		})
	}
}

func TestControllerRejectsStaleUpdatesAndResumeFailures(t *testing.T) {
	snapshot := safeModeSnapshot()
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			return control.Snapshot{}, control.ErrStaleGeneration
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatalf("NewController() error: %v", err)
	}

	stale := snapshot
	stale.Generation--
	if err := controller.Update(stale, control.ReasonProbeFailed); !errors.Is(
		err,
		control.ErrStaleGeneration,
	) {
		t.Fatalf("Update() error = %v, want stale generation", err)
	}
	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation))
	if response.Error != ipc.ErrorStaleGeneration {
		t.Fatalf("resume error = %q, want %q", response.Error, ipc.ErrorStaleGeneration)
	}
}

func TestControllerEvaluatesResumePolicyInShadowWithoutChangingLegacyOutcome(t *testing.T) {
	snapshot := safeModeSnapshot()
	evaluator := &recordingResumePolicyEvaluator{decision: policy.ActionAuthorizationDecision{
		Reason: policy.ActionSelectorMismatch,
	}}
	resumeCalls := 0
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(expected uint64, at control.Tick) (control.Snapshot, error) {
			resumeCalls++
			updated := snapshot
			updated.State = control.StateDegraded
			updated.Generation++
			updated.Attempts = 0
			updated.RecoveringSince = 0
			updated.NextActionAt = at
			updated.LastTick = at
			updated.SafeUntil = 0
			return updated, nil
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetResumePolicyEvaluator(evaluator); err != nil {
		t.Fatal(err)
	}

	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation))
	if !response.OK || response.Resume == nil || resumeCalls != 1 {
		t.Fatalf("shadow denial changed resume response=%+v calls=%d", response, resumeCalls)
	}
	if evaluator.calls != 1 || evaluator.domain != policy.DomainUser ||
		evaluator.target != string(control.ComponentPritunl) ||
		evaluator.controlGeneration != snapshot.Generation ||
		len(evaluator.planSHA256) != 64 {
		t.Fatalf("shadow evaluation = %+v", evaluator)
	}
}

func TestControllerPreservesGenerationGuardBeforeShadowEvaluation(t *testing.T) {
	snapshot := safeModeSnapshot()
	evaluator := &recordingResumePolicyEvaluator{}
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			t.Fatal("stale resume invoked mutation")
			return control.Snapshot{}, nil
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetResumePolicyEvaluator(evaluator); err != nil {
		t.Fatal(err)
	}

	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation-1))
	if response.Error != ipc.ErrorStaleGeneration || evaluator.calls != 0 {
		t.Fatalf("response=%+v shadow calls=%d", response, evaluator.calls)
	}
}

func TestControllerEnforcesResumeOnlyBehindValidatedQualification(t *testing.T) {
	snapshot := safeModeSnapshot()
	evaluator := &recordingResumePolicyEvaluator{decision: policy.ActionAuthorizationDecision{
		Allowed: true, Reason: policy.ActionAuthorized,
	}}
	executor := &recordingResumePolicyExecutor{}
	legacyCalls := 0
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			legacyCalls++
			return control.Snapshot{}, errors.New("legacy resume must stay disabled")
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetResumePolicyEvaluator(evaluator); err != nil {
		t.Fatal(err)
	}
	if err := controller.EnableResumePolicyEnforcement(
		executor,
		policyqualification.Gate{},
	); !errors.Is(err, ErrInvalidController) {
		t.Fatalf("zero qualification error=%v", err)
	}
	gate := completeResumeGate(t)
	executor.after = snapshot
	executor.after.State = control.StateDegraded
	executor.after.Generation++
	executor.after.Attempts = 0
	executor.after.RecoveringSince = 0
	executor.after.NextActionAt = 100
	executor.after.SafeUntil = 0
	executor.after.LastTick = 100
	if err := controller.EnableResumePolicyEnforcement(executor, gate); err != nil {
		t.Fatal(err)
	}

	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation))
	if !response.OK || response.Resume == nil || executor.calls != 1 || legacyCalls != 0 ||
		executor.plan.Digest() == "" {
		t.Fatalf(
			"response=%+v executor_calls=%d legacy_calls=%d",
			response,
			executor.calls,
			legacyCalls,
		)
	}
}

func TestControllerActiveResumeDenialCannotReachAnyMutation(t *testing.T) {
	snapshot := safeModeSnapshot()
	evaluator := &recordingResumePolicyEvaluator{decision: policy.ActionAuthorizationDecision{
		Reason: policy.ActionSelectorMismatch,
	}}
	executor := &recordingResumePolicyExecutor{}
	legacyCalls := 0
	controller, err := NewController(
		ipc.RoleUser,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		snapshot,
		control.ReasonRecoveryBudget,
		func(uint64, control.Tick) (control.Snapshot, error) {
			legacyCalls++
			return control.Snapshot{}, nil
		},
		func() control.Tick { return 100 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SetResumePolicyEvaluator(evaluator); err != nil {
		t.Fatal(err)
	}
	gate := completeResumeGate(t)
	if err := controller.EnableResumePolicyEnforcement(executor, gate); err != nil {
		t.Fatal(err)
	}

	response := controller.Handle(resumeRequest(control.ComponentPritunl, snapshot.Generation))
	if response.Error != ipc.ErrorPrecondition || executor.calls != 0 || legacyCalls != 0 {
		t.Fatalf(
			"response=%+v executor_calls=%d legacy_calls=%d",
			response,
			executor.calls,
			legacyCalls,
		)
	}
}

func completeResumeGate(t *testing.T) policyqualification.Gate {
	t.Helper()
	binding := policyqualification.Binding{
		SessionID:        "11111111-1111-4111-8111-111111111111",
		BundleGeneration: 7, RootPolicyGeneration: 5, UserPolicyGeneration: 6,
		ManifestSHA256: policy.SHA256Hex([]byte("operator test manifest")),
	}
	root := filepath.Join(t.TempDir(), "qualification")
	recorder, err := policyqualification.OpenRecorder(root, binding)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	bootOne := metadata.UUID("22222222-2222-4222-8222-222222222222")
	bootTwo := metadata.UUID("33333333-3333-4333-8333-333333333333")
	sources := make(map[metadata.UUID][]byte)
	sequence := 0
	observation := func(boot metadata.UUID, offset, monotonic time.Duration) policyqualification.Observation {
		sequence++
		id := metadata.UUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence))
		content := []byte(fmt.Sprintf("operator source %d", sequence))
		sources[id] = content
		return policyqualification.Observation{
			BootID:            boot,
			Sources:           []policyqualification.SourceReference{{EventID: id, SHA256: policy.SHA256Hex(content)}},
			ObservedAt:        base.Add(offset).Format(time.RFC3339Nano),
			SourceMonotonicNS: int64(monotonic),
			Result:            policyqualification.ResultPassed, Reason: policyqualification.ReasonNone,
		}
	}
	must := func(_ policyqualification.EvidenceRecord, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("append qualification record %d: %v", sequence, err)
		}
	}
	decisionDigest := policy.SHA256Hex([]byte("same decision"))
	must(recorder.RecordSafetyComparison(
		observation(bootOne, time.Hour, time.Hour),
		policyqualification.SafetyComparison{
			ExpectedSHA256: decisionDigest, ObservedSHA256: decisionDigest,
		},
	))
	must(recorder.RecordSleepWake(
		observation(bootOne, 2*time.Hour, 2*time.Hour),
		policyqualification.SleepWake{
			SleptAt:          base.Add(90 * time.Minute).Format(time.RFC3339Nano),
			WokeAt:           base.Add(2 * time.Hour).Format(time.RFC3339Nano),
			SleptMonotonicNS: int64(90 * time.Minute), WokeMonotonicNS: int64(2 * time.Hour),
		},
	))
	must(recorder.RecordFaultInjection(
		policyqualification.KindInvalidSignature,
		observation(bootOne, 3*time.Hour, 3*time.Hour),
		policyqualification.FaultInjection{Outcome: policyqualification.OutcomeCandidateRejected},
	))
	must(recorder.RecordEligibleWindow(
		observation(bootOne, 4*time.Hour, 4*time.Hour),
		policyqualification.EligibleWindow{
			StartedAt:        base.Format(time.RFC3339Nano),
			EndedAt:          base.Add(4 * time.Hour).Format(time.RFC3339Nano),
			EndedMonotonicNS: int64(4 * time.Hour),
		},
	))
	must(recorder.RecordReboot(
		observation(bootTwo, 4*time.Hour, 0),
		policyqualification.Reboot{
			PreviousBootID: bootOne, CurrentBootID: bootTwo,
			ObservedAt: base.Add(4 * time.Hour).Format(time.RFC3339Nano),
		},
	))
	must(recorder.RecordFaultInjection(
		policyqualification.KindSelectorConflict,
		observation(bootTwo, 5*time.Hour, time.Hour),
		policyqualification.FaultInjection{Outcome: policyqualification.OutcomeCandidateRejected},
	))
	must(recorder.RecordFaultInjection(
		policyqualification.KindStaleGeneration,
		observation(bootTwo, 6*time.Hour, 2*time.Hour),
		policyqualification.FaultInjection{Outcome: policyqualification.OutcomeMutationRejected},
	))
	must(recorder.RecordFaultInjection(
		policyqualification.KindCrossDomainCrash,
		observation(bootTwo, 7*time.Hour, 3*time.Hour),
		policyqualification.FaultInjection{Outcome: policyqualification.OutcomeDomainMismatchBlocked},
	))
	must(recorder.RecordSleepWake(
		observation(bootTwo, 8*time.Hour, 4*time.Hour),
		policyqualification.SleepWake{
			SleptAt:          base.Add(15 * time.Hour / 2).Format(time.RFC3339Nano),
			WokeAt:           base.Add(8 * time.Hour).Format(time.RFC3339Nano),
			SleptMonotonicNS: int64(7 * time.Hour / 2), WokeMonotonicNS: int64(4 * time.Hour),
		},
	))
	must(recorder.RecordEligibleWindow(
		observation(bootTwo, 72*time.Hour, 68*time.Hour),
		policyqualification.EligibleWindow{
			StartedAt:        base.Add(4 * time.Hour).Format(time.RFC3339Nano),
			EndedAt:          base.Add(72 * time.Hour).Format(time.RFC3339Nano),
			EndedMonotonicNS: int64(68 * time.Hour),
		},
	))
	if records, err := policyqualification.ReadRecords(root); err != nil || len(records) != 10 {
		t.Fatalf("read qualification records count=%d error=%v", len(records), err)
	}
	gate, err := policyqualification.Replay(
		root,
		binding,
		policyqualification.SourceLoaderFunc(func(id metadata.UUID) ([]byte, error) {
			content, ok := sources[id]
			if !ok {
				t.Fatalf("qualification source %s not found", id)
				return nil, os.ErrNotExist
			}
			return content, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return gate
}

func safeModeSnapshot() control.Snapshot {
	return control.Snapshot{
		SchemaVersion:       control.SnapshotSchemaVersion,
		Generation:          7,
		State:               control.StateSafeMode,
		ConsecutiveFailures: 5,
		Attempts:            3,
		LastTick:            90,
		RecoveringSince:     80,
		NextActionAt:        95,
		SafeUntil:           700,
	}
}

func resumeRequest(target control.Component, generation uint64) ipc.Request {
	return ipc.Request{
		Version:            ipc.ProtocolVersion,
		RequestID:          "resume-1",
		Action:             ipc.ActionResumeTarget,
		Target:             target,
		ExpectedGeneration: generation,
	}
}

func assertNoSecretBearingFields(t *testing.T, value reflect.Type) {
	t.Helper()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, forbidden := range []string{
			"pin",
			"otp",
			"secret",
			"key",
			"profile",
			"address",
			"server",
			"command",
			"hostname",
		} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("diagnostics exposes secret-bearing field %s", field.Name)
			}
		}
		if field.Type.Kind() == reflect.Struct {
			assertNoSecretBearingFields(t, field.Type)
		}
	}
}
