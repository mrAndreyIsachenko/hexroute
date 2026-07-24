package sentinel

import (
	"context"
	"strings"
	"testing"
)

type runnerCall struct {
	name string
	args []string
}

type recordingRunner struct {
	calls []runnerCall
	err   error
}

func (runner *recordingRunner) Output(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	runner.calls = append(runner.calls, runnerCall{
		name: name,
		args: append([]string(nil), args...),
	})
	return nil, runner.err
}

func TestMacOSRestarterInvokesOnlyFixedHexroutedLabel(t *testing.T) {
	runner := &recordingRunner{}
	restarter, err := NewMacOSRootRestarter(runner)
	if err != nil {
		t.Fatalf("NewMacOSRootRestarter() error: %v", err)
	}
	if err := restarter.RestartHexrouted(context.Background()); err != nil {
		t.Fatalf("RestartHexrouted() error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	call := runner.calls[0]
	if call.name != "/bin/launchctl" ||
		len(call.args) != 3 ||
		call.args[0] != "kickstart" ||
		call.args[1] != "-k" ||
		call.args[2] != "system/com.hexroute.observe.hexrouted" {
		t.Fatalf("call = %#v", call)
	}
}

func TestMacOSRestarterHasNoProtectedMutationTarget(t *testing.T) {
	runner := &recordingRunner{}
	restarter, _ := NewMacOSRootRestarter(runner)
	_ = restarter.RestartHexrouted(context.Background())
	call := runner.calls[0]
	command := strings.ToLower(call.name + " " + strings.Join(call.args, " "))
	for _, forbidden := range []string{
		"route add",
		"route change",
		"route delete",
		"pritunl",
		"xray",
		"nginx",
		"mtg",
		"adguard",
	} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("fixed restart command contains protected target %q", forbidden)
		}
	}
}
