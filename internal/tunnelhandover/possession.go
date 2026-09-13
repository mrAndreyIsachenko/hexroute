package tunnelhandover

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Incumbent is whatever holds the tunnel when the handover begins.
//
// Running answers with the same definition of "the tunnel process" that the
// decision rule uses for process_gone. If the two disagreed, this runtime could
// report having taken a tunnel it had not stopped, or stop something the rule
// does not count as the tunnel at all.
type Incumbent interface {
	Running(ctx context.Context) (int, bool, error)
	Stop(pid int) error
}

var (
	ErrNoIncumbent = errors.New("a real handover needs to know who holds the tunnel")
	ErrPossession  = errors.New("the tunnel could not be taken from its holder")
)

// possess stops whatever holds the tunnel, and waits until it is gone.
//
// It runs after the claim and before the start. The claim is what makes the
// process this runtime's to stop — the previous owner reads that file and steps
// back — and starting first would put two sing-box processes on one tunnel
// address, which is the one arrangement neither of them can recover from.
//
// Waiting for the previous owner to stop its own process was rejected. Its loop
// ticks at sixty seconds against a hundred-and-twenty-second deadline, so half
// the budget would go to waiting for somebody else to read a file.
//
// The previous owner checks the claim immediately before it execs, so it cannot
// restart what this stops. If it ever did, this waits for a tunnel that keeps
// coming back and gives up at the deadline rather than starting a second one.
func (transaction *Transaction) possess(ctx context.Context, now func() time.Time) error {
	pid, running, err := transaction.Incumbent.Running(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPossession, err)
	}
	if !running {
		// Nothing holds it. Ordinary on a machine whose previous owner has
		// already stopped, and not a reason to refuse.
		return nil
	}
	if err := transaction.Incumbent.Stop(pid); err != nil {
		return fmt.Errorf("%w: %v", ErrPossession, err)
	}

	deadline := now().Add(transaction.Policy.Possession)
	for {
		_, running, err := transaction.Incumbent.Running(ctx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrPossession, err)
		}
		if !running {
			return nil
		}
		if !now().Before(deadline) {
			return fmt.Errorf("%w: pid %d was still there after %s",
				ErrPossession, pid, transaction.Policy.Possession)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: interrupted while waiting for pid %d", ErrPossession, pid)
		case <-time.After(transaction.Policy.Between):
		}
	}
}
