package sentinel

import (
	"testing"
)

func present(sequence uint64) HeartbeatObservation {
	return HeartbeatObservation{Present: true, Sequence: sequence}
}

func TestStaleHeartbeatAloneCannotTriggerRestart(t *testing.T) {
	tracker, _ := NewTracker(30)
	if _, err := tracker.Evaluate(0, present(10), true); err != nil {
		t.Fatalf("initial Evaluate() error: %v", err)
	}
	decision, err := tracker.Evaluate(30, present(10), true)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !decision.HeartbeatStale ||
		decision.DataPathBroken ||
		decision.EvidenceReady ||
		decision.Action != ActionNone ||
		!decision.ObserveOnly {
		t.Fatalf("Evaluate() = %+v", decision)
	}
}

func TestFailedDataPathAloneCannotTriggerRestart(t *testing.T) {
	tracker, _ := NewTracker(30)
	decision, err := tracker.Evaluate(0, present(10), false)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if decision.HeartbeatStale ||
		!decision.DataPathBroken ||
		decision.EvidenceReady ||
		decision.Action != ActionNone {
		t.Fatalf("Evaluate() = %+v", decision)
	}
}

func TestBothSignalsOnlyProduceObserveOnlyEvidence(t *testing.T) {
	tracker, _ := NewTracker(30)
	if _, err := tracker.Evaluate(0, present(10), false); err != nil {
		t.Fatalf("initial Evaluate() error: %v", err)
	}
	decision, err := tracker.Evaluate(30, present(10), false)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !decision.HeartbeatStale ||
		!decision.DataPathBroken ||
		!decision.EvidenceReady ||
		decision.Action != ActionNone ||
		!decision.ObserveOnly {
		t.Fatalf("Evaluate() = %+v", decision)
	}
}

func TestSequenceProgressResetsMonotonicStaleWindow(t *testing.T) {
	tracker, _ := NewTracker(30)
	_, _ = tracker.Evaluate(0, present(10), true)
	_, _ = tracker.Evaluate(29, present(10), true)
	progressed, err := tracker.Evaluate(30, present(11), true)
	if err != nil {
		t.Fatalf("progressed Evaluate() error: %v", err)
	}
	if progressed.HeartbeatStale {
		t.Fatalf("progressed Evaluate() = %+v", progressed)
	}
	beforeThreshold, _ := tracker.Evaluate(59, present(11), true)
	if beforeThreshold.HeartbeatStale {
		t.Fatalf("before-threshold Evaluate() = %+v", beforeThreshold)
	}
	atThreshold, _ := tracker.Evaluate(60, present(11), true)
	if !atThreshold.HeartbeatStale {
		t.Fatalf("at-threshold Evaluate() = %+v", atThreshold)
	}
}

func TestMissingHeartbeatGetsFullConservativeGraceWindow(t *testing.T) {
	tracker, _ := NewTracker(30)
	first, err := tracker.Evaluate(100, HeartbeatObservation{}, false)
	if err != nil {
		t.Fatalf("first Evaluate() error: %v", err)
	}
	if first.HeartbeatStale || first.EvidenceReady {
		t.Fatalf("first Evaluate() = %+v", first)
	}
	stale, err := tracker.Evaluate(130, HeartbeatObservation{}, false)
	if err != nil {
		t.Fatalf("stale Evaluate() error: %v", err)
	}
	if !stale.HeartbeatStale || !stale.EvidenceReady || stale.Action != ActionNone {
		t.Fatalf("stale Evaluate() = %+v", stale)
	}
}

func TestTrackerRejectsSequenceRollbackAndNonMonotonicTime(t *testing.T) {
	tracker, _ := NewTracker(30)
	_, _ = tracker.Evaluate(10, present(10), true)
	if _, err := tracker.Evaluate(11, present(9), true); err == nil {
		t.Fatal("Evaluate() accepted sequence rollback")
	}

	tracker, _ = NewTracker(30)
	_, _ = tracker.Evaluate(10, present(10), true)
	if _, err := tracker.Evaluate(9, present(10), true); err == nil {
		t.Fatal("Evaluate() accepted non-monotonic time")
	}
}
