package userdaemon

import (
	"context"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// Root failing to answer is not root refusing.
//
// On 2026-09-10 a rescue came back refused and root's log held nothing about
// it. Both readings were open — root refused above the act, or root never took
// the request up — and this record could not separate them, because every code
// it did not name arrived as the reason that says root looked and disagreed.
func TestAnAnswererThatFailedIsNotARefusal(t *testing.T) {
	for _, testCase := range []struct {
		name string
		code ipc.ErrorCode
	}{
		{name: "root said it failed internally", code: ipc.ErrorInternal},
		{name: "root answered with nothing this can read", code: ipc.ErrorCode("")},
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
			if got == recoveryRefusedPrecondition {
				t.Fatal("an answerer that failed was reported as a refusal on the merits")
			}
			if reason := outcomeReason(got); reason != logging.ReasonRootInternal {
				t.Fatalf("reason = %q, want %q", reason, logging.ReasonRootInternal)
			}
			// Nothing refused this, so it is not a refusal. Reporting it as one
			// sends the reader to the policy for an answer that is not there.
			if result := outcomeResult(got); result != logging.ResultDegraded {
				t.Fatalf("result = %q, want %q", result, logging.ResultDegraded)
			}
		})
	}
}
