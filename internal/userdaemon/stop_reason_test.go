package userdaemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycollect"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// refusingWriter refuses exactly the records naming one event, and takes the
// rest. It names an event rather than counting writes: a fake coupled to how
// many records a startup emits stops testing what it was written for the moment
// one is added.
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

// refusingStore is the snapshot on disk that cannot be written.
type refusingStore struct{}

func (refusingStore) Save(control.Snapshot) error {
	return errors.New("the state file refused the snapshot")
}

// stoppedClock is the time context a collector stamps a fact with.
type stoppedClock struct{}

func (stoppedClock) Wall() time.Time    { return time.Unix(1790000000, 0).UTC() }
func (stoppedClock) Tick() control.Tick { return 1 }

// unownedPublisher is the real publisher with a source that owns none of the
// components this domain speaks for, so the first fact it is asked for is
// refused.
//
// It is the real object rather than a fake because the loop holds the concrete
// publisher: a fake in its place would test a different exit than the one that
// runs. A root that does not answer is not this exit — a refusal costs the cycle
// nothing by design, which is why the fault has to be on this side.
func unownedPublisher(t *testing.T) *factPublisher {
	t.Helper()
	publisher, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock",
		filepath.Join(t.TempDir(), "connectivity-stream.json"), 15*time.Second)
	if err != nil || publisher == nil {
		t.Fatalf("newFactPublisher() error: %v", err)
	}
	collector, err := connectivitycollect.New(connectivitycollect.Options{
		Source: connectivity.SourceID("hexroute-root-observer"),
		Domain: policy.DomainUser,
		BootID: "boot-0000000000000000",
		Clock:  stoppedClock{},
		Random: rand.Reader,
	})
	if err != nil {
		t.Fatalf("connectivitycollect.New() error: %v", err)
	}
	for _, component := range userComponents() {
		publisher.sources[component] = collector
	}
	return publisher
}

func stopTestSummary() Summary {
	snapshot := control.NewSnapshot(control.StateHealthy)
	snapshot.Generation = 1
	return Summary{Plan: pritunlplan.Plan{
		ObserveOnly: true,
		State:       control.StateHealthy,
		Action:      pritunlplan.ActionNone,
		Reason:      pritunlplan.ReasonNone,
		Snapshot:    snapshot,
	}}
}

func stopTestStore(t *testing.T) StateStore {
	t.Helper()
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	store, _, err := openSnapshotStore(filepath.Join(stateDir, stateFileName))
	if err != nil {
		t.Fatalf("openSnapshotStore() error: %v", err)
	}
	return store
}

func stopTestController(t *testing.T, tick control.Tick) *operator.Controller {
	t.Helper()
	controller, err := operator.NewController(
		ipc.RoleUser, ipc.ModeObserveOnly,
		[]control.Component{control.ComponentPritunl},
		control.NewSnapshot(control.StateHealthy), control.ReasonNone, nil,
		func() control.Tick { return tick })
	if err != nil {
		t.Fatalf("operator.NewController() error: %v", err)
	}
	return controller
}

