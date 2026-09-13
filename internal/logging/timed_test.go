package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A component must be able to say how long its own work took.
//
// Nothing in this runtime could, so every question about the daemon's start was
// answered by sampling the process from outside — and three such answers in one
// session were wrong, by factors of two, thirteen and more, because a frame's
// presence in a sample tree was read as its weight.
func TestATimedEventCarriesItsDuration(t *testing.T) {
	out := &bytes.Buffer{}
	logger, err := New(out, ComponentDaemon)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := logger.EmitTimed(LevelInfo, EventStoreOpened, ResultOK,
		StepUserJournal, 1234*time.Millisecond); err != nil {
		t.Fatalf("EmitTimed: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("the record is not JSON: %v", err)
	}
	if record["step"] != string(StepUserJournal) {
		t.Fatalf("step = %v, want %q", record["step"], StepUserJournal)
	}
	if record["duration_ms"] != float64(1234) {
		t.Fatalf("duration_ms = %v, want 1234", record["duration_ms"])
	}
}

// The step vocabulary is closed, like every other field of this record.
//
// These logs are collected and this repository is public. A free-text step name
// is somewhere a path or a hostname arrives by accident, and the secret guard
// cannot tell a step name from a leak.
func TestAStepOutsideTheVocabularyIsRefused(t *testing.T) {
	out := &bytes.Buffer{}
	logger, err := New(out, ComponentDaemon)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := logger.EmitTimed(LevelInfo, EventStoreOpened, ResultOK,
		Step("/Library/Application Support/Hexroute/observe-root"),
		time.Second); err == nil {
		t.Fatal("a step outside the vocabulary was written")
	}
	if out.Len() != 0 {
		t.Fatalf("the refused record was written anyway: %s", out.String())
	}
}

// An event with nothing to time carries no duration at all.
//
// A step that took no measurable time and a step that was not timed are
// different claims, and a zero would say the first when the second is true.
func TestAnUntimedEventCarriesNoDuration(t *testing.T) {
	out := &bytes.Buffer{}
	logger, err := New(out, ComponentDaemon)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := logger.Emit(LevelInfo, EventDaemonStarted, ResultOK, ""); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if strings.Contains(out.String(), "duration_ms") ||
		strings.Contains(out.String(), "\"step\"") {
		t.Fatalf("an untimed event carries timing fields: %s", out.String())
	}
}

// A step faster than a millisecond is reported as one, not as absent.
func TestAStepFasterThanItsUnitStillReportsOne(t *testing.T) {
	out := &bytes.Buffer{}
	logger, err := New(out, ComponentDaemon)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := logger.EmitTimed(LevelInfo, EventStoreOpened, ResultOK,
		StepCheckpoints, 40*time.Microsecond); err != nil {
		t.Fatalf("EmitTimed: %v", err)
	}
	if !strings.Contains(out.String(), `"duration_ms":1`) {
		t.Fatalf("a step faster than a millisecond reported %s", out.String())
	}
}
