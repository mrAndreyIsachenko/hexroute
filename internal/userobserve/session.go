package userobserve

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const (
	statCommand  = "/usr/bin/stat"
	ioregCommand = "/usr/sbin/ioreg"
)

var ErrInvalidObservation = errors.New("invalid user observation")

type SessionState string

const (
	SessionUnknown  SessionState = "unknown"
	SessionActive   SessionState = "active"
	SessionInactive SessionState = "inactive"
)

type SessionObservation struct {
	State      SessionState
	ConsoleUID int
}

type WakeObservation struct {
	Lid  observe.LidState
	Wake observe.WakeKind
}

type MacOSObserver struct {
	runner observe.Runner
}

func NewMacOSObserver(runner observe.Runner) (*MacOSObserver, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}
	return &MacOSObserver{runner: runner}, nil
}

func (observer *MacOSObserver) UserSession(
	ctx context.Context,
	expectedUID int,
) (SessionObservation, error) {
	if expectedUID <= 0 {
		return SessionObservation{}, ErrInvalidObservation
	}
	output, err := observer.runner.Output(ctx, statCommand, "-f", "%u", "/dev/console")
	if err != nil {
		return SessionObservation{}, err
	}
	value := strings.TrimSpace(string(output))
	consoleUID, err := strconv.Atoi(value)
	if err != nil || consoleUID < 0 {
		return SessionObservation{}, ErrInvalidObservation
	}
	state := SessionInactive
	if consoleUID == expectedUID {
		state = SessionActive
	}
	return SessionObservation{
		State:      state,
		ConsoleUID: consoleUID,
	}, nil
}

func (observer *MacOSObserver) Clamshell(ctx context.Context) (WakeObservation, error) {
	lidOutput, err := observer.runner.Output(
		ctx,
		ioregCommand,
		"-r",
		"-k",
		"AppleClamshellState",
		"-d",
		"4",
	)
	if err != nil {
		return WakeObservation{}, err
	}
	wakeOutput, err := observer.runner.Output(
		ctx,
		ioregCommand,
		"-r",
		"-n",
		"IOPMrootDomain",
		"-d",
		"1",
	)
	if err != nil {
		return WakeObservation{}, err
	}
	return WakeObservation{
		Lid:  parseLidState(lidOutput),
		Wake: parseWakeKind(wakeOutput),
	}, nil
}

func parseLidState(output []byte) observe.LidState {
	normalized := strings.ToLower(string(output))
	switch {
	case strings.Contains(normalized, `"appleclamshellstate" = yes`):
		return observe.LidStateClosed
	case strings.Contains(normalized, `"appleclamshellstate" = no`):
		return observe.LidStateOpen
	default:
		return observe.LidStateUnknown
	}
}

func parseWakeKind(output []byte) observe.WakeKind {
	normalized := strings.ToLower(string(output))
	if strings.Contains(normalized, "darkwake") || strings.Contains(normalized, "dark wake") {
		return observe.WakeKindDark
	}
	if strings.Contains(normalized, "wake type") || strings.Contains(normalized, "wake reason") {
		return observe.WakeKindFull
	}
	return observe.WakeKindUnknown
}
