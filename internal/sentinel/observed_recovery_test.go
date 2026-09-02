package sentinel

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

// The observing sentinel is not one that declines to act. It is one that
// cannot: the controller is built with no restarter at all, so there is
// nothing to call on the day someone changes the branch that checks the mode.
func TestAnObservingCycleHoldsNoMeansOfActing(t *testing.T) {
	cycle, err := NewCycle(
		sentinelRuntimeFixture(t),
		fakeHeartbeatReader{record: heartbeat.Record{
			Schema: heartbeat.Schema, Sequence: 1, PID: 1, MonotonicTick: 1,
		}},
		fakeEndpointObserver{ready: true},
		mustTracker(t),
	)
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	if cycle.recovery == nil {
		t.Fatal("the cycle plans nothing")
	}
	if cycle.recovery.authority != RecoveryObserveOnly {
		t.Fatalf("authority %q, want observe-only", cycle.recovery.authority)
	}
	if cycle.recovery.restarter != nil {
		t.Fatal("an observing sentinel holds a restarter it could call")
	}
}

func mustTracker(t *testing.T) *Tracker {
	t.Helper()
	tracker, err := NewTracker(30)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return tracker
}

// 1.2, 2.4 — the plan comes from the planner and not from the raw evidence.
// A cycle where only one source failed still has a phase, and it is the
// planner that says which.
func TestIncompleteEvidenceStillProducesAPhase(t *testing.T) {
	cycle, err := NewCycle(
		sentinelRuntimeFixture(t),
		fakeHeartbeatReader{record: heartbeat.Record{
			Schema: heartbeat.Schema, Sequence: 1, PID: 1, MonotonicTick: 1,
		}},
		// The data path is healthy, so the gate cannot be met.
		fakeEndpointObserver{ready: true},
		mustTracker(t),
	)
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	summary := cycle.Observe(context.Background(), 0)
	if summary.Decision.EvidenceReady {
		t.Fatal("one healthy source met the gate")
	}
	if !summary.PlanKnown {
		t.Fatalf("no plan was produced: %v", summary.PlanRefused)
	}
	if summary.Plan.Action != RecoveryActionNone {
		t.Fatalf("action %q with incomplete evidence", summary.Plan.Action)
	}
	if summary.Plan.Phase != RecoveryMonitoring {
		t.Fatalf("phase %q, want monitoring", summary.Plan.Phase)
	}
	if !summary.Plan.ObserveOnly {
		t.Fatal("the plan does not say it was observed rather than acted on")
	}
}

// 1.4 — a planner that refuses an input must not take the watching with it.
//
// The refusal has to be provoked at the planner rather than at the tracker:
// the tracker guards a backwards tick itself and refuses first, so through an
// ordinary cycle the planner's own refusal path is unreachable. A decision
// that disagrees with itself is one only a broken tracker produces, and the
// planner is right to refuse it — what matters is that the sentinel keeps
// looking afterwards.
type inconsistentTracker struct{}

func (inconsistentTracker) Evaluate(
	control.Tick, HeartbeatObservation, bool,
) (Decision, error) {
	return Decision{
		ObserveOnly: true, Action: ActionNone,
		HeartbeatStale: false, DataPathBroken: false,
		EvidenceReady: true,
	}, nil
}

func TestAPlannerRefusalDoesNotStopTheCycle(t *testing.T) {
	cycle, err := NewCycle(
		sentinelRuntimeFixture(t),
		fakeHeartbeatReader{record: heartbeat.Record{
			Schema: heartbeat.Schema, Sequence: 1, PID: 1, MonotonicTick: 1,
		}},
		fakeEndpointObserver{ready: true},
		inconsistentTracker{},
	)
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	summary := cycle.Observe(context.Background(), 10)
	if summary.PlanKnown {
		t.Fatal("the planner accepted a decision that disagrees with itself")
	}
	if summary.PlanRefused == nil {
		t.Fatal("a refusal was not reported")
	}
	// The observation itself survived, which is the point: the sentinel's job
	// is to keep watching.
	if summary.Failures != 0 || !summary.HeartbeatFound || !summary.DataPathReady {
		t.Fatalf("a planner refusal cost the observation: %+v", summary)
	}
}

