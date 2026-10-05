package incumbentcheck

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner runs a command and returns what it printed. It exists so the reading
// below can be driven without a process table.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands.
type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// PSProcesses finds the supervisor in the process table and reads when it
// started.
//
// It asks for the start time rather than for an elapsed time: elapsed is a
// duration from now, and a preflight comparing it with a file's timestamp would
// be comparing two different clocks' opinions of the same moment.
//
// Several processes carry that script in their command line, not one. A shell
// subshell inherits its parent's arguments, and that supervisor forks one for
// every command substitution it makes — so the table holds the supervisor, a
// long-lived child of it, and whatever momentary subshell existed when the
// listing was taken. Measured 2026-09-25: two consecutive preflights read
// different processes, one of them a subshell three seconds old, and reported
// opposite answers about the same machine.
//
// The supervisor is the one whose parent is not itself running that script.
type PSProcesses struct {
	Runner Runner
	// Location is how the process table's local times are read. Empty means the
	// machine's own, which is what `ps` prints.
	Location *time.Location
}

const psCommand = "/bin/ps"

// psLayout is what `ps -o lstart=` prints: "Mon Sep 14 11:14:02 2026".
const psLayout = "Mon Jan _2 15:04:05 2006"

func (processes PSProcesses) Started(
	ctx context.Context,
	script string,
) (int, time.Time, bool, error) {
	if processes.Runner == nil || script == "" {
		return 0, time.Time{}, false, ErrInvalidCheck
	}
	output, err := processes.Runner.Output(ctx, psCommand, "-axo", "pid=,ppid=,lstart=,args=")
	if err != nil {
		return 0, time.Time{}, false, err
	}
	location := processes.Location
	if location == nil {
		location = time.Local
	}
	type candidate struct {
		pid, parent int
		startedAt   time.Time
	}
	var candidates []candidate
	running := make(map[int]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// The arguments are what identify it: a supervisor is a process running
		// that script, whatever started it and whatever shell it wears.
		if !strings.Contains(line, script) {
			continue
		}
		fields := strings.Fields(line)
		// pid, ppid, then five fields of lstart, then the command.
		if len(fields) < 8 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		startedAt, err := time.ParseInLocation(psLayout, strings.Join(fields[2:7], " "), location)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{pid: pid, parent: parent, startedAt: startedAt})
		running[pid] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return 0, time.Time{}, false, err
	}
	// The supervisor is the one nothing else running that script fathered. Its
	// children are subshells wearing its arguments, and taking one of those
	// would report a process seconds old as the runtime that has held the
	// tunnel for days.
	chosen := candidate{}
	for _, item := range candidates {
		if _, fathered := running[item.parent]; fathered {
			continue
		}
		if chosen.pid == 0 || item.startedAt.Before(chosen.startedAt) {
			chosen = item
		}
	}
	if chosen.pid == 0 {
		return 0, time.Time{}, false, nil
	}
	return chosen.pid, chosen.startedAt, true, nil
}
