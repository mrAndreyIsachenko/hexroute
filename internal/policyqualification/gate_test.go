package policyqualification

import (
	"errors"
	"testing"
)

func TestValidateRequiresCompleteShadowEvidence(t *testing.T) {
	complete := completeEvidence()
	gate, err := Validate(complete)
	if err != nil || !gate.Complete() {
		t.Fatalf("Validate() gate=%+v error=%v", gate, err)
	}

	tests := []struct {
		name   string
		mutate func(*Evidence)
	}{
		{name: "duration", mutate: func(value *Evidence) { value.EligibleDuration-- }},
		{name: "sleep wake", mutate: func(value *Evidence) { value.SleepWakeCycles = 1 }},
		{name: "reboot", mutate: func(value *Evidence) { value.RebootObserved = false }},
		{name: "signature", mutate: func(value *Evidence) { value.InvalidSignatureInjected = false }},
		{name: "selector", mutate: func(value *Evidence) { value.SelectorConflictInjected = false }},
		{name: "generation", mutate: func(value *Evidence) { value.StaleGenerationInjected = false }},
		{name: "cross domain", mutate: func(value *Evidence) { value.CrossDomainCrashInjected = false }},
		{name: "mismatch", mutate: func(value *Evidence) { value.UnexplainedSafetyMismatch = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := complete
			test.mutate(&evidence)
			gate, err := Validate(evidence)
			if !errors.Is(err, ErrIncompleteEvidence) || gate.Complete() {
				t.Fatalf("Validate() gate=%+v error=%v", gate, err)
			}
		})
	}
	if (Gate{}).Complete() {
		t.Fatal("zero gate enabled enforcement")
	}
}

func completeEvidence() Evidence {
	return Evidence{
		EligibleDuration: MinimumEligibleDuration, SleepWakeCycles: 2,
		RebootObserved: true, InvalidSignatureInjected: true,
		SelectorConflictInjected: true, StaleGenerationInjected: true,
		CrossDomainCrashInjected: true,
	}
}
