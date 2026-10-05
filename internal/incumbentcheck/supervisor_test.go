package incumbentcheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeProcesses struct {
	pid   int
	at    time.Time
	found bool
	err   error
	asked string
}

func (processes *fakeProcesses) Started(
	_ context.Context, script string,
) (int, time.Time, bool, error) {
	processes.asked = script
	return processes.pid, processes.at, processes.found, processes.err
}

func scriptAt(t *testing.T, written time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twilight-up.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	return path
}

// A supervisor that started before the script installed for it is not running
// what is installed. Measured on this machine: a process eleven days into bytes
// replaced within hours of its start.
func TestASupervisorOlderThanItsScriptIsNotFresh(t *testing.T) {
	written := time.Date(2026, 9, 14, 16, 7, 0, 0, time.UTC)
	script := scriptAt(t, written)
	checker := Checker{
		Script:    script,
		Processes: &fakeProcesses{pid: 95451, at: written.Add(-5 * time.Hour), found: true},
	}
	reading, err := checker.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reading.Fresh() {
		t.Fatalf("a supervisor started before its script read as fresh: %+v", reading)
	}
	if !reading.StartedAt.Equal(written.Add(-5*time.Hour)) ||
		!reading.ScriptWrittenAt.Equal(written) {
		t.Fatalf("reading = %+v; both times must be readable to be reported", reading)
	}
	// The same supervisor restarted after the install is fresh.
	checker.Processes = &fakeProcesses{pid: 95999, at: written.Add(time.Minute), found: true}
	reading, err = checker.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reading.Fresh() {
		t.Fatalf("a supervisor restarted after its install read as stale: %+v", reading)
	}
}

// No supervisor at all is not a fresh one.
func TestNoSupervisorIsNotFresh(t *testing.T) {
	written := time.Now().Add(-time.Hour)
	script := scriptAt(t, written)
	// The time is later than the script's, so only "nobody found it" can make
	// this stale: a reading that compared the times alone would call it fresh.
	checker := Checker{Script: script, Processes: &fakeProcesses{at: time.Now(), found: false}}
	reading, err := checker.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reading.Fresh() {
		t.Fatal("a supervisor nobody found read as fresh")
	}
}

// A supervisor that started at the moment its script was written is not running
// it: the script is written and then the process starts, so equal times mean the
// process is the older of the two by everything that is not the filesystem's
// resolution.
func TestASupervisorAsOldAsItsScriptIsNotFresh(t *testing.T) {
	written := time.Date(2026, 9, 14, 16, 7, 0, 0, time.UTC)
	checker := Checker{
		Script:    scriptAt(t, written),
		Processes: &fakeProcesses{pid: 1, at: written, found: true},
	}
	reading, err := checker.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if reading.Fresh() {
		t.Fatal("a supervisor as old as its script read as fresh")
	}
}

// What the supervisor announced is read rather than assumed: the script's own
// default is not the value it runs with, and a restart that changed it would
// change how often the machine is watched.
func TestTheAnnouncedIntervalIsReadFromWhatItSaid(t *testing.T) {
	script := scriptAt(t, time.Now().Add(-time.Hour))
	log := filepath.Join(t.TempDir(), "supervisor.stdout.log")
	if err := os.WriteFile(log, []byte(
		"healthcheck: enabled every 10s, failures=3\n"+
			"ok: something\n"+
			"healthcheck: enabled every 60s, failures=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checker := Checker{
		Script: script, Log: log,
		Processes: &fakeProcesses{pid: 1, at: time.Now(), found: true},
	}
	reading, err := checker.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The last one, because a restart appends: the running process announced it.
	if reading.Announced != time.Minute {
		t.Fatalf("announced = %s, want the last announcement", reading.Announced)
	}
	// A log that says nothing about it announces nothing rather than zero
	// seconds.
	if err := os.WriteFile(log, []byte("ok: something\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reading, err = checker.Read(context.Background())
	if err != nil || reading.Announced != 0 {
		t.Fatalf("a silent log gave %s (%v)", reading.Announced, err)
	}
	// A log that is not there is not an error: a supervisor that has never
	// written one is a supervisor, and the times still answer.
	checker.Log = filepath.Join(t.TempDir(), "absent.log")
	if _, err := checker.Read(context.Background()); err != nil {
		t.Fatalf("an absent log was an error: %v", err)
	}
	// A log that is there and cannot be read is an error rather than an
	// announcement of nothing: a preflight that reported no announcement for a
	// log it never read would be reporting on a supervisor it never saw.
	unreadable := filepath.Join(t.TempDir(), "supervisor.stdout.log")
	if err := os.Mkdir(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	checker.Log = unreadable
	if _, err := checker.Read(context.Background()); err == nil {
		t.Fatal("a log that could not be read announced nothing without complaint")
	}
}

// A check with nothing to read refuses rather than reporting a fresh supervisor.
func TestACheckWithNothingToReadRefuses(t *testing.T) {
	if _, err := (Checker{}).Read(context.Background()); !errors.Is(err, ErrInvalidCheck) {
		t.Fatal("a checker with no reader answered")
	}
	if _, err := (Checker{Script: "/nowhere/twilight-up.sh", Processes: &fakeProcesses{}}).
		Read(context.Background()); err == nil {
		t.Fatal("a script nobody can stat answered")
	}
}
