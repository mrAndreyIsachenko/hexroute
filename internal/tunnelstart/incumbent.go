package tunnelstart

import (
	"context"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

// ProcessObserver is what answers whether a tunnel process is running.
//
// It is the observer the root daemon already uses to decide process_gone. The
// handover borrows it rather than looking for sing-box its own way, so the
// runtime that stops the tunnel and the rule that reports on it cannot come to
// disagree about which process that is.
type ProcessObserver interface {
	SingBox(ctx context.Context, expectedParentPID int) (observe.ProcessObservation, error)
}

// Incumbent is the sing-box that holds the tunnel before this runtime takes it.
type Incumbent struct {
	Observer ProcessObserver
	// Runner stops it. The same runner that starts this runtime's own tunnel,
	// so both ends of the exchange are signalled the same way.
	Runner Runner
}

// Running reports the holder's pid, and whether there is one.
//
// Parentage is not asked for. The daemon passes its expected parent to tell its
// own child from a stranger's; the handover is asking about the stranger's, so
// a filter on parentage would answer no exactly when there is something to take.
func (incumbent *Incumbent) Running(ctx context.Context) (int, bool, error) {
	if incumbent.Observer == nil {
		return 0, false, fmt.Errorf("%w: nothing to observe the tunnel with", ErrMisplaced)
	}
	observation, err := incumbent.Observer.SingBox(ctx, 0)
	if err != nil {
		return 0, false, err
	}
	if !observation.Running {
		return 0, false, nil
	}
	return observation.Process.PID, true, nil
}

// Stop signals the holder.
func (incumbent *Incumbent) Stop(pid int) error {
	if incumbent.Runner == nil {
		return ErrMisplaced
	}
	if pid <= 0 {
		return fmt.Errorf("%w: no such tunnel process to stop", ErrMisplaced)
	}
	return incumbent.Runner.Stop(pid)
}
