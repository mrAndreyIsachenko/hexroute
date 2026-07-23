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
}

type ProcessObservation struct {
	Running    bool
	Process    Process
	OwnedChild bool
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

func (observer *ProcessObserver) SingBox(ctx context.Context, expectedParentPID int) (ProcessObservation, error) {
	output, err := observer.runner.Output(ctx, psCommand, "-axo", "pid=,ppid=,uid=,comm=")
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
		return ProcessObservation{
			Running:    true,
			Process:    process,
			OwnedChild: expectedParentPID > 0 && process.ParentPID == expectedParentPID,
		}, nil
	}
	return ProcessObservation{}, nil
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
		executable := strings.Join(fields[3:], " ")
		if executable == "" || len(executable) > 1024 {
			return nil, ErrInvalidProcessObservation
		}
		processes = append(processes, Process{
			PID:        pid,
			ParentPID:  parentPID,
			UID:        uid,
			Executable: executable,
		})
	}
	return processes, nil
}
