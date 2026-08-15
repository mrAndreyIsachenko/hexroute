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
	if !enabled.ProposalTranslation ||
		!enabled.ExecutionIPC ||
		enabled.ProposalComparison ||
		enabled.Replay ||
		len(enabled.CapabilityIDs) == 0 {
		t.Fatalf("startup surface did not expose synthetic runtime after gates: %+v", enabled)
	}
}

func TestStartupSurfaceKeepsShadowComparisonAndReplayBehindFeatureGates(t *testing.T) {
	ready, err := EvaluatePrerequisites(testBinding(), []PrerequisiteEvidence{
		testEvidenceFor(PrerequisiteAtomicPolicyGenerations),
		testEvidenceFor(PrerequisiteObservableConnectivityStateMachine),
	})
	if err != nil {
		t.Fatalf("EvaluatePrerequisites() error = %v", err)
	}
	comparison := BuildStartupSurface(
		ready,
		DefaultSyntheticRegistry(),
		FeatureGate{SyntheticEngine: true, SyntheticShadowComparison: true},
	)
	if !comparison.ProposalTranslation ||
		!comparison.ExecutionIPC ||
		!comparison.ProposalComparison ||
		comparison.Replay {
		t.Fatalf("comparison surface = %+v", comparison)
	}

	replayOnly := BuildStartupSurface(
		ready,
		DefaultSyntheticRegistry(),
		FeatureGate{SyntheticEngine: true, SyntheticReplay: true},
	)
	if replayOnly.ProposalComparison || replayOnly.Replay {
		t.Fatalf("replay surfaced without comparison gate: %+v", replayOnly)
	}

	replay := BuildStartupSurface(
		ready,
		DefaultSyntheticRegistry(),
		FeatureGate{
			SyntheticEngine:           true,
			SyntheticShadowComparison: true,
			SyntheticReplay:           true,
		},
	)
	if !replay.ProposalComparison || !replay.Replay {
		t.Fatalf("replay surface = %+v", replay)
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
