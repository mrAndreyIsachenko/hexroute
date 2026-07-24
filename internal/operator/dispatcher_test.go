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

func TestDispatcherSeparatesReadOnlyAndMutatingRequests(t *testing.T) {
	readOnly := &countingReadHandler{}
	mutating := &countingMutationHandler{}
	dispatcher, err := NewDispatcher(readOnly, mutating)
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
	if readOnly.calls != 2 || mutating.calls != 2 {
		t.Fatalf("read-only calls=%d mutating calls=%d", readOnly.calls, mutating.calls)
	}
}
