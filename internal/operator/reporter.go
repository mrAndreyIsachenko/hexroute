package operator

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

type RejectionLogger struct {
	logger *logging.Logger
}

func NewRejectionLogger(logger *logging.Logger) (*RejectionLogger, error) {
	if logger == nil {
		return nil, ErrInvalidController
	}
	return &RejectionLogger{logger: logger}, nil
}

func (reporter *RejectionLogger) ReportIPCRejection(err error) {
	if reporter == nil || reporter.logger == nil {
		return
	}
	reason := logging.ReasonMalformedRequest
	if named, ok := RejectionReason(err); ok {
		reason = named
	}
	_ = reporter.logger.Emit(
		logging.LevelWarn,
		logging.EventIPCRejected,
		logging.ResultRejected,
		reason,
	)
}

// RejectionReason names the check an IPC error came from.
//
// Every refusal the IPC layer can produce names the check that made it.
// Collapsing them into one reason is not a smaller log, it is a wrong one: a
// request refused for its target, its domain, its identifier and its payload
// all read identically, and the reader goes looking somewhere else.
//
// The second result says whether the error was named at all. A caller that can
// also fail for reasons outside this layer — a publisher whose round trip may
// never reach a peer — has to tell "the IPC layer refused this" from "the IPC
// layer has nothing to say about it", and one reason cannot carry both.
func RejectionReason(err error) (logging.Reason, bool) {
	var reason logging.Reason
	switch {
	case errors.Is(err, ipc.ErrUnauthorizedPeer):
		reason = logging.ReasonUnauthorizedPeer
	case errors.Is(err, ipc.ErrFrameTooLarge):
		reason = logging.ReasonOversizedRequest
	case errors.Is(err, ipc.ErrPeerSilent):
		reason = logging.ReasonPeerSilent
	case errors.Is(err, ipc.ErrMalformedFrame):
		reason = logging.ReasonMalformedFrame
	case errors.Is(err, ipc.ErrUnsupportedVersion):
		reason = logging.ReasonUnsupportedVersion
	case errors.Is(err, ipc.ErrUnknownAction):
		reason = logging.ReasonUnsupportedAction
	case errors.Is(err, ipc.ErrInvalidRequestID):
		reason = logging.ReasonInvalidRequestID
	case errors.Is(err, ipc.ErrInvalidTarget):
		reason = logging.ReasonInvalidTarget
	case errors.Is(err, ipc.ErrInvalidPolicyMessage):
		reason = logging.ReasonInvalidPolicyMessage
	case errors.Is(err, ipc.ErrInvalidReconcilerMessage):
		reason = logging.ReasonInvalidReconcilerMsg
	case errors.Is(err, ipc.ErrConnectivityDomain):
		reason = logging.ReasonConnectivityDomain
	case errors.Is(err, ipc.ErrInvalidRescueMessage):
		reason = logging.ReasonInvalidRescueMessage
	case errors.Is(err, ipc.ErrInvalidConnectivityMessage):
		reason = logging.ReasonInvalidConnectivityMsg
	default:
		return "", false
	}
	return reason, true
}
