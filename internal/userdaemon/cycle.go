package userdaemon

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type SessionObserver interface {
	UserSession(context.Context, int) (userobserve.SessionObservation, error)
	Clamshell(context.Context) (userobserve.WakeObservation, error)
}

type PritunlObserver interface {
	Profile(context.Context, string) (userobserve.ProfileObservation, error)
	Service(context.Context) (userobserve.ServiceObservation, error)
	ClientAddress(
		context.Context,
		userobserve.ProfileObservation,
	) (userobserve.ClientAddressObservation, error)
}

type EndpointObserver interface {
	Endpoint(context.Context, observe.Endpoint) (observe.ReadinessObservation, error)
}

type ReconnectPlanner interface {
	Plan(pritunlplan.Observation) (pritunlplan.Plan, error)
	Snapshot() control.Snapshot
}

type Summary struct {
	Failures             uint32
	OuterReady           bool
	ServiceRunning       bool
	ClientAddressPresent bool
	Plan                 pritunlplan.Plan
}

type Cycle struct {
	config    RuntimeConfig
	session   SessionObserver
	pritunl   PritunlObserver
	readiness EndpointObserver
	planner   ReconnectPlanner
}

func NewCycle(
	config RuntimeConfig,
	session SessionObserver,
	pritunl PritunlObserver,
	readiness EndpointObserver,
	planner ReconnectPlanner,
) (*Cycle, error) {
	if session == nil || pritunl == nil || readiness == nil || planner == nil {
		return nil, ErrInvalidConfig
	}
	return &Cycle{
		config:    config,
		session:   session,
		pritunl:   pritunl,
		readiness: readiness,
		planner:   planner,
	}, nil
}

func (cycle *Cycle) Observe(
	ctx context.Context,
	at control.Tick,
	unixSeconds int64,
) Summary {
	summary := cycle.failedSummary()

	session, err := cycle.session.UserSession(ctx, cycle.config.ExpectedUID)
	if err != nil {
		summary.Failures++
		return summary
	}
	wake, err := cycle.session.Clamshell(ctx)
	if err != nil {
		summary.Failures++
		return summary
	}
	if session.State != userobserve.SessionActive ||
		wake.Lid != observe.LidStateOpen ||
		wake.Wake != observe.WakeKindFull {
		return cycle.plan(summary, pritunlplan.Observation{
			At:            at,
			Session:       session.State,
			Wake:          wake,
			OptionalInner: pritunlplan.OptionalInnerUnknown,
		})
	}

	profile, err := cycle.pritunl.Profile(ctx, cycle.config.ProfileID)
	if err != nil {
		summary.Failures++
		return summary
	}
	service, err := cycle.pritunl.Service(ctx)
	if err != nil {
		summary.Failures++
	} else {
		summary.ServiceRunning = service.Running
		if !service.Running {
			summary.Failures++
		}
	}
	optionalInner := pritunlplan.OptionalInnerUnknown
	if profile.HasClientAddress {
		client, clientErr := cycle.pritunl.ClientAddress(ctx, profile)
		if clientErr == nil {
			summary.ClientAddressPresent = client.Present
			if client.Present {
				optionalInner = pritunlplan.OptionalInnerReady
			} else {
				optionalInner = pritunlplan.OptionalInnerFailed
			}
		}
	}
	outer, err := cycle.readiness.Endpoint(ctx, cycle.config.OuterEndpoint)
	if err != nil {
		summary.Failures++
	} else {
		summary.OuterReady = outer.Ready
	}
	otpRemaining, err := pritunlplan.OTPSecondsRemaining(
		unixSeconds,
		cycle.config.Policy.OTPPeriod,
	)
	if err != nil {
		summary.Failures++
		return summary
	}
	return cycle.plan(summary, pritunlplan.Observation{
		At:                  at,
		Session:             session.State,
		Wake:                wake,
		OuterReady:          summary.OuterReady,
		Profile:             profile,
		OptionalInner:       optionalInner,
		OTPSecondsRemaining: otpRemaining,
	})
}

func (cycle *Cycle) plan(
	summary Summary,
	observation pritunlplan.Observation,
) Summary {
	plan, err := cycle.planner.Plan(observation)
	if err != nil {
		summary.Failures++
		return summary
	}
	summary.Plan = plan
	return summary
}

func (cycle *Cycle) failedSummary() Summary {
	snapshot := cycle.planner.Snapshot()
	return Summary{
		Plan: pritunlplan.Plan{
			ObserveOnly: true,
			State:       snapshot.State,
			Action:      pritunlplan.ActionNone,
			Reason:      pritunlplan.ReasonNone,
			Snapshot:    snapshot,
		},
	}
}
