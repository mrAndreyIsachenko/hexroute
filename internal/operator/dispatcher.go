package operator

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

type Dispatcher struct {
	readOnly ReadHandler
	mutating MutationHandler
	policy   PolicyHandler
}

type ReadHandler interface {
	Handle(ipc.Request) ipc.Response
}

type MutationHandler interface {
	HandleIPC(context.Context, ipc.Request) ipc.Response
}

type PolicyHandler interface {
	HandleIPC(context.Context, ipc.Request) ipc.Response
}

func NewDispatcher(
	readOnly ReadHandler,
	mutating MutationHandler,
	policyHandler PolicyHandler,
) (*Dispatcher, error) {
	if readOnly == nil || mutating == nil || policyHandler == nil {
		return nil, ErrInvalidController
	}
	return &Dispatcher{
		readOnly: readOnly,
		mutating: mutating,
		policy:   policyHandler,
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
	case ipc.ActionPolicyStatus,
		ipc.ActionPreparePolicy,
		ipc.ActionCommitPolicy,
		ipc.ActionAbortPolicy:
		return dispatcher.policy.HandleIPC(ctx, request)
	default:
		return ipc.Response{
			Version:   ipc.ProtocolVersion,
			RequestID: request.RequestID,
			Error:     ipc.ErrorInvalidRequest,
		}
	}
}
