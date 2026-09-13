package tunnelstart

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type fixedObserver struct {
	observation observe.ProcessObservation
	err         error
	askedParent int
	asked       int
}

func (observer *fixedObserver) SingBox(
	_ context.Context, expectedParentPID int,
) (observe.ProcessObservation, error) {
	observer.asked++
	observer.askedParent = expectedParentPID
	return observer.observation, observer.err
}

type signalledRunner struct {
	pid    int
	calls  int
	refuse error
}

func (runner *signalledRunner) Start(context.Context, string, ...string) (int, error) {
	return 0, errors.New("not used")
}

func (runner *signalledRunner) Stop(pid int) error {
	runner.calls++
	runner.pid = pid
	return runner.refuse
}

// The incumbent is whatever sing-box is running, whoever started it.
//
// The daemon passes its own pid as the expected parent so it can tell its child
// from a stranger's. The handover is asking about the stranger's, so asking with
// a parent would report nothing running in exactly the case there is something
// to take — and this runtime would start a second tunnel beside it.
func TestTheIncumbentIsNotFilteredByParentage(t *testing.T) {
	observer := &fixedObserver{observation: observe.ProcessObservation{
		Running:    true,
		Process:    observe.Process{PID: 4242, ParentPID: 1, Executable: "sing-box"},
		OwnedChild: false,
	}}
	incumbent := &Incumbent{Observer: observer, Runner: &signalledRunner{}}

	pid, running, err := incumbent.Running(context.Background())
	if err != nil {
		t.Fatalf("Running: %v", err)
	}
	if !running || pid != 4242 {
		t.Fatalf("Running = %d, %v", pid, running)
	}
	if observer.askedParent != 0 {
		t.Fatalf("the holder was looked for under parent %d", observer.askedParent)
	}
}

func TestAnAbsentIncumbentReportsNoPID(t *testing.T) {
	incumbent := &Incumbent{
		Observer: &fixedObserver{observation: observe.ProcessObservation{}},
		Runner:   &signalledRunner{},
	}
	pid, running, err := incumbent.Running(context.Background())
	if err != nil || running || pid != 0 {
		t.Fatalf("Running = %d, %v, %v", pid, running, err)
	}
}

func TestAFailedObservationIsNotAnAbsentIncumbent(t *testing.T) {
	incumbent := &Incumbent{
		Observer: &fixedObserver{err: errors.New("ps refused")},
		Runner:   &signalledRunner{},
	}
	if _, running, err := incumbent.Running(context.Background()); err == nil || running {
		t.Fatalf("a failed reading reported running=%v err=%v", running, err)
	}
}

// Stopping signals the pid that was observed, and no other.
func TestStoppingSignalsTheObservedProcess(t *testing.T) {
	runner := &signalledRunner{}
	incumbent := &Incumbent{Observer: &fixedObserver{}, Runner: runner}

	if err := incumbent.Stop(4242); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runner.calls != 1 || runner.pid != 4242 {
		t.Fatalf("signalled pid %d, %d times", runner.pid, runner.calls)
	}
}

// Nothing is signalled when there is no pid to signal.
//
// Zero is what an absent observation reports, and on this platform a signal to
// process group zero reaches the whole group — including the terminal holding
// the transaction.
func TestStoppingRefusesAnEmptyPID(t *testing.T) {
	runner := &signalledRunner{}
	incumbent := &Incumbent{Observer: &fixedObserver{}, Runner: runner}

	if err := incumbent.Stop(0); !errors.Is(err, ErrMisplaced) {
		t.Fatalf("Stop(0) = %v, want ErrMisplaced", err)
	}
	if runner.calls != 0 {
		t.Fatalf("an empty pid was signalled %d times", runner.calls)
	}
}
