package userdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// Being unable to try and trying and failing are different faults.
//
// Both were reported as a degraded cycle with no reason. They send the reader
// to opposite places: one says the deployment is missing something it was
// authorized to have, the other says the act was attempted and the attempt did
// not work.
//
// On 2026-09-09 a reconnect was attempted and reported degraded, and there was
// no way to tell from the log which of the two had happened — so no way to know
// whether waiting for the next reconnect could ever prove anything.
func TestBeingUnableIsNotTheSameAsFailing(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		outcome recoveryOutcome
		reason  string
	}{
		{
			name:    "authorized and missing what it would act with",
			outcome: recoveryUnequipped,
			reason:  "recovery_unequipped",
		},
		{
			name:    "attempted and the attempt did not work",
			outcome: recoveryFailed,
			reason:  "recovery_failed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := outcomeResult(testCase.outcome); got != logging.ResultDegraded {
				t.Fatalf("result = %q, want degraded", got)
			}
			if got := string(outcomeReason(testCase.outcome)); got != testCase.reason {
				t.Fatalf("reason = %q, want %q", got, testCase.reason)
			}
		})
	}
}

// The distinction has to survive the path that produces it.
func TestAReconnectWithNothingToActWithIsUnequipped(t *testing.T) {
	executor := &recovery{
		authorize: func(string, uint64, string) policy.ActionAuthorizationDecision {
			return policy.ActionAuthorizationDecision{Allowed: true}
		},
	}
	plan := reconnectPlan()
	if got := executor.perform(context.Background(), plan); got != recoveryUnequipped {
		t.Fatalf("a reconnect with no client and no credentials = %q, want %q",
			got, recoveryUnequipped)
	}
}

// A degraded cycle now carries a reason, and the logger refuses an event whose
// result and reason disagree — so this is also what keeps the daemon alive.
func TestADegradedOutcomeIsWritable(t *testing.T) {
	for _, outcome := range []recoveryOutcome{recoveryUnequipped, recoveryFailed} {
		summary := Summary{
			Outcome:  outcome,
			Failures: 1,
			Plan: pritunlplan.Plan{
				State:  control.StateDegraded,
				Action: pritunlplan.ActionReconnect,
				Reason: pritunlplan.ReasonReconnectAllowed,
			},
		}
		logger, err := logging.New(discardWriter{}, logging.ComponentUser)
		if err != nil {
			t.Fatalf("logger: %v", err)
		}
		if err := emitSummary(logger, logging.NewChangeGate(), summary); err != nil {
			t.Fatalf("recording %q returned %v", outcome, err)
		}
	}
}

func reconnectPlan() pritunlplan.Plan {
	return pritunlplan.Plan{
		State:  control.StateDegraded,
		Action: pritunlplan.ActionReconnect,
		Reason: pritunlplan.ReasonReconnectAllowed,
		Snapshot: control.Snapshot{
			SchemaVersion: control.SnapshotSchemaVersion,
			State:         control.StateDegraded,
			Generation:    9,
		},
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

var _ = errors.Is
