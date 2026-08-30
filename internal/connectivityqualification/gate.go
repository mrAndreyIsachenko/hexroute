package connectivityqualification

import "fmt"

// A qualification exists to gate something. The spec says what happens when it
// does not hold:
//
//	no later mutation change may use that evidence as a passing gate
//
// which is a statement about a reader that does not exist yet. What can be
// built now is the answer it will have to ask for, shaped so that the wrong
// answer is the hard one to obtain.

// Gate is whether a later change may treat this qualification as passed.
//
// Its fields are unexported on purpose. A zero Gate refuses, so a caller that
// forgets to ask, or that stores the answer and reads it back through a
// half-built value, is refused rather than admitted. Passing is something only
// a replay of the chain can produce.
type Gate struct {
	passing bool
	refusal string
}

// Passing reports whether the qualification may back a later change.
func (gate Gate) Passing() bool { return gate.passing }

// Refusal says in one bounded phrase why it may not. It is empty only when
// the gate passes.
func (gate Gate) Refusal() string { return gate.refusal }

// String renders the gate for a bounded diagnostic line.
func (gate Gate) String() string {
	if gate.passing {
		return "passing"
	}
	if gate.refusal == "" {
		return "refused: nothing was asked"
	}
	return "refused: " + gate.refusal
}

// GateFor replays the chain and answers whether it may gate a later change.
//
// Every way of not knowing is a refusal. A chain that is absent, unreadable,
// broken, from another session or simply unfinished all produce the same kind
// of answer, because a gate that treated "no evidence" differently from "bad
// evidence" would pass on an empty directory.
func GateFor(root string, binding Binding) Gate {
	progress, err := Inspect(root, binding)
	if err != nil {
		// Deliberately not the underlying error: what a caller may act on is
		// that the evidence could not be replayed, and the detail belongs in
		// the diagnostic the operator reads, not in a gate decision.
		return Gate{refusal: "the evidence chain could not be replayed"}
	}
	if !progress.Complete {
		refusal := progress.Blocking
		if refusal == "" {
			refusal = "the gate is not complete"
		}
		return Gate{refusal: refusal}
	}
	return Gate{passing: true}
}

// ErrGateRefused is what a caller returns when it declines to proceed.
var ErrGateRefused = fmt.Errorf("connectivity qualification did not pass")
