// Package tunnelstop ends a tunnel process.
//
// It exists because there were two of these and they disagreed. The executor
// sent SIGTERM and waited ten seconds; the handover sent an interrupt and waited
// its possession window. Measured 2026-09-26 on the same tunnel: the first ended
// it in about 2.5 seconds, twice; the second did not end it in thirty, and the
// release that needed it completed only at ninety. Whether the interrupt needed
// longer or the second attempt finished what the first began was not separated
// by that reading, and one path removes the question.
//
// It is its own package because the two callers cannot share one otherwise: the
// handover verifies a signed configuration version and the always-running
// daemons are kept off that path, so nothing the daemons import may reach it.
package tunnelstop

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	// Deadline is how long a stop waits for the process to be gone. A stop that
	// returned while the process was still there would let a second tunnel
	// start beside it.
	Deadline = 10 * time.Second
	poll     = 200 * time.Millisecond
)

// ErrStillRunning is a process that outlived the window.
var ErrStillRunning = errors.New("the tunnel process did not end")

// Stop ends the process and waits until it is gone.
//
// A process that is already gone is not an error: what the caller wants is the
// process not running, and it is not running.
func Stop(pid int) error {
	if pid <= 0 {
		return errors.New("no such tunnel process to stop")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// A process that is already gone refuses the signal, and that refusal
		// is the answer the caller wanted: what it asked for is the process not
		// running. Reported as a failure it would fail a rebuild for a tunnel
		// that died between the cycle observing it and this stop.
		if gone(process) {
			return nil
		}
		return err
	}
	for waited := time.Duration(0); waited < Deadline; waited += poll {
		if gone(process) {
			return nil
		}
		time.Sleep(poll)
	}
	return ErrStillRunning
}

// gone says the process cannot be signalled any more.
//
// Signal zero is the question without the act. It answers about a process that
// has been reaped; one that has exited and not been waited for is still there
// to signal, which is a property of being someone's child and not of the tunnel
// this ends — the tunnel is started through another binary and belongs to no
// runtime here.
func gone(process *os.Process) bool {
	return process.Signal(syscall.Signal(0)) != nil
}
