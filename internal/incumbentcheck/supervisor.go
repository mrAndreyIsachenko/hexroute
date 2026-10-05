// Package incumbentcheck reads whether the runtime holding the tunnel is
// running what is installed for it.
//
// It exists because of what the first handover found. That runtime's supervisor
// restarts its tunnel inside its own process, so it goes on running the script
// it loaded at start however many times a newer one is installed: measured
// 2026-09-25, a supervisor eleven days into running bytes that had been replaced
// within hours of its start. Every claim a preflight makes about how it will
// behave under a claim is a claim about a file it is not executing.
//
// Nothing here changes that runtime. It reads when its process started and when
// its script was last written, from outside, and says whether the first is after
// the second.
package incumbentcheck

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"time"
)

const (
	// DefaultScript is the supervisor the previous owner runs.
	DefaultScript = "/Library/Application Support/twilight/supervisor/scripts/twilight-up.sh"
	// DefaultLog is where that supervisor says what it is running under. The
	// settings it runs with come from a file it reads at start rather than from
	// its launch definition, so what it announces is the only reading of them
	// from outside.
	DefaultLog = "/Library/Logs/twilight/supervisor.stdout.log"
)

// ProcessReader finds the supervisor and says when it started.
type ProcessReader interface {
	// Started reports the process running the script and when it started. It
	// reports found false where none is.
	Started(ctx context.Context, script string) (pid int, at time.Time, found bool, err error)
}

// Reading is what can be established about the supervisor from outside it.
type Reading struct {
	PID             int
	StartedAt       time.Time
	ScriptWrittenAt time.Time
	Found           bool
	// Announced is the health interval it said it was running with, or zero
	// where the log says nothing. It is read rather than assumed because the
	// script's own default is not the value it runs with: measured 2026-09-25,
	// ten seconds in the script and sixty in what it announced.
	Announced time.Duration
}

// Fresh is whether the running supervisor started after the script installed for
// it was written.
//
// The weakness is written down rather than hidden: an install that preserved the
// script's timestamp would satisfy this while replacing the bytes. What it
// catches is the failure that happened — a supervisor outliving its script —
// and it needs nothing from the runtime being taken from.
func (reading Reading) Fresh() bool {
	return reading.Found && reading.StartedAt.After(reading.ScriptWrittenAt)
}

var ErrInvalidCheck = errors.New("invalid incumbent check")

// Checker reads the supervisor.
type Checker struct {
	Script    string
	Log       string
	Processes ProcessReader
}

// Read establishes what can be established without touching that runtime.
func (checker Checker) Read(ctx context.Context) (Reading, error) {
	if checker.Processes == nil || checker.Script == "" {
		return Reading{}, ErrInvalidCheck
	}
	pid, startedAt, found, err := checker.Processes.Started(ctx, checker.Script)
	if err != nil {
		return Reading{}, err
	}
	reading := Reading{PID: pid, StartedAt: startedAt.UTC(), Found: found}
	info, err := os.Stat(checker.Script)
	if err != nil {
		return Reading{}, err
	}
	reading.ScriptWrittenAt = info.ModTime().UTC()
	if checker.Log != "" {
		announced, err := announcedInterval(checker.Log)
		if err != nil {
			return Reading{}, err
		}
		reading.Announced = announced
	}
	return reading, nil
}

// announcedHealth is the line the supervisor prints at start about how often it
// looks. The last one is the running process's, because a restart appends.
var announcedHealth = regexp.MustCompile(`healthcheck: enabled every ([0-9]+)s`)

// announcedInterval reads the last interval the supervisor announced, or zero
// where it announced none.
//
// A log that cannot be read is not an announcement of zero: it is an error, and
// a preflight that took it for a value would pass a supervisor it never read.
func announcedInterval(path string) (time.Duration, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	matches := announcedHealth.FindAllSubmatch(content, -1)
	if len(matches) == 0 {
		return 0, nil
	}
	seconds, err := strconv.Atoi(string(matches[len(matches)-1][1]))
	if err != nil || seconds <= 0 {
		return 0, nil
	}
	return time.Duration(seconds) * time.Second, nil
}
