package logging

import "sync"

// ChangeGate answers whether an event still says what it said last time.
//
// An observe loop that logs every cycle records the passage of time rather
// than what happened. On one host that came to 33 MB across 167,000 lines, of
// which 98% were the same line: everything is fine, checked again. Nothing in
// that volume was information, and it buried the 1% that was.
//
// The read model already draws this distinction one layer down — its lineage
// advances on effective change, not on ticks — and this brings the daemons'
// logs into line with it.
//
// Liveness is deliberately not this log's job. The control-loop heartbeat
// carries it, and it advances whether or not anything changed. A log that
// repeats itself to prove the process is alive is answering a question that
// already has a better answer.
type ChangeGate struct {
	mu   sync.Mutex
	last map[EventName]string
}

// NewChangeGate returns a gate that has seen nothing, so the first observation
// of every event is always emitted.
func NewChangeGate() *ChangeGate {
	return &ChangeGate{last: make(map[EventName]string)}
}

// Changed reports whether this event now describes something different, and
// remembers the new description.
//
// A nil gate reports every observation as changed. That is the honest failure
// direction: losing the suppression costs volume, and losing the line costs
// evidence.
func (gate *ChangeGate) Changed(event EventName, state string) bool {
	if gate == nil {
		return true
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.last == nil {
		gate.last = make(map[EventName]string)
	}
	previous, seen := gate.last[event]
	if seen && previous == state {
		return false
	}
	gate.last[event] = state
	return true
}
