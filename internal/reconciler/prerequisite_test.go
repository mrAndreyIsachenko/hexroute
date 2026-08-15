package reconciler

import (
	"errors"
	"reflect"
	"testing"
)

const (
	testManifest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testEvidence = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testBaseline = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestRequiredPrerequisitesRecordExactStructuralArtifacts(t *testing.T) {
	requirements := RequiredPrerequisites()
	if len(requirements) != 2 {
		t.Fatalf("RequiredPrerequisites() length = %d, want 2", len(requirements))
	}
	atomic := requirements[0]
	if atomic.ID != PrerequisiteAtomicPolicyGenerations ||
		atomic.ChangeReference != "openspec/changes/archive/2026-08-15-add-atomic-policy-generations" ||
		atomic.BaselineSpecPath != "openspec/specs/atomic-policy-generations/spec.md" ||
		atomic.EvidenceSchema != AtomicPolicyEvidenceSchema {
		t.Fatalf("atomic prerequisite not pinned to completed artifact: %+v", atomic)
	}
	observable := requirements[1]
	if observable.ID != PrerequisiteObservableConnectivityStateMachine ||
		observable.BaselineSpecPath != "openspec/specs/observable-connectivity-state-machine/spec.md" ||
		observable.EvidenceSchema == "" {
		t.Fatalf("observable prerequisite not recorded: %+v", observable)
	}
}

func TestPrerequisiteGateFailsClosedUntilBothPrerequisitesPass(t *testing.T) {
	expected := testBinding()
	tests := []struct {
		name     string
		evidence []PrerequisiteEvidence
		reason   GateReason
		blocked  PrerequisiteID
	}{
		{
			name:     "missing",
			evidence: nil,
			reason:   GateReasonMissingPrerequisite,
			blocked:  PrerequisiteAtomicPolicyGenerations,
		},
		{
			name: "incomplete",
			evidence: []PrerequisiteEvidence{
				testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
				withStatus(testEvidenceFor(PrerequisiteObservableConnectivityStateMachine), PrerequisiteCollecting),
			},
			reason:  GateReasonIncompletePrerequisite,
			blocked: PrerequisiteObservableConnectivityStateMachine,
		},
		{
			name: "invalid",
			evidence: []PrerequisiteEvidence{
				testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
				withValidity(testEvidenceFor(PrerequisiteObservableConnectivityStateMachine), false),
			},
			reason:  GateReasonInvalidPrerequisite,
			blocked: PrerequisiteObservableConnectivityStateMachine,
		},
		{
			name: "unsynchronized",
			evidence: []PrerequisiteEvidence{
				testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
				withSync(testEvidenceFor(PrerequisiteObservableConnectivityStateMachine), false),
			},
			reason:  GateReasonUnsyncedBaseline,
			blocked: PrerequisiteObservableConnectivityStateMachine,
		},
		{
			name: "generation mismatch",
			evidence: []PrerequisiteEvidence{
				testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
				withBinding(testEvidenceFor(PrerequisiteObservableConnectivityStateMachine), GenerationBinding{
					BundleGeneration: 9, RootPolicyGeneration: 2, UserPolicyGeneration: 1, ManifestSHA256: testManifest,
				}),
			},
			reason:  GateReasonGenerationMismatch,
			blocked: PrerequisiteObservableConnectivityStateMachine,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gate, err := EvaluatePrerequisites(expected, test.evidence)
			if err != nil {
				t.Fatalf("EvaluatePrerequisites() error = %v", err)
			}
			if gate.Ready() || gate.Reason() != test.reason || gate.Prerequisite() != test.blocked {
				t.Fatalf("gate = ready:%v reason:%s prerequisite:%s", gate.Ready(), gate.Reason(), gate.Prerequisite())
			}
		})
	}
}

func TestPrerequisiteGatePassesOnlyForCompleteSynchronizedMatchingEvidence(t *testing.T) {
	gate, err := EvaluatePrerequisites(testBinding(), []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		testEvidenceFor(PrerequisiteObservableConnectivityStateMachine),
	})
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	if !gate.Ready() || gate.Reason() != GateReasonNone || gate.Prerequisite() != "" {
		t.Fatalf("gate = ready:%v reason:%s prerequisite:%s", gate.Ready(), gate.Reason(), gate.Prerequisite())
	}
}

func TestPrerequisiteGateRejectsInvalidInputs(t *testing.T) {
	if _, err := EvaluatePrerequisites(GenerationBinding{}, nil); !errors.Is(err, ErrInvalidPrerequisiteInput) {
		t.Fatalf("EvaluatePrerequisites(empty binding) error = %v", err)
	}
	duplicate := []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
	}
	if _, err := EvaluatePrerequisites(testBinding(), duplicate); !errors.Is(err, ErrInvalidPrerequisiteInput) {
		t.Fatalf("EvaluatePrerequisites(duplicate) error = %v", err)
	}
}

func TestGateHasNoCallerSettableFields(t *testing.T) {
	gateType := reflect.TypeOf(Gate{})
	for index := 0; index < gateType.NumField(); index++ {
		if gateType.Field(index).IsExported() {
			t.Fatalf("Gate exposes caller-settable field %s", gateType.Field(index).Name)
		}
	}
}

func testBinding() GenerationBinding {
	return GenerationBinding{
		BundleGeneration:     2,
		RootPolicyGeneration: 2,
		UserPolicyGeneration: 1,
		ManifestSHA256:       testManifest,
	}
}

func testEvidenceFor(id PrerequisiteID) PrerequisiteEvidence {
	return PrerequisiteEvidence{
		ID:                       id,
		Status:                   PrerequisiteComplete,
		Valid:                    true,
		BaselineSynchronized:     true,
		Binding:                  testBinding(),
		EvidenceSHA256:           testEvidence,
		BaselineRevisionSHA256:   testBaseline,
		QualificationArtifactRef: "synthetic-artifact",
	}
}

func withStatus(evidence PrerequisiteEvidence, status PrerequisiteStatus) PrerequisiteEvidence {
	evidence.Status = status
	return evidence
}

func withValidity(evidence PrerequisiteEvidence, valid bool) PrerequisiteEvidence {
	evidence.Valid = valid
	return evidence
}

func withSync(evidence PrerequisiteEvidence, synchronized bool) PrerequisiteEvidence {
	evidence.BaselineSynchronized = synchronized
	return evidence
}

func withBinding(evidence PrerequisiteEvidence, binding GenerationBinding) PrerequisiteEvidence {
	evidence.Binding = binding
	return evidence
}
