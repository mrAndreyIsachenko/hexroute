package cloudincident

import (
	"errors"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/silentnode"
)

const (
	testNodeID     = metadata.UUID("11111111-1111-4111-8111-111111111111")
	testTriggerID  = metadata.UUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	testRecoveryID = metadata.UUID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	testSleepID    = metadata.UUID("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
)

func TestSignalFromSilentDecisionMapsEvidenceRoles(t *testing.T) {
	now := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		decision   silentnode.Decision
		state      ConditionState
		reason     ResolutionReason
		evidenceID metadata.UUID
		role       EvidenceRole
	}{
		{
			name: "silent",
			decision: silentnode.Decision{
				NodeID:           testNodeID,
				State:            silentnode.StateSilent,
				ReferenceEventID: testTriggerID,
				EvaluatedAt:      now,
			},
			state:      ConditionDetected,
			evidenceID: testTriggerID,
			role:       EvidenceTrigger,
		},
		{
			name: "healthy",
			decision: silentnode.Decision{
				NodeID:           testNodeID,
				State:            silentnode.StateHealthy,
				ReferenceEventID: testRecoveryID,
				EvaluatedAt:      now,
			},
			state:      ConditionCleared,
			reason:     ResolutionConditionCleared,
			evidenceID: testRecoveryID,
			role:       EvidenceRecovery,
		},
		{
			name: "sleeping",
			decision: silentnode.Decision{
				NodeID:       testNodeID,
				State:        silentnode.StateSleeping,
				SleepEventID: testSleepID,
				EvaluatedAt:  now,
			},
			state:      ConditionCleared,
			reason:     ResolutionExpectedSleep,
			evidenceID: testSleepID,
			role:       EvidenceExclusion,
		},
		{
			name: "ignored",
			decision: silentnode.Decision{
				NodeID:      testNodeID,
				State:       silentnode.StateIgnored,
				EvaluatedAt: now,
			},
			state:  ConditionCleared,
			reason: ResolutionNodeInactive,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal, err := SignalFromSilentDecision(test.decision)
			if err != nil {
				t.Fatalf("SignalFromSilentDecision() error = %v", err)
			}
			if signal.State != test.state ||
				signal.ResolutionReason != test.reason ||
				signal.CorrelationKey != "silent-node:"+string(testNodeID) {
				t.Fatalf("signal = %+v", signal)
			}
			if test.evidenceID == "" {
				if len(signal.Evidence) != 0 {
					t.Fatalf("evidence = %+v, want none", signal.Evidence)
				}
				return
			}
			if len(signal.Evidence) != 1 ||
				signal.Evidence[0].EventID != test.evidenceID ||
				signal.Evidence[0].Role != test.role {
				t.Fatalf("evidence = %+v", signal.Evidence)
			}
		})
	}
}

func TestSignalFromSilentDecisionRejectsInvalidEvidence(t *testing.T) {
	_, err := SignalFromSilentDecision(silentnode.Decision{
		NodeID:           testNodeID,
		State:            silentnode.StateSilent,
		ReferenceEventID: "not-a-uuid",
		EvaluatedAt:      time.Now(),
	})
	if !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidSignal)
	}
}
