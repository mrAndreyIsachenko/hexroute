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
	switch {
	case errors.Is(err, ipc.ErrUnauthorizedPeer):
		reason = logging.ReasonUnauthorizedPeer
	case errors.Is(err, ipc.ErrFrameTooLarge):
		reason = logging.ReasonOversizedRequest
	case errors.Is(err, ipc.ErrUnsupportedVersion):
		reason = logging.ReasonUnsupportedVersion
	case errors.Is(err, ipc.ErrUnknownAction):
		reason = logging.ReasonUnsupportedAction
	}
	_ = reporter.logger.Emit(
		logging.LevelWarn,
		logging.EventIPCRejected,
		logging.ResultRejected,
		reason,
	)
}
