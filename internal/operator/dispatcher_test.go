package operator

import (
	"context"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
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
	return ipc.Response{
		Version: request.Version, RequestID: request.RequestID, Error: ipc.ErrorInternal,
	}
}

func TestDispatcherSeparatesReadOnlyAndMutatingRequests(t *testing.T) {
	readOnly := &countingReadHandler{}
	mutating := &countingMutationHandler{}
	policyHandler := &countingPolicyHandler{allowed: true}
	dispatcher, err := NewDispatcher(readOnly, mutating, policyHandler)
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
	dispatcher, err := NewDispatcher(readOnly, mutating, policyHandler)
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
