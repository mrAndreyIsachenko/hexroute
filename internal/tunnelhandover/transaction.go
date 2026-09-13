package tunnelhandover

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Claimer is where ownership of the tunnel is recorded.
type Claimer interface {
	Place(transaction string) error
	Release() error
}

// Tunnel is what starts and stops the process this transaction moves.
//
// Absent during a rehearsal: a rehearsal that could start a tunnel is not a
// rehearsal. The transaction refuses to enter a phase it has no means to leave.
type Tunnel interface {
	Start(ctx context.Context) (int, error)
	Stop(pid int) error
}

// Prover answers whether traffic traversed the tunnel. A connection that
// completed is not an answer: this repository already records that a reachable
// socket is not qualification, and the handover cannot complete on evidence
// weaker than the thing it claims.
type Prover interface {
	Traversed(ctx context.Context) (bool, error)
}

// Policy is what completion requires.
type Policy struct {
	// Proofs is how many consecutive traversals complete the handover. One
	// answer says what one moment was like, which is the mistake the link cause
	// made six times in half an hour before it was given a threshold.
	Proofs int
	// Deadline bounds the whole attempt. Twice the fifty-seven seconds the
	// incumbent was measured taking to restart its own tunnel.
	Deadline time.Duration
	// Between is the wait between proofs, and between the readings that watch
	// the previous holder go.
	Between time.Duration
	// Possession bounds the wait for the previous holder to be gone. The
	// incumbent's own tunnel restart was measured at fifty-seven seconds end to
	// end on this machine; stopping is the cheap half of that.
	Possession time.Duration
}

// Outcome is what happened, in enough detail to be recorded.
type Outcome struct {
	Transaction string
	Rehearsal   bool
	Completed   bool
	Phase       Phase
	Proofs      int
	Reason      string
}

var (
	ErrNoTunnel      = errors.New("a real handover needs something to start")
	ErrInvalidPolicy = errors.New("invalid handover policy")
)

// Transaction runs one handover, or rehearses one.
type Transaction struct {
	Store *Store
	Claim Claimer
	// Incumbent is who holds the tunnel now, and is stopped before this runtime
	// starts its own. Absent during a rehearsal, for the same reason Tunnel is.
	Incumbent Incumbent
	Tunnel    Tunnel
	Prover    Prover
	Policy    Policy
	Now       func() time.Time
}

// Run carries the handover from prepared to proven, or aborts it back.
//
// The order is: record the phase, then perform it. A transaction that fell over
// between acting and recording would leave the machine changed by a phase no
// record mentions, and the abort that came later would not undo it.
func (transaction *Transaction) Run(ctx context.Context, id string, rehearsal bool) (Outcome, error) {
	if transaction.Policy.Proofs < 1 || transaction.Policy.Deadline <= 0 ||
		transaction.Policy.Possession <= 0 {
		return Outcome{}, ErrInvalidPolicy
	}
	if !rehearsal && transaction.Tunnel == nil {
		return Outcome{}, ErrNoTunnel
	}
	if !rehearsal && transaction.Incumbent == nil {
		return Outcome{}, ErrNoIncumbent
	}
	now := transaction.Now
	if now == nil {
		now = time.Now
	}

	session, err := transaction.Store.Begin(id, rehearsal)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Transaction: id, Rehearsal: rehearsal, Phase: PhasePrepared}

	if rehearsal {
		// Every phase except the two that change the machine. What is exercised
		// is the session record, the proving and the abort — which is all of it
		// except the part whose first mistake costs the network.
		proofs, reason := transaction.prove(ctx, now)
		outcome.Proofs, outcome.Reason = proofs, reason
		outcome.Completed = proofs >= transaction.Policy.Proofs
		if !outcome.Completed && outcome.Reason == "" {
			outcome.Reason = "the payload did not traverse"
		}
		phase := PhaseProven
		if !outcome.Completed {
			phase = PhaseAborted
		}
		if _, err := transaction.Store.Advance(session, phase); err != nil {
			return outcome, err
		}
		outcome.Phase = phase
		return outcome, transaction.Store.Clear()
	}

	session, err = transaction.Store.Advance(session, PhaseClaimed)
	if err != nil {
		return outcome, err
	}
	outcome.Phase = PhaseClaimed
	if err := transaction.Claim.Place(id); err != nil {
		return outcome, transaction.abort(&outcome, session, 0, fmt.Sprintf("the claim was refused: %v", err))
	}

	// Taking the tunnel from its holder belongs to the claimed phase rather than
	// to one of its own. The claim is what authorises it, and releasing the
	// claim is what undoes it: the previous owner restarts a tunnel it finds
	// missing once it is supervising again. A phase whose undo is another
	// phase's undo is not a phase.
	if err := transaction.possess(ctx, now); err != nil {
		return outcome, transaction.abort(&outcome, session, 0, err.Error())
	}

	session, err = transaction.Store.Advance(session, PhaseStarted)
	if err != nil {
		return outcome, err
	}
	outcome.Phase = PhaseStarted
	pid, err := transaction.Tunnel.Start(ctx)
	if err != nil {
		return outcome, transaction.abort(&outcome, session, 0, fmt.Sprintf("the tunnel did not start: %v", err))
	}
	session.StartedPID = pid

	proofs, reason := transaction.prove(ctx, now)
	outcome.Proofs = proofs
	if proofs < transaction.Policy.Proofs {
		if reason == "" {
			reason = "the payload did not traverse twice inside the deadline"
		}
		return outcome, transaction.abort(&outcome, session, pid, reason)
	}

	if _, err := transaction.Store.Advance(session, PhaseProven); err != nil {
		return outcome, err
	}
	outcome.Phase, outcome.Completed = PhaseProven, true
	return outcome, transaction.Store.Clear()
}

