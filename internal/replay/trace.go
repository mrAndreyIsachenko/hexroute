package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	TraceSchema = "hexroute.trace-event.v1"
	MaxTrace    = 128 * 1024
)

type Component string

const (
	ComponentRoot    Component = "root"
	ComponentUser    Component = "user"
	ComponentNetwork Component = "network"
	ComponentSingBox Component = "sing_box"
	ComponentRoute   Component = "route"
	ComponentPritunl Component = "pritunl"
)

type Kind string

const (
	KindObservation  Kind = "observation"
	KindTransition   Kind = "transition"
	KindDecision     Kind = "decision"
	KindVerification Kind = "verification"
)

type State string

const (
	StateSuspended  State = "SUSPENDED"
	StateHealthy    State = "HEALTHY"
	StateDegraded   State = "DEGRADED"
	StateRecovering State = "RECOVERING"
	StateSafeMode   State = "SAFE_MODE"
)

type Action string

const (
	ActionNone              Action = "none"
	ActionSkipProbe         Action = "skip_probe"
	ActionApplyScopedRoutes Action = "apply_scoped_routes"
	ActionSelectNextIngress Action = "select_next_ingress"
	ActionRestartSingBox    Action = "restart_sing_box"
	ActionReconnectPritunl  Action = "reconnect_pritunl"
)

type Event struct {
	Schema    string    `json:"schema"`
	Trace     string    `json:"trace"`
	Sequence  uint64    `json:"seq"`
	OffsetMS  uint64    `json:"offset_ms"`
	Component Component `json:"component"`
	Kind      Kind      `json:"kind"`
	State     State     `json:"state"`
	Reason    string    `json:"reason"`
	Action    Action    `json:"action"`
}

type Trace struct {
	Name   string
	Events []Event
}

var ErrInvalidTrace = errors.New("invalid trace fixture")

func Decode(reader io.Reader) (Trace, error) {
	limited := io.LimitReader(reader, MaxTrace+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return Trace{}, err
	}
	if len(content) > MaxTrace {
		return Trace{}, ErrInvalidTrace
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), 16*1024)
	var trace Trace
	var previousOffset uint64
	for scanner.Scan() {
		var event Event
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return Trace{}, ErrInvalidTrace
		}
		if err := validateEvent(event); err != nil {
			return Trace{}, err
		}
		if trace.Name == "" {
			trace.Name = event.Trace
		}
		if event.Trace != trace.Name ||
			event.Sequence != uint64(len(trace.Events)+1) ||
			(len(trace.Events) > 0 && event.OffsetMS < previousOffset) {
			return Trace{}, ErrInvalidTrace
		}
		trace.Events = append(trace.Events, event)
		previousOffset = event.OffsetMS
	}
	if err := scanner.Err(); err != nil {
		return Trace{}, ErrInvalidTrace
	}
	if len(trace.Events) == 0 {
		return Trace{}, ErrInvalidTrace
	}
	return trace, nil
}

func validateEvent(event Event) error {
	if event.Schema != TraceSchema ||
		event.Trace == "" ||
		len(event.Trace) > 80 ||
		event.Sequence == 0 ||
		event.Reason == "" ||
		len(event.Reason) > 80 ||
		!validComponent(event.Component) ||
		!validKind(event.Kind) ||
		!validState(event.State) ||
		!validAction(event.Action) {
		return fmt.Errorf("%w: invalid event", ErrInvalidTrace)
	}
	for _, character := range event.Trace + event.Reason {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '-' &&
			character != '_' {
			return ErrInvalidTrace
		}
	}
	return nil
}

func validComponent(value Component) bool {
	switch value {
	case ComponentRoot, ComponentUser, ComponentNetwork, ComponentSingBox, ComponentRoute, ComponentPritunl:
		return true
	default:
		return false
	}
}

func validKind(value Kind) bool {
	switch value {
	case KindObservation, KindTransition, KindDecision, KindVerification:
		return true
	default:
		return false
	}
}

func validState(value State) bool {
	switch value {
	case StateSuspended, StateHealthy, StateDegraded, StateRecovering, StateSafeMode:
		return true
	default:
		return false
	}
}

func validAction(value Action) bool {
	switch value {
	case ActionNone, ActionSkipProbe, ActionApplyScopedRoutes, ActionSelectNextIngress,
		ActionRestartSingBox, ActionReconnectPritunl:
		return true
	default:
		return false
	}
}
