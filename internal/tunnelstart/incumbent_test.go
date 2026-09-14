package tunnelstart

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const incumbentConfig = "/Library/Application Support/twilight/supervisor/client/twilight-sing-box-tun.json"

type fixedObserver struct {
	observation observe.ProcessObservation
	err         error
	askedConfig string
	asked       int
}

func (observer *fixedObserver) Tunnel(
	_ context.Context, configPath string,
) (observe.ProcessObservation, error) {
	observer.asked++
	observer.askedConfig = configPath
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

// The incumbent is the process running the incumbent's configuration.
//
// Asking by name took the first process called sing-box, and the incumbent's
// own ingress probe is one.
func TestTheIncumbentIsAskedForByItsConfiguration(t *testing.T) {
	observer := &fixedObserver{observation: observe.ProcessObservation{
		Running: true,
		Process: observe.Process{PID: 4242, ParentPID: 1, Executable: "/opt/homebrew/bin/sing-box"},
	}}
	incumbent := &Incumbent{Observer: observer, ConfigPath: incumbentConfig, Runner: &signalledRunner{}}

	pid, running, err := incumbent.Running(context.Background())
	if err != nil {
		t.Fatalf("Running: %v", err)
	}
	if !running || pid != 4242 {
		t.Fatalf("Running = %d, %v", pid, running)
	}
	if observer.askedConfig != incumbentConfig {
		t.Fatalf("the holder was looked for under %q, want %q", observer.askedConfig, incumbentConfig)
	}
}

// An incumbent whose configuration is not named asks nothing.
func TestAnUnnamedIncumbentIsRefused(t *testing.T) {
	observer := &fixedObserver{}
	incumbent := &Incumbent{Observer: observer, Runner: &signalledRunner{}}
	if _, _, err := incumbent.Running(context.Background()); !errors.Is(err, ErrMisplaced) {
		t.Fatalf("Running without a configuration = %v, want ErrMisplaced", err)
	}
	if observer.asked != 0 {
		t.Fatal("an incumbent with no configuration was looked for anyway")
	}
}

func TestAnAbsentIncumbentReportsNoPID(t *testing.T) {
	incumbent := &Incumbent{
		Observer: &fixedObserver{}, ConfigPath: incumbentConfig, Runner: &signalledRunner{},
	}
	pid, running, err := incumbent.Running(context.Background())
	if err != nil || running || pid != 0 {
		t.Fatalf("Running = %d, %v, %v", pid, running, err)
	}
}

func TestAFailedObservationIsNotAnAbsentIncumbent(t *testing.T) {
	incumbent := &Incumbent{
		Observer:   &fixedObserver{err: errors.New("ps refused")},
		ConfigPath: incumbentConfig, Runner: &signalledRunner{},
	}
	if _, running, err := incumbent.Running(context.Background()); err == nil || running {
		t.Fatalf("a failed reading reported running=%v err=%v", running, err)
	}
}

// Stopping signals the pid that was observed, and no other.
func TestStoppingSignalsTheObservedProcess(t *testing.T) {
	runner := &signalledRunner{}
	incumbent := &Incumbent{Observer: &fixedObserver{}, ConfigPath: incumbentConfig, Runner: runner}
	if err := incumbent.Stop(4242); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runner.calls != 1 || runner.pid != 4242 {
		t.Fatalf("signalled pid %d, %d times", runner.pid, runner.calls)
	}
}

// Nothing is signalled when there is no pid to signal.
//
// Zero is what an absent observation reports, and a signal to process group
// zero reaches the whole group — including the terminal holding the transaction.
func TestStoppingRefusesAnEmptyPID(t *testing.T) {
	runner := &signalledRunner{}
	incumbent := &Incumbent{Observer: &fixedObserver{}, ConfigPath: incumbentConfig, Runner: runner}
	if err := incumbent.Stop(0); !errors.Is(err, ErrMisplaced) {
		t.Fatalf("Stop(0) = %v, want ErrMisplaced", err)
	}
	if runner.calls != 0 {
		t.Fatalf("an empty pid was signalled %d times", runner.calls)
	}
}
