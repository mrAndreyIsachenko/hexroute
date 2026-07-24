package sentinel

import (
	"context"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type HeartbeatReader interface {
	Load(string) (heartbeat.Record, error)
}

type EndpointObserver interface {
	Endpoint(context.Context, observe.Endpoint) (observe.ReadinessObservation, error)
}

type DecisionTracker interface {
	Evaluate(control.Tick, HeartbeatObservation, bool) (Decision, error)
}

type FileHeartbeatReader struct{}

func (FileHeartbeatReader) Load(path string) (heartbeat.Record, error) {
	return heartbeat.Load(path)
}

type Summary struct {
	Failures       uint32
	HeartbeatFound bool
	DataPathReady  bool
	Decision       Decision
}

type Cycle struct {
	config    RuntimeConfig
	heartbeat HeartbeatReader
	readiness EndpointObserver
	tracker   DecisionTracker
}

func NewCycle(
	config RuntimeConfig,
	heartbeatReader HeartbeatReader,
	readiness EndpointObserver,
	tracker DecisionTracker,
) (*Cycle, error) {
	if heartbeatReader == nil || readiness == nil || tracker == nil {
		return nil, ErrInvalidConfig
	}
	return &Cycle{
		config:    config,
		heartbeat: heartbeatReader,
		readiness: readiness,
		tracker:   tracker,
	}, nil
}

func (cycle *Cycle) Observe(ctx context.Context, at control.Tick) Summary {
	summary := Summary{
		Decision: Decision{
			ObserveOnly: true,
			Action:      ActionNone,
		},
	}
	record, err := cycle.heartbeat.Load(cycle.config.HeartbeatPath)
	heartbeatObservation := HeartbeatObservation{}
	switch {
	case err == nil:
		summary.HeartbeatFound = true
		heartbeatObservation = HeartbeatObservation{
			Present:  true,
			Sequence: record.Sequence,
		}
	case errors.Is(err, heartbeat.ErrHeartbeatMissing):
	default:
		summary.Failures++
		return summary
	}

	probe, err := cycle.readiness.Endpoint(ctx, cycle.config.DataPathEndpoint)
	if err != nil {
		summary.Failures++
		return summary
	}
	summary.DataPathReady = probe.Ready
	decision, err := cycle.tracker.Evaluate(
		at,
		heartbeatObservation,
		summary.DataPathReady,
	)
	if err != nil {
		summary.Failures++
		return summary
	}
	summary.Decision = decision
	return summary
}
