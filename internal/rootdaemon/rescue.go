package rootdaemon

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue"
)

// pritunlRescuer performs the one production act this runtime is authorized to
// perform: restarting a stale Pritunl service, when a request from the user
// domain asks and this runtime's own observations agree.
//
// It is the smallest production authority in this system and is meant to stay
// that way. It restarts one named service and can do nothing else, it holds no
// credential and is given none, and it acts only while an active generation
// grants the capability.
type pritunlRescuer struct {
	handler   *pritunlrescue.Handler
	authorize func(uint64, string) policy.ActionAuthorizationDecision
	restart   func(context.Context) error
	peerUID   uint32
	// refusal records this runtime's own ground for saying no. The caller gets
	// a code; the ground the code cannot carry stays here.
	refusal func(logging.Reason)
}

// newPritunlRescuer builds the rescuer, or nothing.
//
// Absent a service label there is no named service to restart, so there is no
// capability to exercise and the runtime answers a request the way it always
// has: with a precondition failure.
func newPritunlRescuer(
	label string,
	operatorUID uint32,
	generation func() uint64,
	outerReady func(context.Context) (bool, error),
	authorize func(uint64, string) policy.ActionAuthorizationDecision,
	refusal func(logging.Reason),
) *pritunlRescuer {
	if label == "" || operatorUID == 0 || generation == nil ||
		outerReady == nil || authorize == nil || refusal == nil {
		return nil
	}
	runner := observe.ExecRunner{}
	verifier, err := pritunlrescue.NewLaunchdVerifier(
		runner, label, generation, outerReady,
	)
	if err != nil {
		return nil
	}
	handler, err := pritunlrescue.NewHandler(operatorUID, verifier)
	if err != nil {
		return nil
	}
	return &pritunlRescuer{
		handler:   handler,
		authorize: authorize,
		peerUID:   operatorUID,
		refusal:   refusal,
		restart: func(ctx context.Context) error {
			_, err := runner.Output(
				ctx, "/bin/launchctl", "kickstart", "-k", "system/"+label,
			)
			return err
		},
	}
}

// handle answers one request.
//
// The order is deliberate. The request is evaluated first — the handler checks
// the caller, the generation, and then looks at the service itself — and only a
// request that survives that is put to the authorization. Asking for authority
// to do something the situation does not call for would put a decision in the
// record that nothing needed.
// rescueEvaluationError names which of this runtime's own checks refused.
//
// Six outcomes used to arrive as one precondition failure, so a caller learned
// that it had been refused and nothing else. Most of them already have a code;
// using it puts the answer in a log the operator can read without a password.
//
// The two genuine preconditions share a code because the protocol has one for
// them. Telling those apart is what the reason written down here is for.
func rescueEvaluationError(evaluated error) ipc.ErrorCode {
	switch {
	case errors.Is(evaluated, ipc.ErrUnauthorizedPeer):
		return ipc.ErrorUnauthorized
	case errors.Is(evaluated, control.ErrStaleGeneration):
		return ipc.ErrorStaleGeneration
	case errors.Is(evaluated, pritunlrescue.ErrInvalidRequest):
		return ipc.ErrorInvalidRequest
	default:
		return ipc.ErrorPrecondition
	}
}

// rescueEvaluationReason is what this runtime writes down about its refusal.
//
// The code travels to the caller; this stays here, and it keeps apart the one
// thing the code cannot: which of the two preconditions failed.
func rescueEvaluationReason(evaluated error) logging.Reason {
	switch {
	case errors.Is(evaluated, ipc.ErrUnauthorizedPeer):
		return logging.ReasonUnauthorizedPeer
	case errors.Is(evaluated, control.ErrStaleGeneration):
		return logging.ReasonGenerationConflict
	case errors.Is(evaluated, pritunlrescue.ErrInvalidRequest):
		return logging.ReasonMalformedRequest
	case errors.Is(evaluated, pritunlrescue.ErrOuterNotReady):
		return logging.ReasonOuterPathNotReady
	case errors.Is(evaluated, pritunlrescue.ErrServiceNotStale):
		return logging.ReasonServiceNotStale
	default:
		return logging.ReasonRecoveryRefused
	}
}

// report writes down this runtime's own refusal, or does nothing when it was
// built without somewhere to write.
func (rescuer *pritunlRescuer) report(reason logging.Reason) {
	if rescuer == nil || rescuer.refusal == nil {
		return
	}
	rescuer.refusal(reason)
}

func (rescuer *pritunlRescuer) handle(
	ctx context.Context,
	request ipc.Request,
) ipc.Response {
	response := ipc.Response{
		Version:   ipc.ProtocolVersion,
		RequestID: request.RequestID,
	}
	if rescuer == nil || rescuer.handler == nil {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	decision, err := rescuer.handler.Evaluate(ctx, rescuer.peerUID, request)
	if err != nil || !decision.Approved {
		rescuer.report(rescueEvaluationReason(err))
		response.Error = rescueEvaluationError(err)
		return response
	}
	if authorized := rescuer.authorize(
		request.ExpectedGeneration, rescuePlanDigest(request),
	); !authorized.Allowed {
		// The situation calls for it and nothing signed for it. That is a
		// refusal on authority, not on the service's state — the same code as
		// an unwelcome peer, and a different reason written down beside it.
		rescuer.report(logging.ReasonUnsignedAuthority)
		response.Error = ipc.ErrorUnauthorized
		return response
	}
	if err := rescuer.restart(ctx); err != nil {
		response.Error = ipc.ErrorInternal
		return response
	}
	response.OK = true
	return response
}

// rescuePlanDigest binds the authorization to this request rather than to the
// act in general.
func rescuePlanDigest(request ipc.Request) string {
	digest, _, err := policy.CanonicalSHA256(struct {
		Action     string `json:"action"`
		Target     string `json:"target"`
		Generation uint64 `json:"generation"`
	}{
		Action:     string(request.Action),
		Target:     string(request.Target),
		Generation: request.ExpectedGeneration,
	})
	if err != nil {
		return ""
	}
	return digest
}

// answer routes one operator request.
//
// The Pritunl rescue is the one action this runtime performs rather than
// reports, so it is the one action that leaves the controller's hands. Every
// other request is answered exactly as it was before this existed.
func answer(
	ctx context.Context,
	controller controllerHandler,
	rescuer *pritunlRescuer,
	request ipc.Request,
) ipc.Response {
	if request.Action == ipc.ActionRescuePritunlService {
		return rescuer.handle(ctx, request)
	}
	return controller.Handle(request)
}

// controllerHandler is what answer needs of the operator controller, which is
// one method.
type controllerHandler interface {
	Handle(ipc.Request) ipc.Response
}

// rootObservations is what this runtime last saw of itself.
//
// The rescuer reads these rather than probing again. They are root's own
// observations either way, taken by the cycle that runs every interval, and a
// second probe taken at the moment of a request would answer a different
// question than the one the runtime has been answering all along.
type rootObservations struct {
	generation atomic.Uint64
	outerReady atomic.Bool
}

func (observations *rootObservations) record(generation uint64, outerReady bool) {
	if observations == nil {
		return
	}
	observations.generation.Store(generation)
	observations.outerReady.Store(outerReady)
}

func (observations *rootObservations) currentOuterReady(context.Context) (bool, error) {
	if observations == nil {
		return false, nil
	}
	return observations.outerReady.Load(), nil
}
