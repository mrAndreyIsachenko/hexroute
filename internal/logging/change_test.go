package logging

import "testing"

func TestFirstObservationIsAlwaysEmitted(t *testing.T) {
	gate := NewChangeGate()
	if !gate.Changed(EventObservationCycle, "ok") {
		t.Fatal("the first observation was suppressed")
	}
}

func TestRepeatedStateIsSuppressedAndAChangeIsNot(t *testing.T) {
	gate := NewChangeGate()
	gate.Changed(EventObservationCycle, "ok")
	if gate.Changed(EventObservationCycle, "ok") {
		t.Fatal("an unchanged state was emitted again")
	}
	if !gate.Changed(EventObservationCycle, "degraded") {
		t.Fatal("a change was suppressed")
	}
	// Returning to a state it held before is still a change from what was
	// last said, and must be reported.
	if !gate.Changed(EventObservationCycle, "ok") {
		t.Fatal("a return to a prior state was suppressed")
	}
}

func TestEventsAreTrackedSeparately(t *testing.T) {
	gate := NewChangeGate()
	gate.Changed(EventObservationCycle, "ok")
	if !gate.Changed(EventConnectivitySnapshot, "ok") {
		t.Fatal("one event's state suppressed another's")
	}
}

// Losing the gate must cost volume, never evidence.
func TestNilGateEmitsEverything(t *testing.T) {
	var gate *ChangeGate
	for range 3 {
		if !gate.Changed(EventObservationCycle, "ok") {
			t.Fatal("a nil gate suppressed a line")
		}
	}
}
