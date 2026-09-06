package pritunlrescue

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type launchctlRunner struct {
	output string
	err    error
	args   []string
	calls  int
}

func (runner *launchctlRunner) Output(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	runner.calls++
	runner.args = append([]string{name}, args...)
	if runner.err != nil {
		return nil, runner.err
	}
	return []byte(runner.output), nil
}

func testVerifier(t *testing.T, runner *launchctlRunner) *LaunchdVerifier {
	t.Helper()
	verifier, err := NewLaunchdVerifier(
		runner, "com.example.client.service",
		func() uint64 { return 9 },
		func(context.Context) (bool, error) { return true, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

// Root does not take the requester's word. The request says a service is
// stale; this is the looking.
func TestStalenessIsMeasuredAndNarrow(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		output string
		err    error
		stale  bool
	}{
		{
			name:   "loaded and not running",
			output: "system/com.example.client.service = {\n\tstate = not running\n}\n",
			stale:  true,
		},
		{
			name:   "running with a pid",
			output: "system/com.example.client.service = {\n\tstate = running\n\tpid = 4211\n}\n",
		},
		{
			name: "not loaded at all",
			err:  errors.New("exit status 113"),
		},
		{
			name:   "an answer with no state in it",
			output: "could not find service\n",
		},
		{
			// Restarting a service that is starting interrupts the recovery
			// already under way.
			name:   "starting",
			output: "system/com.example.client.service = {\n\tstate = spawn scheduled\n}\n",
		},
		{
			name:   "waiting to be run again",
			output: "system/com.example.client.service = {\n\tstate = waiting\n}\n",
		},
		{
			name:   "nothing",
			output: "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &launchctlRunner{output: testCase.output, err: testCase.err}
			stale, err := testVerifier(t, runner).PritunlServiceStale(context.Background())
			if err != nil || stale != testCase.stale {
				t.Fatalf("stale = %t (%v), want %t", stale, err, testCase.stale)
			}
			// It asks about the one named service and nothing else.
			joined := strings.Join(runner.args, " ")
			if joined != "/bin/launchctl print system/com.example.client.service" {
				t.Fatalf("asked: %q", joined)
			}
		})
	}
}

// The label is an argument to a privileged command and names the one service a
// rescue is for.
func TestOnlyANamedServiceCanBeAskedAbout(t *testing.T) {
	runner := &launchctlRunner{}
	generation := func() uint64 { return 1 }
	ready := func(context.Context) (bool, error) { return true, nil }
	for _, label := range []string{
		"", "com.example service", "com.example/../other", "-com.example",
		strings.Repeat("a", 200), "com.example\nservice",
	} {
		if _, err := NewLaunchdVerifier(runner, label, generation, ready); !errors.Is(err, ErrInvalidVerifier) {
			t.Fatalf("accepted label %q", label)
		}
	}
	if _, err := NewLaunchdVerifier(nil, "com.example.service", generation, ready); !errors.Is(err, ErrInvalidVerifier) {
		t.Fatal("accepted a verifier with no runner")
	}
	if _, err := NewLaunchdVerifier(runner, "com.example.service", nil, ready); !errors.Is(err, ErrInvalidVerifier) {
		t.Fatal("accepted a verifier that cannot answer for a generation")
	}
	if runner.calls != 0 {
		t.Fatal("a refused verifier still ran a command")
	}
}

// The handler already refuses everything but a revalidated restart. This is
// the verifier it refuses or approves with.
func TestTheVerifierSatisfiesTheContractTheHandlerNeeds(t *testing.T) {
	runner := &launchctlRunner{
		output: "system/com.example.client.service = {\n\tstate = not running\n}\n",
	}
	verifier := testVerifier(t, runner)
	if verifier.Generation() != 9 {
		t.Fatalf("generation = %d", verifier.Generation())
	}
	ready, err := verifier.OuterReady(context.Background())
	if err != nil || !ready {
		t.Fatalf("outer ready = %t, %v", ready, err)
	}
	if _, err := NewHandler(501, verifier); err != nil {
		t.Fatalf("the handler would not take the verifier: %v", err)
	}

	if (*LaunchdVerifier)(nil).Generation() != 0 {
		t.Fatal("a nil verifier claimed a generation")
	}
	if stale, err := (*LaunchdVerifier)(nil).PritunlServiceStale(
		context.Background(),
	); stale || !errors.Is(err, ErrInvalidVerifier) {
		t.Fatalf("a nil verifier answered: %t %v", stale, err)
	}
}
