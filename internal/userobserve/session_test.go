package userobserve

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []runnerCall
}

func (runner *fakeRunner) Output(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	key := fakeKey(name, args...)
	runner.calls = append(runner.calls, runnerCall{
		name: name,
		args: append([]string(nil), args...),
	})
	if err := runner.errors[key]; err != nil {
		return nil, err
	}
	return runner.outputs[key], nil
}

func fakeKey(name string, args ...string) string {
	return name + "\x00" + stringsJoin(args)
}

func stringsJoin(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += "\x00"
		}
		result += value
	}
	return result
}

func TestUserSessionMatchesOnlyExpectedConsoleUID(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(statCommand, "-f", "%u", "/dev/console"): []byte("501\n"),
	}}
	observer, err := NewMacOSObserver(runner)
	if err != nil {
		t.Fatalf("NewMacOSObserver() error: %v", err)
	}

	observation, err := observer.UserSession(context.Background(), 501)
	if err != nil {
		t.Fatalf("UserSession() error: %v", err)
	}
	if observation.State != SessionActive || observation.ConsoleUID != 501 {
		t.Fatalf("UserSession() = %+v", observation)
	}

	want := []runnerCall{{
		name: statCommand,
		args: []string{"-f", "%u", "/dev/console"},
	}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestUserSessionTreatsLoginWindowAsInactive(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(statCommand, "-f", "%u", "/dev/console"): []byte("0\n"),
	}}
	observer, _ := NewMacOSObserver(runner)

	observation, err := observer.UserSession(context.Background(), 501)
	if err != nil {
		t.Fatalf("UserSession() error: %v", err)
	}
	if observation.State != SessionInactive || observation.ConsoleUID != 0 {
		t.Fatalf("UserSession() = %+v", observation)
	}
}

func TestUserSessionRejectsInvalidUIDWithoutCommand(t *testing.T) {
	runner := &fakeRunner{}
	observer, _ := NewMacOSObserver(runner)

	if _, err := observer.UserSession(context.Background(), 0); !errors.Is(
		err,
		ErrInvalidObservation,
	) {
		t.Fatalf("UserSession() error = %v, want %v", err, ErrInvalidObservation)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid UID invoked commands: %#v", runner.calls)
	}
}

func TestClamshellDistinguishesFullWakeAndDarkWake(t *testing.T) {
	lidKey := fakeKey(
		ioregCommand,
		"-r",
		"-k",
		"AppleClamshellState",
		"-d",
		"4",
	)
	wakeKey := fakeKey(
		ioregCommand,
		"-r",
		"-n",
		"IOPMrootDomain",
		"-d",
		"1",
	)
	runner := &fakeRunner{outputs: map[string][]byte{
		lidKey:  []byte(`"AppleClamshellState" = No`),
		wakeKey: []byte(`"Wake Type" = "UserActivity Assertion"`),
	}}
	observer, _ := NewMacOSObserver(runner)

	observation, err := observer.Clamshell(context.Background())
	if err != nil {
		t.Fatalf("Clamshell() error: %v", err)
	}
	if observation.Lid != observe.LidStateOpen ||
		observation.Wake != observe.WakeKindFull {
		t.Fatalf("Clamshell() = %+v", observation)
	}

	runner.outputs[lidKey] = []byte(`"AppleClamshellState" = Yes`)
	runner.outputs[wakeKey] = []byte(`"Wake Reason" = "DarkWake from Deep Idle"`)
	observation, err = observer.Clamshell(context.Background())
	if err != nil {
		t.Fatalf("Clamshell() DarkWake error: %v", err)
	}
	if observation.Lid != observe.LidStateClosed ||
		observation.Wake != observe.WakeKindDark {
		t.Fatalf("Clamshell() DarkWake = %+v", observation)
	}
}
