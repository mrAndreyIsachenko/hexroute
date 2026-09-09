package pritunlrescue

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

type RootVerifier interface {
	Generation() uint64
	OuterReady(context.Context) (bool, error)
	PritunlServiceStale(context.Context) (bool, error)
	// TunnelAddressAbsent is this runtime looking for itself. The asking domain
	// says the session's address is on no interface; this answers whether that
	// is so here, which is a fact rather than the conclusion drawn from it.
	TunnelAddressAbsent(context.Context, string) (bool, error)
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
	// ErrPrecondition is what the named preconditions have in common.
	//
	// A caller that only needs to know the situation was refused matches on
	// this; one that needs to know which check refused it matches on the named
	// error. Collapsing them into this one alone is how a refusal arrived with
	// nothing to say for itself.
	ErrPrecondition = errors.New("pritunl rescue precondition failed")
	// ErrOuterNotReady is this runtime's own view of the path the service
	// needs. Restarting into a path that is not there repairs nothing.
	ErrOuterNotReady = fmt.Errorf("%w: the outer path is not ready", ErrPrecondition)
	// ErrServiceNotStale is the service not being in the state this act exists
	// to repair. It covers a service that is running and one that is not loaded
	// at all: neither is a loaded service that has stopped, and restarting on
	// either would act on something this runtime was not asked about.
	ErrServiceNotStale = fmt.Errorf("%w: the service is not stale", ErrPrecondition)
)

// NewRequest builds the request, with the evidence when there is any.
//
// An empty address asks only about the service's own state. A named one says
// this is what the session claims and no interface carries it — evidence the
// answering runtime confirms for itself rather than a conclusion it is asked to
// accept.
func NewRequest(
	requestID string,
	expectedGeneration uint64,
	unreachableClientAddress string,
) (ipc.Request, error) {
	request := ipc.Request{
		Version:            ipc.ProtocolVersion,
		RequestID:          requestID,
		Action:             ipc.ActionRescuePritunlService,
		Target:             control.ComponentPritunl,
		ExpectedGeneration: expectedGeneration,
	}
	if unreachableClientAddress != "" {
		request.RescuePritunlService = &ipc.RescuePritunlServiceRequest{
			UnreachableClientAddress: unreachableClientAddress,
		}
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

// blackholeConfirmed asks whether this runtime sees what the request describes.
//
// A request naming no address is not describing a blackhole, so there is
// nothing to confirm and nothing is approved on that ground.
func (handler *Handler) blackholeConfirmed(
	ctx context.Context,
	request ipc.Request,
) (bool, error) {
	if request.RescuePritunlService == nil ||
		request.RescuePritunlService.UnreachableClientAddress == "" {
		return false, nil
	}
	return handler.verifier.TunnelAddressAbsent(
		ctx, request.RescuePritunlService.UnreachableClientAddress,
	)
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
		return Decision{}, ErrOuterNotReady
	}
	stale, err := handler.verifier.PritunlServiceStale(ctx)
	if err != nil {
		return Decision{}, err
	}
	if !stale {
		// A service that is running is not stale, and until this existed that
		// ended it — which meant the one fault the asking domain actually sees,
		// a session up and carrying nothing, could never be approved. That
		// session's service is running; it is what makes it a blackhole.
		//
		// So the other question is asked, and asked of this runtime's own eyes:
		// is the address the session claims really on no interface.
		absent, err := handler.blackholeConfirmed(ctx, request)
		if err != nil {
			return Decision{}, err
		}
		if !absent {
			return Decision{}, ErrServiceNotStale
		}
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
