package pritunlrescue

import (
	"context"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

type RootVerifier interface {
	Generation() uint64
	OuterReady(context.Context) (bool, error)
	PritunlServiceStale(context.Context) (bool, error)
}

type Decision struct {
	Approved bool
	Action   control.Action
}

type Handler struct {
	allowedUID uint32
	verifier   RootVerifier
}

var (
	ErrInvalidRequest = errors.New("invalid Pritunl rescue request")
	ErrPrecondition   = errors.New("pritunl rescue precondition failed")
)

func NewRequest(requestID string, expectedGeneration uint64) (ipc.Request, error) {
	request := ipc.Request{
		Version:            ipc.ProtocolVersion,
		RequestID:          requestID,
		Action:             ipc.ActionRescuePritunlService,
		Target:             control.ComponentPritunl,
		ExpectedGeneration: expectedGeneration,
	}
	if err := request.Validate(); err != nil {
		return ipc.Request{}, ErrInvalidRequest
	}
	return request, nil
}

func NewHandler(allowedUID uint32, verifier RootVerifier) (*Handler, error) {
	if allowedUID == 0 || verifier == nil {
		return nil, ErrInvalidRequest
	}
	return &Handler{
		allowedUID: allowedUID,
		verifier:   verifier,
	}, nil
}

func (handler *Handler) Evaluate(
	ctx context.Context,
	peerUID uint32,
	request ipc.Request,
) (Decision, error) {
	if handler == nil || handler.verifier == nil {
		return Decision{}, ErrInvalidRequest
	}
	if err := request.Validate(); err != nil ||
		request.Action != ipc.ActionRescuePritunlService ||
		request.Target != control.ComponentPritunl {
		return Decision{}, ErrInvalidRequest
	}
	if peerUID != handler.allowedUID {
		return Decision{}, ipc.ErrUnauthorizedPeer
	}

	generation := handler.verifier.Generation()
	if request.ExpectedGeneration != generation {
		return Decision{}, control.ErrStaleGeneration
	}
	outerReady, err := handler.verifier.OuterReady(ctx)
	if err != nil {
		return Decision{}, err
	}
	if !outerReady {
		return Decision{}, ErrPrecondition
	}
	stale, err := handler.verifier.PritunlServiceStale(ctx)
	if err != nil {
		return Decision{}, err
	}
	if !stale {
		return Decision{}, ErrPrecondition
	}

	action := control.Action{
		Kind:       control.ActionRestart,
		Target:     control.TargetPritunlService,
		Generation: generation,
		Reason:     control.ReasonProbeFailed,
	}
	if err := safety.ValidateAction(action); err != nil {
		return Decision{}, err
	}
	return Decision{
		Approved: true,
		Action:   action,
	}, nil
}