func stopTestLogger(t *testing.T, writer *bytes.Buffer) *logging.Logger {
	t.Helper()
	logger, err := logging.New(writer, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	return logger
}

// This runtime is asked the same question as the other one, rather than assumed
// to be different. A defect that ends it is as invisible as the defect that
// ended the root daemon on 2026-09-26, and this repository has already been
// caught writing a check for one side and reporting the other side ready.
func TestTheUserLoopSaysWhatEndedIt(t *testing.T) {
	t.Run("the runtime was built wrong", func(t *testing.T) {
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), 0, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
			stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
			stopTestLogger(t, &output), nil, &recovery{}, nil)
		if err == nil {
			t.Fatal("a loop with no interval ran")
		}
		if reason != logging.ReasonInvalidRuntime {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("a log record could not be written", func(t *testing.T) {
		writer := &refusingWriter{event: "daemon_started"}
		logger, err := logging.New(writer, logging.ComponentUser)
		if err != nil {
			t.Fatalf("logging.New() error: %v", err)
		}
		reason, err := observeLoop(
			context.Background(), time.Minute, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
			stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
			logger, nil, &recovery{}, nil)
		if err == nil {
			t.Fatal("a loop whose log refused its own start ran")
		}
		if reason != logging.ReasonJournalUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The publication is asked before the loop acts on its own conclusions, and
	// a root that is unreachable costs it nothing by design — so what ends the
	// loop here is a fact this domain cannot produce, not a peer that did not
	// answer.
	t.Run("the publication failed", func(t *testing.T) {
		summary := stopTestSummary()
		summary.Observed.Reached = true
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: summary}, stopTestStore(t),
			stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
			stopTestLogger(t, &output), unownedPublisher(t), &recovery{}, nil)
		if err == nil {
			t.Fatal("the publication refused and the loop carried on")
		}
		if reason != logging.ReasonPublicationFailed {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The cycle's own record, rather than the one the loop starts with: the
	// loop must reach the end of a cycle and still name the journal.
	t.Run("the cycle's record could not be written", func(t *testing.T) {
		writer := &refusingWriter{event: "observation_cycle"}
		logger, err := logging.New(writer, logging.ComponentUser)
		if err != nil {
			t.Fatalf("logging.New() error: %v", err)
		}
		reason, err := observeLoop(
			context.Background(), time.Minute, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
			stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
			logger, nil, &recovery{}, nil)
		if err == nil {
			t.Fatal("the cycle's record was refused and the loop carried on")
		}
		if reason != logging.ReasonJournalUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	// The snapshot on disk and the controller's own are the same state by two
	// routes, and answer under one name.
	t.Run("the state file could not be written", func(t *testing.T) {
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: stopTestSummary()}, refusingStore{},
			stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
			stopTestLogger(t, &output), nil, &recovery{}, nil)
		if err == nil {
			t.Fatal("the state file refused the snapshot and the loop carried on")
		}
		if reason != logging.ReasonControlStateUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("the control state could not be updated", func(t *testing.T) {
		ahead := control.NewSnapshot(control.StateHealthy)
		ahead.Generation = 99
		controller, err := operator.NewController(
			ipc.RoleUser, ipc.ModeObserveOnly,
			[]control.Component{control.ComponentPritunl},
			ahead, control.ReasonNone, nil, func() control.Tick { return 0 })
		if err != nil {
			t.Fatalf("operator.NewController() error: %v", err)
		}
		var output bytes.Buffer
		reason, err := observeLoop(
			context.Background(), time.Minute, true, func() control.Tick { return 0 },
			fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
			controller, &fakeIncidentNotifier{}, nil, nil,
			stopTestLogger(t, &output), nil, &recovery{}, nil)
		if err == nil {
			t.Fatal("the controller refused the update and the loop carried on")
		}
		if reason != logging.ReasonControlStateUnwritable {
			t.Fatalf("reason = %q", reason)
		}
	})

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
				context.Background(), time.Minute, false, func() control.Tick { return 0 },
				fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
				stopTestController(t, 0), &fakeIncidentNotifier{}, nil, done,
				stopTestLogger(t, &output), nil, &recovery{}, nil)
			if err == nil {
				t.Fatal("the socket ended and the loop carried on")
			}
			if reason != logging.ReasonOperatorSocketEnded {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}

// An ending that was asked for names no failure here either.
func TestTheUserLoopNamesNothingWhenNobodyFailed(t *testing.T) {
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
				ctx, time.Minute, once, func() control.Tick { return 0 },
				fixedUserCycler{summary: stopTestSummary()}, stopTestStore(t),
				stopTestController(t, 0), &fakeIncidentNotifier{}, nil, nil,
				stopTestLogger(t, &output), nil, &recovery{}, nil)
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

// The stop is reported through the log the failure cannot have broken, and
// failing to report it does not change the ending. Driven through the real
// command, as the root runtime's is.
func TestTheUserStopIsReportedThroughTheOtherLog(t *testing.T) {
	config := filepath.Join(t.TempDir(), "user-observe.json")
	if err := os.WriteFile(config, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	state := filepath.Join(stateDir, stateFileName)

	var stderr bytes.Buffer
	code := Run([]string{"--observe", "--once", "--config", config, "--state", state},
		&refusingWriter{event: "daemon_started"}, &stderr)
	if code != 1 {
		t.Fatalf("Run() = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"event":"daemon_stopped"`) ||
		!strings.Contains(stderr.String(), `"result":"degraded"`) ||
		!strings.Contains(stderr.String(), `"reason":"journal_unwritable"`) {
		t.Fatalf("the stop was not reported: %q", stderr.String())
	}

	// And with neither log writable it still stops, with the status the failure
	// calls for.
	if code := Run([]string{"--observe", "--once", "--config", config, "--state", state},
		&refusingWriter{event: "daemon_started"},
		&refusingWriter{event: "daemon_stopped"}); code != 1 {
		t.Fatalf("Run() = %d with neither log writable", code)
	}
}