// prove waits for consecutive traversals, and gives up at the deadline.
//
// Consecutive rather than cumulative: a path that traverses, fails, and
// traverses again has not been shown to hold, and the count starts over.
func (transaction *Transaction) prove(ctx context.Context, now func() time.Time) (int, string) {
	deadline := now().Add(transaction.Policy.Deadline)
	consecutive := 0
	for now().Before(deadline) {
		traversed, err := transaction.Prover.Traversed(ctx)
		if err != nil {
			return consecutive, fmt.Sprintf("the proof could not be taken: %v", err)
		}
		if traversed {
			consecutive++
			if consecutive >= transaction.Policy.Proofs {
				return consecutive, ""
			}
		} else {
			consecutive = 0
		}
		if transaction.Policy.Between > 0 {
			select {
			case <-ctx.Done():
				return consecutive, "the transaction was interrupted"
			case <-time.After(transaction.Policy.Between):
			}
		}
	}
	return consecutive, ""
}

// abort returns the tunnel rather than releasing it.
//
// The previous owner restarts the process when it finds it missing — measured
// at fifty-seven seconds on this machine — so the abort removes the claim and
// stops what this runtime started, and the path that already exists does the
// rest. Leaving the tunnel stopped and unowned would leave the machine without
// a network for as long as nobody is watching the terminal.
func (transaction *Transaction) abort(
	outcome *Outcome,
	session Session,
	pid int,
	reason string,
) error {
	outcome.Reason = reason
	if pid > 0 && transaction.Tunnel != nil {
		if err := transaction.Tunnel.Stop(pid); err != nil {
			outcome.Reason = fmt.Sprintf("%s; and what was started could not be stopped: %v", reason, err)
		}
	}
	if err := transaction.Claim.Release(); err != nil {
		return fmt.Errorf("the claim could not be released after %s: %w", reason, err)
	}
	if _, err := transaction.Store.Advance(session, PhaseAborted); err != nil {
		return err
	}
	outcome.Phase = PhaseAborted
	return transaction.Store.Clear()
}

// Abort undoes whatever a previous invocation left behind.
//
// A terminal that closed mid-transaction leaves a phase on disk and nothing
// watching it. This reads that phase and undoes exactly it: the claim if one
// was placed, the process if one was started.
func (transaction *Transaction) Abort() (Outcome, error) {
	session, inFlight, err := transaction.Store.Read()
	if err != nil {
		return Outcome{}, err
	}
	if !inFlight {
		return Outcome{Completed: false, Reason: "no handover was in flight"}, nil
	}
	outcome := Outcome{
		Transaction: session.Transaction, Rehearsal: session.Rehearsal,
		Phase: session.Phase,
	}
	return outcome, transaction.abort(&outcome, session, session.StartedPID,
		fmt.Sprintf("abandoned at %s", session.Phase))
}
