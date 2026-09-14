package observe

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

const psCommand = "/bin/ps"

var ErrInvalidProcessObservation = errors.New("invalid process observation")

type Process struct {
	PID        int
	ParentPID  int
	UID        int
	Executable string
	// Args is the whole command line after the executable, as ps reports it.
	Args string
}

type ProcessObservation struct {
	Running bool
	Process Process
}

type ProcessObserver struct {
	runner Runner
}

func NewProcessObserver(runner Runner) (*ProcessObserver, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}
	return &ProcessObserver{runner: runner}, nil
}

// Tunnel reports the sing-box that runs the given configuration, and no other.
//
// The tunnel is identified by what it runs, never by the name of what runs it.
// Other processes run the same executable: the previous owner's ingress probe
// runs `sing-box run -c /tmp/...`, and on 2026-09-14 a lookup that took the
// first process named sing-box stopped the right one only because the tunnel's
// pid was the lower. The previous owner finds its own process the same way,
// by its configuration.
func (observer *ProcessObserver) Tunnel(ctx context.Context, configPath string) (ProcessObservation, error) {
	if !filepath.IsAbs(configPath) {
		return ProcessObservation{}, ErrInvalidProcessObservation
	}
	output, err := observer.runner.Output(ctx, psCommand, "-axo", "pid=,ppid=,uid=,args=")
	if err != nil {
		return ProcessObservation{}, err
	}
	processes, err := parseProcesses(output)
	if err != nil {
		return ProcessObservation{}, err
	}
	for _, process := range processes {
		if filepath.Base(process.Executable) != "sing-box" {
			continue
		}
		if !runsConfiguration(process.Args, configPath) {
			continue
		}
		return ProcessObservation{Running: true, Process: process}, nil
	}
	return ProcessObservation{}, nil
}

// runsConfiguration says whether a command line runs exactly this configuration.
//
// Exactly: a path followed by anything but the end of the line or a space is a
// different file, and `tunnel.json.bak` is not `tunnel.json`.
func runsConfiguration(args, configPath string) bool {
	fields := " " + args + " "
	if !strings.Contains(fields, " run ") {
		return false
	}
	needle := " -c " + configPath
	for from := 0; ; {
		at := strings.Index(fields[from:], needle)
		if at < 0 {
			return false
		}
		after := from + at + len(needle)
		if after >= len(fields) || fields[after] == ' ' {
			return true
		}
		from = after
	}
}

func parseProcesses(output []byte) ([]Process, error) {
	var processes []Process
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			return nil, ErrInvalidProcessObservation
		}
		parentPID, err := strconv.Atoi(fields[1])
		if err != nil || parentPID < 0 {
			return nil, ErrInvalidProcessObservation
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < 0 {
			return nil, ErrInvalidProcessObservation
		}
		command := strings.Join(fields[3:], " ")
		if command == "" || len(command) > 4096 {
			return nil, ErrInvalidProcessObservation
		}
		executable, args, _ := strings.Cut(command, " ")
		processes = append(processes, Process{
			PID:        pid,
			ParentPID:  parentPID,
			UID:        uid,
			Executable: executable,
			Args:       args,
		})
	}
	return processes, nil
}
