package policycontrol

import (
	"context"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type recordingRejectionReporter struct {
	cause error
}

func (reporter *recordingRejectionReporter) ReportIPCRejection(err error) {
	reporter.cause = err
}

func TestRejectionReporterSuspendsOnlyOnPeerOwnershipViolation(t *testing.T) {
	now := time.Date(2030, time.January, 1, 0, 50, 0, 0, time.UTC)
	handler, err := NewUnavailableHandler(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return now }
	next := &recordingRejectionReporter{}
	reporter := NewRejectionReporter(next, handler)
	reporter.ReportIPCRejection(ipc.ErrMalformedFrame)
	status := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "not-suspended",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if status.PolicyStatus.AuthorizationSuspension.Suspended {
		t.Fatal("malformed frame suspended authorization")
	}
	reporter.ReportIPCRejection(ipc.ErrUnauthorizedPeer)
	status = handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "suspended",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	suspension := status.PolicyStatus.AuthorizationSuspension
	if next.cause != ipc.ErrUnauthorizedPeer || !suspension.Suspended ||
		suspension.Reason != policy.ReasonIPCOwnership ||
		suspension.Since != "2030-01-01T00:50:00Z" {
		t.Fatalf("next=%v suspension=%+v", next.cause, suspension)
	}
}
