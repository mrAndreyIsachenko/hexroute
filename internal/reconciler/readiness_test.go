package reconciler

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestReadinessEvaluatorClassifiesPolicyAndSourceGates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReadinessInput)
		status ReadinessStatus
		reason Reason
		retry  RetryClass
	}{
		{
			name: "single transient failure",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 1
				input.Raw.FailedDurationSeconds = 30
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonThreshold,
			retry:  RetryAfterHint,
		},
		{
			name: "stable failure",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
			},
			status: ReadinessReady,
			reason: ReasonAccepted,
			retry:  RetryNone,
		},
		{
			name: "cooldown",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Policy.CooldownRemainingSeconds = 120
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonCooldown,
			retry:  RetryAfterHint,
		},
		{
			name: "exhausted budget",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Policy.BudgetRemaining = 0
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonBudget,
			retry:  RetryAfterHint,
		},
		{
			name: "missing baseline",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Source.BaselineAvailable = false
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonFreshness,
			retry:  RetryAfterHint,
		},
		{
			name: "source gap",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Source.Gap = true
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonLineage,
			retry:  RetryAfterHint,
		},
		{
			name: "source conflict",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Source.Conflict = true
			},
			status: ReadinessTemporarilyBlocked,
			reason: ReasonLineage,
			retry:  RetryAfterHint,
		},
		{
			name: "policy change",
			mutate: func(input *ReadinessInput) {
				input.Raw.ConsecutiveFailures = 3
				input.Raw.FailedDurationSeconds = 180
				input.Raw.ObservedBinding.DomainPolicyGeneration = 99
			},
			status: ReadinessDenied,
			reason: ReasonGeneration,
			retry:  RetryNever,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := readinessInput()
			test.mutate(&input)
			evaluation, err := EvaluateReadiness(input)
			if err != nil {
				t.Fatalf("EvaluateReadiness() error = %v", err)
			}
			if evaluation.Readiness.Status != test.status ||
				evaluation.Readiness.Reason != test.reason ||
				evaluation.Readiness.RetryClass != test.retry {
				t.Fatalf("readiness = %+v", evaluation.Readiness)
			}
		})
	}
}

func TestReadinessInvalidatesOnBootSuspensionAndOwnershipChange(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReadinessInput)
		reason Reason
	}{
		{
			name: "boot change",
			mutate: func(input *ReadinessInput) {
				input.Raw.ObservedBinding.BootID = "88888888-8888-4888-8888-888888888888"
			},
			reason: ReasonGeneration,
		},
		{
			name: "suspended policy",
			mutate: func(input *ReadinessInput) {
				input.Policy.Suspended = true
			},
			reason: ReasonPolicy,
		},
		{
			name: "ownership change",
			mutate: func(input *ReadinessInput) {
				input.Raw.Owner = policy.DomainUser
			},
			reason: ReasonOwnership,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := readinessInput()
			input.Raw.ConsecutiveFailures = 3
			input.Raw.FailedDurationSeconds = 180
			test.mutate(&input)
			evaluation, err := EvaluateReadiness(input)
			if err != nil {
				t.Fatalf("EvaluateReadiness() error = %v", err)
			}
			if evaluation.Readiness.Status != ReadinessDenied || evaluation.Readiness.Reason != test.reason {
				t.Fatalf("readiness = %+v", evaluation.Readiness)
			}
		})
	}
}

func TestReadinessInputHasNoCallerSuppliedReadinessFlag(t *testing.T) {
	inputType := reflect.TypeOf(ReadinessInput{})
	for index := 0; index < inputType.NumField(); index++ {
		name := strings.ToLower(inputType.Field(index).Name)
		if name == "ready" || name == "readiness" || name == "actionable" {
			t.Fatalf("ReadinessInput exposes caller-supplied readiness field %s", inputType.Field(index).Name)
		}
	}
}

func TestReadinessStatusProjectionKeepsRawAndActionReadinessSeparate(t *testing.T) {
	input := readinessInput()
	input.Raw.ConsecutiveFailures = 1
	input.Raw.FailedDurationSeconds = 30
	evaluation, err := EvaluateReadiness(input)
	if err != nil {
		t.Fatalf("EvaluateReadiness() error = %v", err)
	}
	projection := ProjectReadiness(evaluation)
	if projection.RawComponent.State != RawFailed ||
		projection.ActionReadiness.Status != ReadinessTemporarilyBlocked {
		t.Fatalf("projection flattened state: %+v", projection)
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"healthy\":", "actionable\":", "ready\":"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("projection contains flattened status field %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(normalized, "raw_component") ||
		!strings.Contains(normalized, "action_readiness") {
		t.Fatalf("projection does not expose separate raw/readiness sections: %s", encoded)
	}
}

func TestReadinessRejectsInvalidInputs(t *testing.T) {
	input := readinessInput()
	input.Source.BaselineAvailable = false
	input.Source.Gap = true
	if _, err := EvaluateReadiness(input); !errors.Is(err, ErrInvalidReadinessInput) {
		t.Fatalf("EvaluateReadiness(invalid source) error = %v", err)
	}
	input = readinessInput()
	input.Raw.State = RawHealthy
	input.Raw.ConsecutiveFailures = 1
	if _, err := EvaluateReadiness(input); !errors.Is(err, ErrInvalidReadinessInput) {
		t.Fatalf("EvaluateReadiness(invalid raw) error = %v", err)
	}
}

func readinessInput() ReadinessInput {
	binding := SnapshotBinding{
		BootID:                 testBootID,
		BundleGeneration:       2,
		DomainPolicyGeneration: 2,
		ControlGeneration:      1,
		SnapshotGeneration:     1,
		SourceWatermark:        10,
	}
	return ReadinessInput{
		Target:          "synthetic.target",
		CapabilityID:    CapabilitySyntheticMemory,
		Domain:          policy.DomainRoot,
		ExpectedBinding: binding,
		SnapshotSHA256:  testDigest("snapshot"),
		Source: SourceEvidenceState{
			Fresh: true, BaselineAvailable: true,
		},
		Raw: RawComponentObservation{
			Target:                "synthetic.target",
			State:                 RawFailed,
			Reason:                ReasonFreshness,
			ConsecutiveFailures:   1,
			FailedDurationSeconds: 30,
			Owner:                 policy.DomainRoot,
			ObservedBinding:       binding,
		},
		Policy: ReadinessPolicy{
			CapabilityAllowed:              true,
			RequiredConsecutiveFailures:    3,
			RequiredFailureDurationSeconds: 120,
			BudgetRemaining:                1,
		},
	}
}
