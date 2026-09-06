package ingressagent

import (
	"context"
	"fmt"
	"time"
)

const (
	// OutcomeProven is the applied version reporting itself healthy through
	// the signed heartbeat.
	OutcomeProven Outcome = "proven"
	// OutcomePending is an applied version still inside its window.
	OutcomePending Outcome = "pending"
)

// Observation is what the host's own signed heartbeat said.
//
// Generation is the version the host reports itself to be running, which the
// agent wrote when it applied one. Healthy is the observer's own verdict on
// the transport, reached by using it.
type Observation struct {
	Generation string
	Healthy    bool
}

// Observer reads the host's signed heartbeat and verifies it.
//
// Reading it unverified would make proof depend on whatever answered the
// loopback port, which is the same mistake as trusting a delivery path.
type Observer interface {
	Observe(ctx context.Context) (Observation, error)
}

// ProveOrReturn decides whether the applied version has proved itself.
//
// Proof is the heartbeat reporting that exact generation healthy. Having
// applied cleanly is not proof, and being reachable is not proof: both were
// true of every version that ever broke a host, right up until it was broken.
//
// A version that has not proved by the end of its window is returned from,
// with the reason it did not.
func (agent *Agent) ProveOrReturn(
	ctx context.Context,
	observer Observer,
	window time.Duration,
) (Result, error) {
	if agent == nil || ctx == nil || observer == nil || window <= 0 {
		return Result{}, fmt.Errorf("%w: nothing to prove", ErrAgent)
	}
	applied, _, ok, err := agent.store.Applied()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("%w: nothing applied", ErrAgent)
	}
	appliedAt, ok, err := agent.store.AppliedAt()
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("%w: no application time", ErrAgent)
	}

	label := applied.Statement.VersionLabel
	digest := applied.Statement.ContentSHA256
	observation, observeErr := observer.Observe(ctx)
	reason := unprovenReason(observation, label, observeErr)
	if reason == "" {
		return agent.record(Result{
			Outcome:       OutcomeProven,
			VersionLabel:  label,
			ContentSHA256: digest,
		})
	}
	if agent.now().UTC().Sub(appliedAt) < window {
		return agent.record(Result{
			Outcome:       OutcomePending,
			VersionLabel:  label,
			ContentSHA256: digest,
			Reason:        reason,
		})
	}

	// The window has passed and this version has never said it works. What
	// the host runs now is something nobody has evidence for.
	if _, _, retained, err := agent.store.Retained(); err != nil {
		return Result{}, err
	} else if !retained {
		// There is nothing to return to. Saying so is better than pretending
		// the version proved, and better than leaving no record at all.
		return agent.record(Result{
			Outcome:      OutcomeRefused,
			VersionLabel: label,
			Reason:       reason,
		})
	}
	result, err := agent.Return(ctx)
	if err != nil {
		return Result{}, err
	}
	result.Reason = reason
	return agent.record(result)
}

// unprovenReason names why a version is not proven, or is empty when it is.
func unprovenReason(observation Observation, label string, err error) string {
	switch {
	case err != nil:
		return "heartbeat_unavailable"
	case observation.Generation != label:
		return "generation_not_running"
	case !observation.Healthy:
		return "transport_unhealthy"
	default:
		return ""
	}
}
