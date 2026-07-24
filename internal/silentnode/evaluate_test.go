package silentnode

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const silentNodeID = metadata.UUID("11111111-1111-4111-8111-111111111111")

func TestEvaluateDistinguishesHealthySleepingSilentAndIgnored(t *testing.T) {
	now := time.Date(2026, time.July, 24, 22, 0, 0, 0, time.UTC)
	policy := Policy{
		MissedHeartbeats: 3,
		MinimumGrace:     time.Minute,
		FutureTolerance:  15 * time.Second,
	}
	base := Node{
		NodeID:                    silentNodeID,
		NodeKind:                  "mac",
		LifecycleStatus:           "active",
		ExpectedHeartbeatInterval: time.Minute,
		CreatedAt:                 now.Add(-time.Hour),
		LastSeenAt:                now.Add(-2 * time.Minute),
	}
	tests := []struct {
		name  string
		node  Node
		at    time.Time
		state State
	}{
		{name: "healthy", node: base, at: now, state: StateHealthy},
		{
			name: "sleeping suppresses stale",
			node: func() Node {
				value := base
				value.SleepingAtEvaluation = true
				value.LastSeenAt = now.Add(-time.Hour)
				return value
			}(),
			at:    now,
			state: StateSleeping,
		},
		{
			name:  "silent after grace",
			node:  base,
			at:    now.Add(2 * time.Minute),
			state: StateSilent,
		},
		{
			name: "retired ignored",
			node: func() Node {
				value := base
				value.LifecycleStatus = "retired"
				return value
			}(),
			at:    now.Add(2 * time.Minute),
			state: StateIgnored,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := Evaluate(test.node, policy, test.at)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if decision.State != test.state {
				t.Fatalf("Evaluate() = %+v, want state %q", decision, test.state)
			}
		})
	}
}

func TestEvaluateRejectsFutureHeartbeatBeyondTolerance(t *testing.T) {
	now := time.Date(2026, time.July, 24, 22, 0, 0, 0, time.UTC)
	_, err := Evaluate(Node{
		NodeID:                    silentNodeID,
		NodeKind:                  "mac",
		LifecycleStatus:           "active",
		ExpectedHeartbeatInterval: time.Minute,
		CreatedAt:                 now.Add(-time.Hour),
		LastSeenAt:                now.Add(time.Minute),
	}, Policy{
		MissedHeartbeats: 3,
		FutureTolerance:  15 * time.Second,
	}, now)
	if !errors.Is(err, ErrInvalidNode) {
		t.Fatalf("Evaluate(future) error = %v, want %v", err, ErrInvalidNode)
	}
}
