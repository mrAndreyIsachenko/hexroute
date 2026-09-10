package userdaemon

import (
	"context"
	"errors"
	"testing"

	"net/netip"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/credentials"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlclient"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
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
	if got := executor.perform(context.Background(), plan, ""); got != recoveryUnequipped {
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

// Evidence is offered only when there is any.
//
// The address is evidence of a session that claims to be up and is carrying
// nothing. A session whose address is on an interface is carrying, so there is
// nothing to ask root to confirm, and asking anyway would put a false premise
// in front of the one runtime that can restart a production service.
func TestEvidenceIsOfferedOnlyWhenTheAddressIsMissing(t *testing.T) {
	present := Summary{ClientAddressPresent: true}
	if got := unreachableClientAddress(present); got != "" {
		t.Fatalf("evidence %q offered for a session that is carrying", got)
	}
	// And with nothing observed at all there is likewise nothing to offer.
	if got := unreachableClientAddress(Summary{}); got != "" {
		t.Fatalf("evidence %q offered when no address was observed", got)
	}
}

// And when the address is missing, the evidence is what is offered.
func TestTheMissingAddressIsWhatIsOffered(t *testing.T) {
	address := netip.MustParseAddr("192.168.244.136")
	observed := Evidence{
		Profile: userobserve.NewProfileObservation(
			true, userobserve.ProfileActive, false, address),
	}
	missing := Summary{Observed: observed, ClientAddressPresent: false}
	if got := unreachableClientAddress(missing); got != address.String() {
		t.Fatalf("evidence = %q, want %q", got, address)
	}
	carrying := Summary{Observed: observed, ClientAddressPresent: true}
	if got := unreachableClientAddress(carrying); got != "" {
		t.Fatalf("evidence %q offered for a session that is carrying", got)
	}
}

// The step a reconnect stopped at reaches the record.
//
// Being unable to act and acting and failing were told apart yesterday. What is
// still one word is where the attempt stopped, and the steps send the reader to
// different places: to the Keychain, to the client, to this code. On 2026-09-09
// an attempt failed and two explanations fitted, with nothing in the log to
// choose between them.
func TestTheStepAReconnectStoppedAtReachesTheRecord(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		failure error
		outcome recoveryOutcome
		reason  string
	}{
		{
			name:    "what it would submit could not be read",
			failure: pritunlclient.ErrCredentialsUnavailable,
			outcome: recoveryFailedCredentials,
			reason:  "recovery_credentials_unavailable",
		},
		{
			name:    "the client would not start the session",
			failure: pritunlclient.ErrSessionNotStarted,
			outcome: recoveryFailedNotStarted,
			reason:  "recovery_session_not_started",
		},
		{
			name:    "the code could not be derived",
			failure: pritunlclient.ErrOneTimeCodeUnavailable,
			outcome: recoveryFailedCode,
			reason:  "recovery_code_unavailable",
		},
		{
			name:    "something else went wrong",
			failure: errors.New("a fault this code did not name"),
			outcome: recoveryFailed,
			reason:  "recovery_failed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := reconnectOutcome(testCase.failure); got != testCase.outcome {
				t.Fatalf("outcome = %q, want %q", got, testCase.outcome)
			}
			if got := outcomeResult(testCase.outcome); got != logging.ResultDegraded {
				t.Fatalf("result = %q, want degraded — nothing refused this", got)
			}
			if got := string(outcomeReason(testCase.outcome)); got != testCase.reason {
				t.Fatalf("reason = %q, want %q", got, testCase.reason)
			}
		})
	}
}

// Through perform, because the mapping matters only if the loop records it.
//
// Driven through reconnectOutcome alone, replacing the call with the general
// failure changed nothing a test could see.
func TestPerformRecordsTheStepTheReconnectStoppedAt(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		failure error
		outcome recoveryOutcome
	}{
		{
			name:    "it could not read what it would submit",
			failure: pritunlclient.ErrCredentialsUnavailable,
			outcome: recoveryFailedCredentials,
		},
		{
			name:    "it could not derive the one-time code",
			failure: pritunlclient.ErrOneTimeCodeUnavailable,
			outcome: recoveryFailedCode,
		},
		{
			name:    "the client would not start the session",
			failure: pritunlclient.ErrSessionNotStarted,
			outcome: recoveryFailedNotStarted,
		},
		{
			name:    "a fault this code has not named",
			failure: errors.New("something else"),
			outcome: recoveryFailed,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			executor := &recovery{
				client: stoppedReconnector{err: testCase.failure},
				source: unreadableSource{},
				authorize: func(string, uint64, string) policy.ActionAuthorizationDecision {
					return policy.ActionAuthorizationDecision{Allowed: true}
				},
			}
			got := executor.perform(context.Background(), reconnectPlan(), "")
			if got != testCase.outcome {
				t.Fatalf("outcome = %q, want %q — the step did not reach the record",
					got, testCase.outcome)
			}
		})
	}
}

// A client that stops where it is told to. Where the real one stops is settled
// in its own package against a pinned clock; this settles what perform does
// with the answer, which is the only thing that decides the record.
type stoppedReconnector struct{ err error }

func (client stoppedReconnector) Reconnect(
	context.Context,
	credentials.Source,
) error {
	return client.err
}

// A source perform will accept and nothing will read.
type unreadableSource struct{}

func (unreadableSource) ReadPritunl(context.Context) (*credentials.Pritunl, error) {
	return nil, errors.New("nothing is readable here")
}
