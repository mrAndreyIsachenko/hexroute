package rootdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
)

// refusingWriter refuses exactly the records naming one event, and takes the
// rest.
//
// It names an event rather than counting writes: a fake that refused the nth
// write would be coupled to how many records a startup happens to emit, and
// would stop testing what it was written for the moment one was added.
type refusingWriter struct {
	event string
	taken bytes.Buffer
}

func (writer *refusingWriter) Write(record []byte) (int, error) {
	if strings.Contains(string(record), `"event":"`+writer.event+`"`) {
		return 0, errors.New("this log refused the record")
	}
	return writer.taken.Write(record)
}

// privateDir is a directory only its owner may enter.
//
// `t.TempDir` hands back a numbered subdirectory created 0755, and the heartbeat
// refuses to live anywhere else can be read — which reads as
// `heartbeat_unavailable` and looks like a broken fixture.
func privateDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	return path
}

func stopTestController(t *testing.T) *operator.Controller {
	t.Helper()
	controller, err := operator.NewController(
		ipc.RoleRoot,
		ipc.ModeObserveOnly,
		[]control.Component{control.ComponentTunnel},
		control.NewSnapshot(control.StateHealthy),
		control.ReasonNone,
		nil,
		func() control.Tick { return 7 },
	)
	if err != nil {
		t.Fatalf("operator.NewController() error: %v", err)
	}
	return controller
}

func stopTestLogger(t *testing.T, writer *bytes.Buffer) *logging.Logger {
	t.Helper()
	logger, err := logging.New(writer, logging.ComponentDaemon)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	return logger
}