// 2.2 — the same phase holding is recorded once, not once per cycle. A line
// repeated every cycle stops being read after the third one.
func TestAPlanIsRecordedOnChangeRatherThanEveryCycle(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(&out, logging.ComponentSentinel)
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	steady := Summary{
		PlanKnown: true,
		Plan: RecoveryPlan{
			ObserveOnly: true, Phase: RecoveryMonitoring,
			Action: RecoveryActionNone,
		},
	}
	var last recordedPlan
	for attempt := 0; attempt < 5; attempt++ {
		last, err = emitPlan(logger, steady, last)
		if err != nil {
			t.Fatalf("emitPlan: %v", err)
		}
	}
	if lines := count(out.String(), "sentinel_recovery_plan"); lines != 1 {
		t.Fatalf("five identical plans wrote %d lines, want 1", lines)
	}

	// A change is written.
	moved := steady
	moved.Plan.Phase = RecoveryVerifying
	moved.Plan.Action = RecoveryActionRestartRoot
	if _, err := emitPlan(logger, moved, last); err != nil {
		t.Fatalf("emitPlan: %v", err)
	}
	if lines := count(out.String(), "sentinel_recovery_plan"); lines != 2 {
		t.Fatalf("a changed plan wrote %d lines in total, want 2", lines)
	}
}

// 2.3 — the bound is its own record. An authorized sentinel spends its one
// attempt and stops; the observing one has no attempt to spend, so the moment
// is invisible unless it is written down separately.
func TestTheAttemptBoundIsRecordedOnceAndOnItsOwn(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(&out, logging.ComponentSentinel)
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	var last recordedPlan
	for _, phase := range []RecoveryPhase{
		RecoveryMonitoring, RecoveryVerifying, RecoveryCooldown,
	} {
		summary := Summary{PlanKnown: true, Plan: RecoveryPlan{
			ObserveOnly: true, Phase: phase, Action: RecoveryActionNone,
		}}
		last, err = emitPlan(logger, summary, last)
		if err != nil {
			t.Fatalf("emitPlan: %v", err)
		}
	}
	if lines := count(out.String(), "sentinel_recovery_bound"); lines != 1 {
		t.Fatalf("reaching cooldown wrote %d bound records, want 1", lines)
	}
	// Still in cooldown is not the bound again.
	cooling := Summary{PlanKnown: true, Plan: RecoveryPlan{
		ObserveOnly: true, Phase: RecoveryCooldown, Action: RecoveryActionNone,
	}}
	if _, err := emitPlan(logger, cooling, last); err != nil {
		t.Fatalf("emitPlan: %v", err)
	}
	if lines := count(out.String(), "sentinel_recovery_bound"); lines != 1 {
		t.Fatalf("staying in cooldown wrote %d bound records, want 1", lines)
	}
}

// A planner that stopped answering says so once, and the sentinel keeps
// watching. A silent planner and a planner with nothing to say produce the
// same empty log.
func TestARefusedPlanIsReportedOnce(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(&out, logging.ComponentSentinel)
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	refused := Summary{PlanRefused: ErrInvalidRecovery}
	var last recordedPlan
	for attempt := 0; attempt < 4; attempt++ {
		last, err = emitPlan(logger, refused, last)
		if err != nil {
			t.Fatalf("emitPlan: %v", err)
		}
	}
	if lines := count(out.String(), "sentinel_planner_unavailable"); lines != 1 {
		t.Fatalf("four refusals wrote %d lines, want 1", lines)
	}
}

func count(haystack, needle string) int {
	return strings.Count(haystack, needle)
}
