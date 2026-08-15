package reconciler

import (
	"errors"
	"reflect"
	"testing"
)

func TestTranslateProposalIsDeterministicAndBindsPlan(t *testing.T) {
	input := translationInput()
	first, err := TranslateProposal(input)
	if err != nil {
		t.Fatalf("TranslateProposal() error = %v", err)
	}
	second, err := TranslateProposal(input)
	if err != nil {
		t.Fatalf("TranslateProposal(second) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("translation not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Acknowledgement.Class != AckAccepted || first.Acknowledgement.ActionID != input.Proposal.ActionID {
		t.Fatalf("ack = %+v", first.Acknowledgement)
	}
	if first.Plan == nil {
		t.Fatal("accepted translation did not return a plan")
	}
	plan := *first.Plan
	if plan.Target != input.Proposal.Target ||
		plan.CapabilityID != input.Proposal.CapabilityID ||
		plan.AdapterVersion != input.Adapter.Version ||
		plan.AdapterSHA256 != input.Adapter.SHA256 ||
		plan.ProposalSHA256 != input.Proposal.ProposalSHA256 ||
		plan.DiffSHA256 != input.Proposal.DiffSHA256 ||
		plan.SnapshotSHA256 != input.Proposal.SnapshotSHA256 ||
		plan.ReadinessSHA256 != input.Proposal.ReadinessSHA256 ||
		plan.BundleGeneration != input.Proposal.SnapshotBinding.BundleGeneration ||
		plan.ControlGeneration != input.Proposal.SnapshotBinding.ControlGeneration ||
		len(plan.StepDigests) != 1 ||
		len(plan.VerificationDigests) != 1 ||
		len(plan.CompensationDigests) != 1 {
		t.Fatalf("plan not bound to proposal: %+v", plan)
	}
}

func TestTranslateProposalRejectsStaleWrongOwnerAndUndeclaredCapability(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TranslationInput)
		reason Reason
	}{
		{
			name: "stale binding",
			mutate: func(input *TranslationInput) {
				input.CurrentSnapshotBinding.ControlGeneration = 99
			},
			reason: ReasonGeneration,
		},
		{
			name: "wrong target",
			mutate: func(input *TranslationInput) {
				input.Readiness.Target = "synthetic.other"
			},
			reason: ReasonTarget,
		},
		{
			name: "undeclared capability",
			mutate: func(input *TranslationInput) {
				input.Proposal.CapabilityID = "synthetic.reconciler.unknown"
			},
			reason: ReasonCapability,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := translationInput()
			test.mutate(&input)
			result, err := TranslateProposal(input)
			if err != nil {
				t.Fatalf("TranslateProposal() error = %v", err)
			}
			if result.Plan != nil ||
				result.Acknowledgement.Class != AckDenied ||
				result.Acknowledgement.Reason != test.reason ||
				result.Acknowledgement.RetryClass != RetryNever {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestTranslateProposalNoopAndTemporaryAcknowledgementsDoNotMintAction(t *testing.T) {
	input := translationInput()
	input.SemanticNoop = true
	input.Proposal.ActionID = ""
	input.Steps = nil
	result, err := TranslateProposal(input)
	if err != nil {
		t.Fatalf("TranslateProposal(noop) error = %v", err)
	}
	if result.Plan != nil ||
		result.Acknowledgement.Class != AckAccepted ||
		!result.Acknowledgement.NoAction ||
		result.Acknowledgement.ActionID != "" {
		t.Fatalf("noop result = %+v", result)
	}

	input = translationInput()
	input.Readiness.Status = ReadinessTemporarilyBlocked
	input.Readiness.Reason = ReasonCooldown
	input.Readiness.RetryClass = RetryAfterHint
	input.Readiness.RetryAfterSeconds = 120
	result, err = TranslateProposal(input)
	if err != nil {
		t.Fatalf("TranslateProposal(temporary) error = %v", err)
	}
	if result.Plan != nil ||
		result.Acknowledgement.Class != AckTemporarilyRejected ||
		result.Acknowledgement.Reason != ReasonCooldown ||
		result.Acknowledgement.RetryAfterSeconds != 120 ||
		result.Acknowledgement.ActionID != "" {
		t.Fatalf("temporary result = %+v", result)
	}
}

func TestTranslateProposalRejectsArbitraryCommandPathCredentialValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TranslationInput)
	}{
		{
			name: "command",
			mutate: func(input *TranslationInput) {
				input.Steps[0].ID = "process-command"
			},
		},
		{
			name: "path",
			mutate: func(input *TranslationInput) {
				input.Steps[0].ID = "private-path"
			},
		},
		{
			name: "credential",
			mutate: func(input *TranslationInput) {
				input.Steps[0].ID = "credential-reference"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := translationInput()
			test.mutate(&input)
			if _, err := TranslateProposal(input); !errors.Is(err, ErrInvalidTranslationInput) {
				t.Fatalf("TranslateProposal() error = %v, want %v", err, ErrInvalidTranslationInput)
			}
		})
	}
}

func TestTranslateProposalDeniesUndeclaredOperationClass(t *testing.T) {
	input := translationInput()
	input.Steps[0].Operation = OperationSyntheticNoop
	result, err := TranslateProposal(input)
	if err != nil {
		t.Fatalf("TranslateProposal() error = %v", err)
	}
	if result.Plan != nil ||
		result.Acknowledgement.Class != AckDenied ||
		result.Acknowledgement.Reason != ReasonCapability {
		t.Fatalf("result = %+v", result)
	}
}

func translationInput() TranslationInput {
	binding := SnapshotBinding{
		BootID:                 testBootID,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		SourceWatermark:        10,
	}
	return TranslationInput{
		Proposal: ProposalBinding{
			RequestID:       testRequestID,
			ActionID:        testActionID,
			Target:          "synthetic.target",
			Domain:          "root",
			CapabilityID:    CapabilitySyntheticMemory,
			ProposalSHA256:  testDigest("proposal"),
			DiffSHA256:      testDigest("diff"),
			SnapshotSHA256:  testDigest("snapshot"),
			ReadinessSHA256: testDigest("readiness"),
			SnapshotBinding: binding,
		},
		CurrentSnapshotBinding: binding,
		Readiness: ReadinessRecord{
			Target: "synthetic.target", Status: ReadinessReady, Reason: ReasonAccepted, RetryClass: RetryNone,
		},
		Registry: DefaultSyntheticRegistry(),
		Adapter: AdapterMetadata{
			ID: "synthetic.adapter.memory", Version: "v1.0.0", SHA256: testDigest("adapter"),
		},
		Steps: []TranslationStep{{
			ID:                 "synthetic.step",
			Operation:          OperationSyntheticState,
			InputSHA256:        testDigest("input"),
			BeforeSHA256:       testDigest("before"),
			AppliedSHA256:      testDigest("after"),
			VerificationSHA256: testDigest("verify"),
			CompensationSHA256: testDigest("compensate"),
		}},
	}
}
