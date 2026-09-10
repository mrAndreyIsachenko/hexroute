package operator

import (
	"sync/atomic"

	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

// RefusalReporter records a refusal made above the handler that would have
// evaluated the request.
//
// Every other refusal in this system is made by something that already has
// somewhere to write. These two are not: a runtime that may not mutate at all
// never reaches the act's own checks, and a request no reader took up was not
// refused by anything — it was not answered. Both used to leave the caller a
// bare code and leave the runtime's log empty, which is how a refused rescue
// on 2026-09-10 could not be attributed at all.
type RefusalReporter interface {
	// ReportMutationRefused records a request refused because this runtime may
	// not perform mutations at all.
	ReportMutationRefused()
	// ReportRequestUnanswered records a request no reader took up.
	ReportRequestUnanswered()
}

// RefusalLogger writes down the refusals made above the act.
//
// It bounds repetition rather than gating on change. The condition behind a
// mutation refusal does not change while it holds — a runtime that may not
// mutate stays that way until a generation says otherwise — so a change gate
// would write once and then hide a condition that persists.
type RefusalLogger struct {
	logger     *logging.Logger
	refused    atomic.Uint64
	unanswered atomic.Uint64
}

// refusalReportEvery is how often a repeating refusal is written down after the
// first. The first is always written: a reader looking for the moment it began
// should not have to wait for a count to come round.
const refusalReportEvery = 60

func NewRefusalLogger(logger *logging.Logger) (*RefusalLogger, error) {
	if logger == nil {
		return nil, ErrInvalidController
	}
	return &RefusalLogger{logger: logger}, nil
}

func (reporter *RefusalLogger) ReportMutationRefused() {
	reporter.report(&reporter.refused, logging.ReasonMutationNotPermitted)
}

func (reporter *RefusalLogger) ReportRequestUnanswered() {
	reporter.report(&reporter.unanswered, logging.ReasonRequestNotTaken)
}

func (reporter *RefusalLogger) report(
	counter *atomic.Uint64,
	reason logging.Reason,
) {
	if reporter == nil || reporter.logger == nil {
		return
	}
	if seen := counter.Add(1); seen != 1 && seen%refusalReportEvery != 0 {
		return
	}
	_ = reporter.logger.Emit(
		logging.LevelWarn,
		logging.EventIPCRejected,
		logging.ResultRejected,
		reason,
	)
}
