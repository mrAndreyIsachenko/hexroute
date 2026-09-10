package userdaemon

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"context"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// A refusal must be reportable.
//
// outcomeResult turns a refusal into ResultRejected, and the logger refuses a
// rejected event that carries no reason — "rejected events require exactly one
// reason". The call site passed none, so writing down that root had said no
// returned an error, the observe loop returned it, and the daemon exited.
//
// It was unreachable until a service that is gone became a reason to ask: the
// only earlier route to a rescue request was a session reporting itself
// connected while carrying no traffic. Inducing the documented precondition on
// 2026-09-09 reached it, root refused, and the daemon restarted every forty
// seconds for as long as the service stayed down — the supervisor dying on the
// one outcome it exists to record.
func TestARefusedRecoveryCanBeWrittenDown(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		outcome recoveryOutcome
		result  string
	}{
		{
			name:    "root refused the request",
			outcome: recoveryRefusedPrecondition,
			result:  "rejected",
		},
		{
			name:    "root did not take the request up",
			outcome: recoveryAnswererFailed,
			result:  "degraded",
		},
		{name: "the act failed", outcome: recoveryFailed, result: "degraded"},
		{name: "nothing is equipped to act", outcome: recoveryUnequipped, result: "degraded"},
		{name: "the act was performed", outcome: recoveryDone, result: "ok"},
		{name: "it is only proposed", outcome: recoveryProposed, result: "proposed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			logger, err := logging.New(out, logging.ComponentUser)
			if err != nil {
				t.Fatalf("logger: %v", err)
			}
			gate := logging.NewChangeGate()
			summary := Summary{
				Outcome: testCase.outcome,
				Plan: pritunlplan.Plan{
					State:  control.StateDegraded,
					Action: pritunlplan.ActionRequestRescue,
					Reason: pritunlplan.ReasonServiceNotRunning,
				},
				Failures: 1,
			}

			if err := emitSummary(logger, gate, summary); err != nil {
				t.Fatalf("recording outcome %q returned %v — the observe loop "+
					"returns this, and the daemon exits", testCase.outcome, err)
			}

			var found bool
			for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				var event map[string]any
				if line == "" || json.Unmarshal([]byte(line), &event) != nil {
					continue
				}
				if event["event"] != string(logging.EventPritunlReconnect) {
					continue
				}
				found = true
				if event["result"] != testCase.result {
					t.Fatalf("result = %v, want %q", event["result"], testCase.result)
				}
				if event["result"] == "rejected" && event["reason"] == "" {
					t.Fatal("a rejected event was written with no reason")
				}
			}
			if !found {
				t.Fatal("the outcome was not recorded at all")
			}
		})
	}
}

// A refusal records which side refused and on what ground.
//
// Every refusal came back as one outcome and one reason, so the user log said
// the request had been refused and nothing more. Root answers with a code now;
// carrying it through means the operator reads the ground in their own log
// rather than in root's, which needs a password.
func TestARefusalRecordsTheGroundRootGave(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		code    ipc.ErrorCode
		outcome recoveryOutcome
		reason  string
	}{
		{
			name:    "nothing signed for it, or the peer is unwelcome",
			code:    ipc.ErrorUnauthorized,
			outcome: recoveryRefusedAuthority,
			reason:  "unsigned_authority",
		},
		{
			name:    "the generation is not the one in force",
			code:    ipc.ErrorStaleGeneration,
			outcome: recoveryRefusedStale,
			reason:  "generation_conflict",
		},
		{
			name:    "root's own checks were not satisfied",
			code:    ipc.ErrorPrecondition,
			outcome: recoveryRefusedPrecondition,
			reason:  "recovery_refused",
		},
		{
			name:    "root would not answer this request at all",
			code:    ipc.ErrorInvalidRequest,
			outcome: recoveryRefusedRequest,
			reason:  "malformed_request",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// Through perform, not the helper: the mapping matters only if the
			// outcome the loop records actually carries it.
			executor := &recovery{
				requestID: func() (string, error) {
					return "11111111-1111-4111-8111-111111111111", nil
				},
				authorize: func(string, uint64, string) policy.ActionAuthorizationDecision {
					return policy.ActionAuthorizationDecision{Allowed: true}
				},
				bundleGeneration: func() uint64 { return 4 },
				roundTrip: func(_ context.Context, request ipc.Request) (ipc.Response, error) {
					return ipc.Response{
						Version:   ipc.ProtocolVersion,
						RequestID: request.RequestID,
						Error:     testCase.code,
					}, nil
				},
			}
			got := executor.perform(context.Background(), pritunlplan.Plan{
				State:  control.StateDegraded,
				Action: pritunlplan.ActionRequestRescue,
				Reason: pritunlplan.ReasonServiceNotRunning,
				Snapshot: control.Snapshot{
					SchemaVersion: control.SnapshotSchemaVersion,
					State:         control.StateDegraded,
					Generation:    7,
				},
			}, "")
			if got != testCase.outcome {
				t.Fatalf("outcome for %q = %q, want %q",
					testCase.code, got, testCase.outcome)
			}
			result := outcomeResult(testCase.outcome)
			if result != logging.ResultRejected {
				t.Fatalf("a refusal reported %q, not rejected", result)
			}
			if got := string(outcomeReason(testCase.outcome)); got != testCase.reason {
				t.Fatalf("reason = %q, want %q", got, testCase.reason)
			}
		})
	}
}
