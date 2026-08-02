package policycontrol

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// RejectionReporter preserves the existing bounded IPC incident and asserts a
// local authority overlay when peer ownership validation fails.
type RejectionReporter struct {
	next    ipc.RejectionReporter
	handler *Handler
}

func NewRejectionReporter(next ipc.RejectionReporter, handler *Handler) *RejectionReporter {
	return &RejectionReporter{next: next, handler: handler}
}

func (reporter *RejectionReporter) ReportIPCRejection(err error) {
	if reporter == nil {
		return
	}
	if errors.Is(err, ipc.ErrUnauthorizedPeer) && reporter.handler != nil {
		reporter.handler.SuspendAuthorization(policy.ReasonIPCOwnership)
	}
	if reporter.next != nil {
		reporter.next.ReportIPCRejection(err)
	}
}
