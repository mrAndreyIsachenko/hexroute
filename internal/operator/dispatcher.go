package operator

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

type Dispatcher struct {
	readOnly ReadHandler
	mutating MutationHandler
}

type ReadHandler interface {
	Handle(ipc.Request) ipc.Response
}

type MutationHandler interface {
	HandleIPC(context.Context, ipc.Request) ipc.Response
}

func NewDispatcher(
	readOnly ReadHandler,
	mutating MutationHandler,
) (*Dispatcher, error) {
	if readOnly == nil || mutating == nil {
		return nil, ErrInvalidController
	}
	return &Dispatcher{
		readOnly: readOnly,
		mutating: mutating,
	}, nil
}

func (dispatcher *Dispatcher) HandleIPC(
	ctx context.Context,
	request ipc.Request,
) ipc.Response {
	if dispatcher == nil {
		return internalResponse(request)
	}
	switch request.Action {
	case ipc.ActionStatus, ipc.ActionExportDiagnostics:
		return dispatcher.readOnly.Handle(request)
	case ipc.ActionResumeTarget, ipc.ActionRescuePritunlService:
		return dispatcher.mutating.HandleIPC(ctx, request)
	default:
		return ipc.Response{
			Version:   ipc.ProtocolVersion,
			RequestID: request.RequestID,
			Error:     ipc.ErrorInvalidRequest,
		}
	}
}
