package silentnode

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type State string

const (
	StateHealthy  State = "healthy"
	StateSleeping State = "sleeping"
	StateSilent   State = "silent"
	StateIgnored  State = "ignored"
)

type Policy struct {
	MissedHeartbeats uint32
	MinimumGrace     time.Duration
	FutureTolerance  time.Duration
}

type Node struct {
	NodeID                    metadata.UUID
	NodeKind                  string
	LifecycleStatus           string
	ExpectedHeartbeatInterval time.Duration
	CreatedAt                 time.Time
	LastSeenAt                time.Time
	SleepingAtEvaluation      bool
}

type Decision struct {
	NodeID      metadata.UUID
	State       State
	Reference   time.Time
	Deadline    time.Time
	EvaluatedAt time.Time
}

var (
	ErrInvalidPolicy = errors.New("invalid silent-node policy")
	ErrInvalidNode   = errors.New("invalid silent-node input")
)

func (policy Policy) Validate() error {
	if policy.MissedHeartbeats == 0 ||
		policy.MissedHeartbeats > 100 ||
		policy.MinimumGrace < 0 ||
		policy.MinimumGrace > 24*time.Hour ||
		policy.FutureTolerance < 0 ||
		policy.FutureTolerance > time.Hour {
		return ErrInvalidPolicy
	}
	return nil
}

func Evaluate(node Node, policy Policy, at time.Time) (Decision, error) {
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	if _, err := metadata.ParseUUID(string(node.NodeID)); err != nil ||
		(node.NodeKind != "mac" && node.NodeKind != "vps" && node.NodeKind != "cloud") ||
		(node.LifecycleStatus != "active" &&
			node.LifecycleStatus != "retired" &&
			node.LifecycleStatus != "revoked") ||
		node.ExpectedHeartbeatInterval <= 0 ||
		node.ExpectedHeartbeatInterval > 24*time.Hour ||
		node.CreatedAt.IsZero() ||
		at.IsZero() {
		return Decision{}, ErrInvalidNode
	}
	at = at.UTC()
	reference := node.LastSeenAt.UTC()
	if node.LastSeenAt.IsZero() {
		reference = node.CreatedAt.UTC()
	}
	if reference.After(at.Add(policy.FutureTolerance)) {
		return Decision{}, ErrInvalidNode
	}
	grace := node.ExpectedHeartbeatInterval * time.Duration(policy.MissedHeartbeats)
	if grace < policy.MinimumGrace {
		grace = policy.MinimumGrace
	}
	decision := Decision{
		NodeID:      node.NodeID,
		Reference:   reference,
		Deadline:    reference.Add(grace),
		EvaluatedAt: at,
	}
	if node.LifecycleStatus != "active" {
		decision.State = StateIgnored
		return decision, nil
	}
	if node.NodeKind == "mac" && node.SleepingAtEvaluation {
		decision.State = StateSleeping
		return decision, nil
	}
	if at.After(decision.Deadline) {
		decision.State = StateSilent
		return decision, nil
	}
	decision.State = StateHealthy
	return decision, nil
}
