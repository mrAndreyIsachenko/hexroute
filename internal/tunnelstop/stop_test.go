package tunnelstop

import (
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"
)

// ignoring starts a process that ignores one signal and says when it is ready to
// be sent another.
//
// The readiness is not decoration. Without it the signal races the shell's own
// `trap`, and a stop measured before the trap is installed measures the race:
// under a mutation that sent an interrupt instead, one test went on passing
// because its process died before it could ignore anything, and the other went
// on passing because its process had not yet stopped ignoring.
func ignoring(t *testing.T, signal string) *exec.Cmd {
	t.Helper()
	command := exec.Command("/bin/sh", "-c",
		"trap '' "+signal+"; echo ready; sleep 30")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	ready := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(output, ready); err != nil {
		t.Fatalf("the process never said it was ready: %v", err)
	}
	return command
}

// Both callers reach the same stop, and it is the one that works.
//
// There were two, and they disagreed on the signal and on the window. Measured
// 2026-09-26 on the same tunnel: SIGTERM ended it in about 2.5 seconds twice,
// and an interrupt did not end it in thirty.
func TestAProcessThatTakesSIGTERMIsStopped(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "sleep 30")
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	// Reaped as it exits. A child nobody waits for stays signalable, so a test
	// that did not would be measuring the caller's parenthood rather than the
	// stop; the tunnel this ends is nobody's child here.
	reaped := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(reaped)
	}()
	began := time.Now()
	if err := Stop(command.Process.Pid); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if took := time.Since(began); took > Deadline {
		t.Fatalf("the stop took %s, past its own window", took)
	}
	<-reaped
}

// The signal is SIGTERM, and that is the difference between the two paths there
// were.
//
// A process that ignores an interrupt and takes a termination is stopped here.
// Measured 2026-09-26 on the tunnel itself: the interrupt did not end it in
// thirty seconds and the termination ended it in about 2.5, twice — so a stop
// that sent the other signal would leave the caller waiting for a process that
// is not going anywhere.
func TestTheSignalIsTheOneTheTunnelTakes(t *testing.T) {
	command := ignoring(t, "INT")
	reaped := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(reaped)
	}()
	if err := Stop(command.Process.Pid); err != nil {
		t.Fatalf("a process that ignores an interrupt was not stopped: %v", err)
	}
	<-reaped
}

// A process that ignores it outlives the window and is reported, rather than
// leaving the caller believing the tunnel is gone.
func TestAProcessThatIgnoresItIsReported(t *testing.T) {
	command := ignoring(t, "TERM")
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	if err := Stop(command.Process.Pid); !errors.Is(err, ErrStillRunning) {
		t.Fatalf("Stop() = %v, want %v", err, ErrStillRunning)
	}
}

// A process that is already gone is not an error: what the caller wants is the
// process not running, and it is not running.
func TestAProcessAlreadyGoneIsNotAFailure(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 0")
	if err := command.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if err := Stop(command.Process.Pid); err != nil {
		t.Fatalf("Stop() on a process already gone: %v", err)
	}
}

// And nothing is signalled without a process to signal.
func TestNoProcessIsRefused(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if err := Stop(pid); err == nil {
			t.Fatalf("Stop(%d) signalled something", pid)
		}
	}
}
