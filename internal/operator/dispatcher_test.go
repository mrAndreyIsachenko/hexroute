package operator

import (
	"context"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type countingReadHandler struct {
	calls int
}

func (handler *countingReadHandler) Handle(request ipc.Request) ipc.Response {
	handler.calls++
	return internalResponse(request)
}

type countingMutationHandler struct {
	calls int
}

type countingPolicyHandler struct {
	calls   int
	allowed bool
	status  *ipc.PolicyStatusResult
}

func (handler *countingPolicyHandler) MutationAllowed() bool { return handler.allowed }

func (handler *countingMutationHandler) HandleIPC(
	context.Context,
	ipc.Request,
) ipc.Response {
	handler.calls++
	return ipc.Response{
		Version:   ipc.ProtocolVersion,
		RequestID: "mutating",
		Error:     ipc.ErrorInternal,
	}
}

func (handler *countingPolicyHandler) HandleIPC(
	_ context.Context,
	request ipc.Request,
) ipc.Response {
	handler.calls++
	if request.Action == ipc.ActionPolicyStatus && handler.status != nil {
		return ipc.Response{
			Version: request.Version, RequestID: request.RequestID, OK: true,
			PolicyStatus: handler.status,
		}
	}
	return ipc.Response{
		Version: request.Version, RequestID: request.RequestID, Error: ipc.ErrorInternal,
	}
}

type successfulReadHandler struct{}

func (successfulReadHandler) Handle(request ipc.Request) ipc.Response {
	status := ipc.Status{
		Role: ipc.RoleRoot, Mode: ipc.ModeObserveOnly,
		State: control.StateHealthy, Generation: 4,
	}
	return ipc.Response{
		Version: request.Version, RequestID: request.RequestID, OK: true,
		Status: &status,
	}
}

func TestDispatcherSeparatesReadOnlyAndMutatingRequests(t *testing.T) {
	readOnly := &countingReadHandler{}
	mutating := &countingMutationHandler{}
	policyHandler := &countingPolicyHandler{allowed: true}
	dispatcher, err := NewDispatcher(readOnly, mutating, policyHandler, nil)
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	for _, action := range []ipc.Action{
		ipc.ActionStatus,
		ipc.ActionExportDiagnostics,
	} {
		dispatcher.HandleIPC(context.Background(), ipc.Request{
			Version:   ipc.ProtocolVersion,
			RequestID: "read-only",
			Action:    action,
		})
	}
	for _, action := range []ipc.Action{
		ipc.ActionResumeTarget,
		ipc.ActionRescuePritunlService,
	} {
		dispatcher.HandleIPC(context.Background(), ipc.Request{
			Version:   ipc.ProtocolVersion,
			RequestID: "mutating",
			Action:    action,
		})
	}
	for _, action := range []ipc.Action{
		ipc.ActionPolicyStatus,
		ipc.ActionPreparePolicy,
		ipc.ActionCommitPolicy,
		ipc.ActionAbortPolicy,
	} {
		dispatcher.HandleIPC(context.Background(), ipc.Request{
			Version: ipc.ProtocolVersion, RequestID: "policy", Action: action,
		})
	}
	if readOnly.calls != 2 || mutating.calls != 2 || policyHandler.calls != 4 {
		t.Fatalf(
			"read-only calls=%d mutating calls=%d policy calls=%d",
			readOnly.calls,
			mutating.calls,
			policyHandler.calls,
		)
	}
}

func TestDispatcherBlocksMutationsDuringPolicyMismatch(t *testing.T) {
	readOnly := &countingReadHandler{}
	mutating := &countingMutationHandler{}
	policyHandler := &countingPolicyHandler{allowed: false}
	dispatcher, err := NewDispatcher(readOnly, mutating, policyHandler, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := dispatcher.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "blocked-mutation",
		Action: ipc.ActionResumeTarget,
	})
	if response.Error != ipc.ErrorPrecondition || mutating.calls != 0 {
		t.Fatalf("response=%+v mutation calls=%d", response, mutating.calls)
	}
}

func TestDispatcherAddsMatchingPolicySnapshotToReadStatus(t *testing.T) {
	policyStatus := &ipc.PolicyStatusResult{
		Status: policy.Status{
			Schema: policy.PolicyStatusSchema, Domain: policy.DomainRoot,
			State: policy.PolicyActive, BundleGeneration: 7,
			PolicyGeneration: 5, ManifestSHA256: strings.Repeat("a", 64),
			ActivatedAt: "2030-01-01T00:00:00Z", Reason: policy.ReasonNone,
		},
		AuthorizationSuspension: policy.AuthorizationSuspension{
			Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
		},
	}
	policyHandler := &countingPolicyHandler{status: policyStatus}
	dispatcher, err := NewDispatcher(
		successfulReadHandler{}, &countingMutationHandler{}, policyHandler, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := dispatcher.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "status-with-policy",
		Action: ipc.ActionStatus,
	})
	if !response.OK || response.Status == nil || response.Status.Policy == nil ||
		response.Status.Policy.Status.BundleGeneration != 7 || policyHandler.calls != 1 {
		t.Fatalf("response=%+v policy calls=%d", response, policyHandler.calls)
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("response validation error = %v", err)
	}
}