// The loop says what ended it, and each ending has its own name.
//
// Measured 2026-09-26: the root daemon ended and left a `daemon_started`, no
// stop, nothing above `info` in either log, and a restart count under launchd.
// Every exit of this loop became one exit code, so the defect that ended it
// cannot be diagnosed from what was kept.
func TestTheLoopSaysWhatEndedIt(t *testing.T) {
	t.Run("the runtime was built wrong", func(t *testing.T) {
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), 0, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{}, stopTestController(t), nil, nil,
			stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
		if err == nil {
			t.Fatal("a loop with no interval ran")
		}
		if reason != logging.ReasonInvalidRuntime {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("a log record could not be written", func(t *testing.T) {
		writer := &refusingWriter{event: "daemon_started"}
		logger, err := logging.New(writer, logging.ComponentDaemon)
		if err != nil {
			t.Fatalf("logging.New() error: %v", err)
		}
		reason, err := observeLoop(
			context.Background(), time.Minute, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{}, stopTestController(t), nil, nil,
			logger, nil, nil, &rootObservations{}, nil)
		if err == nil {
			t.Fatal("a loop whose log refused its own start ran")
		}
		if reason != logging.ReasonJournalUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	// A summary whose state this runtime does not know is the runtime's own
	// fault, and arrives through the same call as a log that refused a record.
	// Naming the journal for it would send the reader to the wrong subsystem.
	t.Run("the summary was not one this runtime knows", func(t *testing.T) {
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleState("a state nobody wrote")}},
			&fixedHeartbeat{}, stopTestController(t), nil, nil,
			stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
		if err == nil {
			t.Fatal("a summary nobody can report was reported")
		}
		if reason != logging.ReasonInvalidRuntime {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("the publication failed", func(t *testing.T) {
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleHealthy}}, failingHeartbeat{}, stopTestController(t), nil, nil,
			stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
		if err == nil {
			t.Fatal("the heartbeat refused and the loop carried on")
		}
		if reason != logging.ReasonPublicationFailed {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The fold of the read model ends the loop only when the log refuses its
	// record. Everything the fold fails at of its own — observing, sampling,
	// recording — it reports and carries on from, so this exit is the journal
	// and naming the read model would be a distinction the code does not make.
	t.Run("the fold's record could not be written", func(t *testing.T) {
		root := t.TempDir()
		reader, err := connectivityhost.Open(
			filepath.Join(root, "host"), "boot", filepath.Join(root, "event-archive"))
		if err != nil {
			t.Fatalf("connectivityhost.Open: %v", err)
		}
		// Only the fold's own record, so the summary before it is taken and the
		// loop reaches the fold.
		writer := &refusingWriter{event: "connectivity_snapshot"}
		logger, err := logging.New(writer, logging.ComponentDaemon)
		if err != nil {
			t.Fatalf("logging.New() error: %v", err)
		}
		reason, err := observeLoop(
			context.Background(), time.Minute, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{},
			stopTestController(t), nil, nil, logger, reader, nil,
			&rootObservations{}, nil)
		if err == nil {
			t.Fatal("the fold's record was refused and the loop carried on")
		}
		if reason != logging.ReasonJournalUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The control state refusing is its own name. It is the one exit that is
	// neither a log nor a socket, and a runtime whose own state cannot be
	// updated is answering the operator with something it did not write.
	t.Run("the control state could not be updated", func(t *testing.T) {
		ahead := control.NewSnapshot(control.StateHealthy)
		ahead.LastTick = 99
		controller, err := operator.NewController(
			ipc.RoleRoot, ipc.ModeObserveOnly,
			[]control.Component{control.ComponentTunnel},
			ahead, control.ReasonNone, nil, func() control.Tick { return 7 })
		if err != nil {
			t.Fatalf("operator.NewController() error: %v", err)
		}
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true,
			func() control.Tick { return 7 }, func() time.Duration { return 0 },
			fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{},
			controller, nil, nil,
			stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
		if err == nil {
			t.Fatal("the controller refused the update and the loop carried on")
		}
		if reason != logging.ReasonControlStateUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The socket ending is fatal whether it says why or not, and both endings
	// are the same loss of the one way in.
	for name, done := range map[string]chan error{
		"the operator socket ended with a fault":    make(chan error, 1),
		"the operator socket closed saying nothing": make(chan error),
	} {
		t.Run(name, func(t *testing.T) {
			if cap(done) > 0 {
				done <- errors.New("the socket server ended")
			} else {
				close(done)
			}
			var output bytes.Buffer
			reason, err := observeLoop(
				context.Background(), time.Minute, false,
				func() control.Tick { return 7 }, func() time.Duration { return 0 },
				fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{}, stopTestController(t), nil, done,
				stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
			if err == nil {
				t.Fatal("the socket ended and the loop carried on")
			}
			if reason != logging.ReasonOperatorSocketEnded {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}

// An ending that was asked for names no failure, and keeps the record it had.
func TestAnEndingThatWasAskedForNamesNoFailure(t *testing.T) {
	for name, once := range map[string]bool{"one cycle": true, "a cancelled context": false} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if !once {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			var output bytes.Buffer
			reason, err := observeLoop(
				ctx, time.Minute, once,
				func() control.Tick { return 7 }, func() time.Duration { return 0 },
				fixedCycler{summary: Summary{State: CycleHealthy}}, &fixedHeartbeat{}, stopTestController(t), nil, nil,
				stopTestLogger(t, &output), nil, nil, &rootObservations{}, nil)
			if err != nil {
				t.Fatalf("observeLoop: %v", err)
			}
			if reason != "" {
				t.Fatalf("an ending nobody failed on named %q", reason)
			}
			if !strings.Contains(output.String(), `"event":"daemon_stopped"`) ||
				!strings.Contains(output.String(), `"result":"ok"`) {
				t.Fatalf("output = %q", output.String())
			}
		})
	}
}

// The stop is reported through the log the failure cannot have broken.
//
// The journal is one of the things that ends this runtime, so a record
// attempted there would go through what just broke. This drives the real
// command: its log refuses the record the loop begins with, and the stop has to
// arrive on the other stream.
func TestAStopIsReportedThroughTheOtherLog(t *testing.T) {
	config := observeConfigFile(t)
	heartbeat := filepath.Join(privateDir(t), "control-loop.heartbeat.json")
	refusing := &refusingWriter{event: "daemon_started"}
	var stderr bytes.Buffer

	code := Run([]string{
		"--observe", "--once", "--config", config, "--heartbeat", heartbeat,
	}, refusing, &stderr)

	if code != 1 {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		var record struct {
			Event  string `json:"event"`
			Result string `json:"result"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record.Event == "daemon_stopped" {
			found = true
			if record.Result != "degraded" ||
				record.Reason != string(logging.ReasonJournalUnwritable) {
				t.Fatalf("the stop was reported as %+v", record)
			}
		}
	}
	if !found {
		t.Fatalf("no stop was reported at all: %q", stderr.String())
	}
	if _, err := os.Stat(heartbeat); err == nil {
		t.Log("the heartbeat was published before the log refused, which is fine")
	}
}

// Failing to say it does not change the ending.
//
// A runtime that can write neither log still stops, with the status the failure
// calls for. Letting the record fail the stop would turn a broken stderr into an
// illegible ending.
func TestARuntimeThatCanWriteNeitherLogStillStops(t *testing.T) {
	config := observeConfigFile(t)
	heartbeat := filepath.Join(privateDir(t), "control-loop.heartbeat.json")

	code := Run([]string{
		"--observe", "--once", "--config", config, "--heartbeat", heartbeat,
	}, &refusingWriter{event: "daemon_started"}, &refusingWriter{event: "daemon_stopped"})

	if code != 1 {
		t.Fatalf("Run() = %d", code)
	}
}
