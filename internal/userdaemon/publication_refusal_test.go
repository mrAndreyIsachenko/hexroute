package userdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

// A refused publication must not fail the cycle and must not pass unrecorded.
//
// Root answers most refusals with an error code. That is a well formed
// response, so root does not report it, and the publisher used to drop it and
// return nil — so a day of refused publications left nothing in either log
// while the stream stood still and the daemon went on reporting healthy. The
// only evidence was a file that had stopped changing, which is evidence only
// to somebody already looking for it.
func TestARefusedPublicationSaysWhoRefusedItAndWhy(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		response ipc.Response
		err      error
		reason   string
	}{
		{
			name:     "root refuses the publication itself",
			response: ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorInvalidRequest},
			reason:   "malformed_request",
		},
		{
			name:     "root cannot fold it",
			response: ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorPrecondition},
			reason:   "read_model_unavailable",
		},
		{
			name:     "root answers with its own failure",
			response: ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorInternal},
			reason:   "root_internal",
		},
		{
			name:   "root never answers",
			err:    os.ErrDeadlineExceeded,
			reason: "publication_timeout",
		},
		{
			name:   "the round trip never completes",
			err:    errors.New("dial unix: connection refused"),
			reason: "socket_unavailable",
		},
		{
			name:   "root is not listening",
			err:    fmt.Errorf("dial unix: %w", fs.ErrNotExist),
			reason: "socket_absent",
		},
		{
			name:   "the socket refuses this user",
			err:    fmt.Errorf("dial unix: %w", fs.ErrPermission),
			reason: "socket_denied",
		},
		// The client validates the request before it dials, so a publication
		// this daemon built wrong comes back through the same error as an
		// unreachable peer. Naming it as a transport failure is what sends the
		// next reader to root's log to look for a request that never arrived.
		{
			name:   "the publication never leaves the process",
			err:    ipc.ErrInvalidConnectivityMessage,
			reason: "invalid_connectivity_message",
		},
		{
			name:   "the publication names a domain root will not take",
			err:    ipc.ErrConnectivityDomain,
			reason: "connectivity_domain_refused",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			log := &bytes.Buffer{}
			logger, err := logging.New(log, logging.ComponentUser)
			if err != nil {
				t.Fatalf("logger: %v", err)
			}
			publisher := refusingPublisher(t, func() (ipc.Response, error) {
				return testCase.response, testCase.err
			})

			// The cycle survives the refusal. That part was always right.
			if err := publisher.Publish(context.Background(), observedEvidence(), logger); err != nil {
				t.Fatalf("a refusal failed the cycle: %v", err)
			}

			event := soleEvent(t, log)
			if event.Event != "connectivity_publication" || event.Result != "rejected" {
				t.Fatalf("a refusal was not recorded as one: %+v", event)
			}
			if event.Reason != testCase.reason {
				t.Fatalf("refusal named %q, want %q", event.Reason, testCase.reason)
			}
		})
	}
}

// A refusal that continues must stay findable without burying the log that
// makes it findable. The qualification agent writing one line every five
// seconds reached eight megabytes of a single repeated sentence; a publisher
// refused every cycle would do the same.
func TestAContinuingRefusalIsStatedPeriodicallyNotEveryCycle(t *testing.T) {
	log := &bytes.Buffer{}
	logger, err := logging.New(log, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	publisher := refusingPublisher(t, func() (ipc.Response, error) {
		return ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorInternal}, nil
	})

	const cycles = refusalReportEvery * 2
	for range cycles {
		if err := publisher.Publish(context.Background(), observedEvidence(), logger); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	// The opening refusal, then one per period. Not one per cycle.
	if lines := countEvents(log, "connectivity_publication"); lines != 3 {
		t.Fatalf("a refusal held for %d cycles wrote %d lines, want 3", cycles, lines)
	}
}

// An outage that ended must say so. A log that records only the beginning
// leaves a reader unable to tell a failure that is still running from one that
// recovered while nobody was watching.
func TestRecoveryClosesTheRunOfRefusals(t *testing.T) {
	log := &bytes.Buffer{}
	logger, err := logging.New(log, logging.ComponentUser)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	refused := true
	publisher := refusingPublisher(t, func() (ipc.Response, error) {
		if refused {
			return ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorInternal}, nil
		}
		return ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorNone}, nil
	})

	for range 3 {
		if err := publisher.Publish(context.Background(), observedEvidence(), logger); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	refused = false
	if err := publisher.Publish(context.Background(), observedEvidence(), logger); err != nil {
		t.Fatalf("publish: %v", err)
	}

	events := decodeEvents(t, log)
	if len(events) != 2 {
		t.Fatalf("want the refusal and its end, got %d events", len(events))
	}
	if events[0].Result != "rejected" || events[1].Result != "ok" {
		t.Fatalf("the run was not closed: %+v", events)
	}

	// A publication accepted while nothing was wrong stays silent, or the
	// steady state writes a line every cycle for ever.
	before := log.Len()
	if err := publisher.Publish(context.Background(), observedEvidence(), logger); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if log.Len() != before {
		t.Fatal("a healthy publication wrote a line")
	}
}

func refusingPublisher(
	t *testing.T,
	answer func() (ipc.Response, error),
) *factPublisher {
	t.Helper()
	publisher, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock",
		filepath.Join(t.TempDir(), "connectivity-stream.json"), 15*time.Second)
	if err != nil || publisher == nil {
		t.Fatalf("publisher: %v", err)
	}
	publisher.roundTrip = func(context.Context, string, ipc.Request) (ipc.Response, error) {
		return answer()
	}
	return publisher
}

type loggedEvent struct {
	Event  string `json:"event"`
	Result string `json:"result"`
	Reason string `json:"reason"`
}

func decodeEvents(t *testing.T, log *bytes.Buffer) []loggedEvent {
	t.Helper()
	events := []loggedEvent{}
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if line == "" {
			continue
		}
		var event loggedEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func soleEvent(t *testing.T, log *bytes.Buffer) loggedEvent {
	t.Helper()
	events := decodeEvents(t, log)
	if len(events) != 1 {
		t.Fatalf("want one event, got %d: %+v", len(events), events)
	}
	return events[0]
}

func countEvents(log *bytes.Buffer, name string) int {
	count := 0
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, `"event":"`+name+`"`) {
			count++
		}
	}
	return count
}
