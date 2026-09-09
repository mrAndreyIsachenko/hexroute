package userdaemon

import (
	"context"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// The two sides have to compare the same number.
//
// The request carried this runtime's control-state generation and root compared
// it with its own. Those are independent per-process counters — measured on the
// host on 2026-09-09 the user runtime was on 197355 and root on 202 — so the
// comparison could only match by coincidence and the request was refused as
// stale every time. The capability granted that morning was unreachable through
// the one path that leads to it.
//
// What both sides share, and what actually gates the act, is the generation of
// the signed policy under which it is permitted. Comparing that asks the
// question worth asking: are we both acting under the same authorization.
func TestTheRescueRequestCarriesTheAuthorizingGeneration(t *testing.T) {
	const bundleGeneration = 4
	const controlStateGeneration = 197355

	var sent ipc.Request
	executor := &recovery{
		requestID: func() (string, error) {
			return "11111111-1111-4111-8111-111111111111", nil
		},
		authorize: func(string, uint64, string) policy.ActionAuthorizationDecision {
			return policy.ActionAuthorizationDecision{Allowed: true}
		},
		bundleGeneration: func() uint64 { return bundleGeneration },
		roundTrip: func(_ context.Context, request ipc.Request) (ipc.Response, error) {
			sent = request
			return ipc.Response{
				Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
			}, nil
		},
	}

	plan := pritunlplan.Plan{
		State:  control.StateDegraded,
		Action: pritunlplan.ActionRequestRescue,
		Reason: pritunlplan.ReasonServiceNotRunning,
		Snapshot: control.Snapshot{
			SchemaVersion: control.SnapshotSchemaVersion,
			State:         control.StateDegraded,
			Generation:    controlStateGeneration,
		},
	}
	executor.perform(context.Background(), plan, "")

	if sent.ExpectedGeneration == controlStateGeneration {
		t.Fatal("the request carries this runtime's control-state generation; " +
			"root compares it with its own and refuses every time")
	}
	if sent.ExpectedGeneration != bundleGeneration {
		t.Fatalf("ExpectedGeneration = %d, want the authorizing generation %d",
			sent.ExpectedGeneration, bundleGeneration)
	}
}
