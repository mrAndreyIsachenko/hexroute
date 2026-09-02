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
	// Plan is what an authorized sentinel would have done with this decision.
	// PlanKnown is false when the planner refused the input, which is a
	// different answer from a plan that selected no action.
	Plan      RecoveryPlan
	PlanKnown bool
	// PlanRefused carries why the planner would not answer, so a sentinel
	// that stopped planning says so rather than reading as one with nothing
	// to plan.
	PlanRefused error
}

type Cycle struct {
	config    RuntimeConfig
	heartbeat HeartbeatReader
	readiness EndpointObserver
	tracker   DecisionTracker
	// recovery decides what an authorized sentinel would do. It is built
	// without a restarter, so it holds no means of acting — "it does not act"
	// is a property of the object rather than of a branch someone could
	// change for a reason that looked good.
	recovery *RecoveryController
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
	planner, err := NewRecoveryPlanner(ObservingRecoveryPolicy)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	// Nil restarter, deliberately. The observing sentinel is not a sentinel
	// that declines to act; it is one that cannot.
	recovery, err := NewRecoveryController(RecoveryObserveOnly, planner, nil)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return &Cycle{
		config:    config,
		heartbeat: heartbeatReader,
		readiness: readiness,
		tracker:   tracker,
		recovery:  recovery,
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

	// The planner runs even when the decision selects nothing, because the
	// phase it is in is the answer an operator wants and a decision with no
	// action still moves it.
	result, planErr := cycle.recovery.Handle(ctx, at, decision)
	if planErr != nil {
		// A planner that refused an input must not take the watching with it.
		// The sentinel's job is to keep looking.
		summary.PlanRefused = planErr
		return summary
	}
	summary.Plan = result.Plan
	summary.PlanKnown = true
	return summary
}
