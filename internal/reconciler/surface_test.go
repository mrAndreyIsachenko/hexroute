package reconciler

import "testing"

func TestStartupSurfaceKeepsTranslationAndExecutionAbsentUntilPrerequisitesPass(t *testing.T) {
	incomplete, err := EvaluatePrerequisites(testBinding(), []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		withStatus(testEvidenceFor(PrerequisiteObservableConnectivityStateMachine), PrerequisiteCollecting),
	})
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	surface := BuildStartupSurface(incomplete, DefaultSyntheticRegistry(), FeatureGate{SyntheticEngine: true})
	if surface.ProposalTranslation || surface.ExecutionIPC || len(surface.CapabilityIDs) != 0 {
		t.Fatalf("startup surface exposed runtime before prerequisites passed: %+v", surface)
	}
}

func TestStartupSurfaceRequiresExplicitSyntheticFeatureGate(t *testing.T) {
	ready, err := EvaluatePrerequisites(testBinding(), []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		testEvidenceFor(PrerequisiteObservableConnectivityStateMachine),
	})
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	disabled := BuildStartupSurface(ready, DefaultSyntheticRegistry(), FeatureGate{})
	if disabled.ProposalTranslation || disabled.ExecutionIPC || len(disabled.CapabilityIDs) != 0 {
		t.Fatalf("startup surface exposed runtime without feature gate: %+v", disabled)
	}
	enabled := BuildStartupSurface(ready, DefaultSyntheticRegistry(), FeatureGate{SyntheticEngine: true})
	if !enabled.ProposalTranslation || !enabled.ExecutionIPC || len(enabled.CapabilityIDs) == 0 {
		t.Fatalf("startup surface did not expose synthetic runtime after gates: %+v", enabled)
	}
}

func TestStartupSurfaceRejectsNonSyntheticRegistryEvenWhenPrerequisitesPass(t *testing.T) {
	ready, err := EvaluatePrerequisites(testBinding(), []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		testEvidenceFor(PrerequisiteObservableConnectivityStateMachine),
	})
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	surface := BuildStartupSurface(ready, Registry{}, FeatureGate{SyntheticEngine: true})
	if surface.ProposalTranslation || surface.ExecutionIPC || len(surface.CapabilityIDs) != 0 {
		t.Fatalf("startup surface exposed runtime with empty registry: %+v", surface)
	}
}
