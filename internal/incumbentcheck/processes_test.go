package incumbentcheck

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedRunner struct {
	output []byte
	err    error
	args   []string
}

func (runner *fixedRunner) Output(
	_ context.Context, _ string, args ...string,
) ([]byte, error) {
	runner.args = args
	return runner.output, runner.err
}

const script = "/Library/Application Support/twilight/supervisor/scripts/twilight-up.sh"

// The supervisor is found by the script it runs, and its start time is read as a
// moment rather than as an age.
//
// An elapsed time is a duration from now, and a preflight comparing it with a
// file's timestamp would be comparing two clocks' opinions of one moment.
func TestTheSupervisorIsFoundByItsScriptWithItsStartTime(t *testing.T) {
	runner := &fixedRunner{output: []byte(
		"  501     1 Fri Sep 25 18:24:31 2026 /bin/zsh -c something-else\n" +
			"95451     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n" +
			"95459 95451 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n")}
	processes := PSProcesses{Runner: runner, Location: time.UTC}
	pid, at, found, err := processes.Started(context.Background(), script)
	if err != nil || !found {
		t.Fatalf("Started: %v, found=%v", err, found)
	}
	if pid != 95451 {
		t.Fatalf("pid = %d", pid)
	}
	want := time.Date(2026, 9, 14, 11, 14, 2, 0, time.UTC)
	if !at.Equal(want) {
		t.Fatalf("started at %s, want %s", at, want)
	}
	// It asks for the start time, not for an elapsed one.
	joined := ""
	for _, argument := range runner.args {
		joined += argument + " "
	}
	if want := "lstart"; !contains(joined, want) {
		t.Fatalf("it asked %q, without %q", joined, want)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for index := 0; index+len(needle) <= len(haystack); index++ {
			if haystack[index:index+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// A process table without it says so, rather than reporting a process that is
// not there.
func TestASupervisorThatIsNotRunningIsNotFound(t *testing.T) {
	processes := PSProcesses{
		Runner:   &fixedRunner{output: []byte("  501     1 Fri Sep 25 18:24:31 2026 /bin/zsh -c something\n")},
		Location: time.UTC,
	}
	_, _, found, err := processes.Started(context.Background(), script)
	if err != nil || found {
		t.Fatalf("found=%v, err=%v", found, err)
	}
}

// A line that cannot be read is skipped rather than taken for the supervisor.
func TestALineThatCannotBeReadIsNotTheSupervisor(t *testing.T) {
	processes := PSProcesses{
		Runner: &fixedRunner{output: []byte(
			"not-a-pid     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n" +
				"95450 not-a-ppid Mon Sep 14 11:13:02 2026 /bin/bash " + script + "\n" +
				"95451     1 nonsense here at all too /bin/bash " + script + "\n" +
				"95452     1 Mon Sep 14 11:15:02 2026 /bin/bash " + script + "\n")},
		Location: time.UTC,
	}
	pid, _, found, err := processes.Started(context.Background(), script)
	if err != nil || !found || pid != 95452 {
		t.Fatalf("pid=%d found=%v err=%v", pid, found, err)
	}
}

// A process table that cannot be read is an error, not an empty answer: a
// preflight that took a failed listing for "no supervisor" would pass.
func TestAProcessTableThatCannotBeReadIsAnError(t *testing.T) {
	processes := PSProcesses{Runner: &fixedRunner{err: errors.New("no")}, Location: time.UTC}
	if _, _, _, err := processes.Started(context.Background(), script); err == nil {
		t.Fatal("a failed listing answered")
	}
	if _, _, _, err := (PSProcesses{}).Started(context.Background(), script); !errors.Is(err, ErrInvalidCheck) {
		t.Fatalf("a reader with no runner gave %v", err)
	}
}

// The supervisor is the process nothing else running that script fathered.
//
// A shell subshell inherits its parent's arguments, and that supervisor forks
// one for every command substitution it makes, so the table holds the
// supervisor, a long-lived child, and whatever momentary subshell existed when
// the listing was taken. Measured 2026-09-25: two consecutive preflights read
// different processes — one a subshell three seconds old — and reported opposite
// answers about the same machine.
func TestASubshellWearingItsParentsArgumentsIsNotTheSupervisor(t *testing.T) {
	processes := PSProcesses{
		Runner: &fixedRunner{output: []byte(
			// The momentary subshell comes first, as it did on the machine.
			"38055 95451 Fri Sep 25 23:02:33 2026 /bin/bash " + script + "\n" +
				"95451     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n" +
				"95459 95451 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n")},
		Location: time.UTC,
	}
	pid, at, found, err := processes.Started(context.Background(), script)
	if err != nil || !found {
		t.Fatalf("Started: %v, found=%v", err, found)
	}
	if pid != 95451 {
		t.Fatalf("pid = %d, want the one launchd fathered", pid)
	}
	want := time.Date(2026, 9, 14, 11, 14, 2, 0, time.UTC)
	if !at.Equal(want) {
		t.Fatalf("started at %s, want %s", at, want)
	}
	// The order in the table does not decide it.
	processes.Runner = &fixedRunner{output: []byte(
		"95451     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n" +
			"38055 95451 Fri Sep 25 23:02:33 2026 /bin/bash " + script + "\n")}
	if pid, _, _, err := processes.Started(context.Background(), script); err != nil || pid != 95451 {
		t.Fatalf("the other order gave pid %d (%v)", pid, err)
	}

	// Nor does it where a child started in the same second as its parent and is
	// listed first, which is what the machine shows: the supervisor and the
	// subshell it forked at start carry the same moment.
	processes.Runner = &fixedRunner{output: []byte(
		"95459 95451 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n" +
			"95451     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n")}
	if pid, _, _, err := processes.Started(context.Background(), script); err != nil || pid != 95451 {
		t.Fatalf("a child listed first gave pid %d (%v)", pid, err)
	}
}

// Where several are fathered by nothing running that script, the oldest is the
// supervisor: a restart leaves the new one, and a listing caught mid-restart
// must not report the newcomer as the runtime that has held the tunnel.
func TestTheOldestUnfatheredProcessIsTheSupervisor(t *testing.T) {
	processes := PSProcesses{
		Runner: &fixedRunner{output: []byte(
			"40000     1 Fri Sep 25 23:02:33 2026 /bin/bash " + script + "\n" +
				"95451     1 Mon Sep 14 11:14:02 2026 /bin/bash " + script + "\n")},
		Location: time.UTC,
	}
	pid, _, _, err := processes.Started(context.Background(), script)
	if err != nil || pid != 95451 {
		t.Fatalf("pid = %d (%v)", pid, err)
	}
}
