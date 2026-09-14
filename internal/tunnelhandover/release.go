package tunnelhandover

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
)

var (
	ErrNoOwnTunnel = errors.New("a release needs to know which tunnel is this runtime's")
	ErrNotReleased = errors.New("the tunnel could not be handed back")
)

// Release gives the tunnel back to the previous owner.
//
// It is the handover run backwards, and it keeps the handover's discipline: the
// phase is recorded before the act it names, completion is traffic and not an
// interface, and a terminal that dies leaves a phase a later invocation undoes.
//
// It stops this runtime's tunnel before it releases the claim. Releasing first
// would leave two runtimes each believing the tunnel is theirs, and the previous
// owner finds its stale process by its own configuration, so it starts a second
// one beside this runtime's rather than replacing it.
//
// If the previous owner does not raise a tunnel inside the deadline, this
// runtime takes it again. A machine with no tunnel and no owner until a person
// reads the terminal is what the handover refused; it is no better this way.
func (transaction *Transaction) Release(ctx context.Context, id string) (Outcome, error) {
	if transaction.Policy.Proofs < 1 || transaction.Policy.Deadline <= 0 ||
		transaction.Policy.Possession <= 0 || transaction.Policy.Between <= 0 {
		return Outcome{}, ErrInvalidPolicy
	}
	if transaction.Own == nil {
		return Outcome{}, ErrNoOwnTunnel
	}
	if transaction.Tunnel == nil {
		return Outcome{}, ErrNoTunnel
	}
	if transaction.Incumbent == nil {
		return Outcome{}, ErrNoIncumbent
	}
	now := transaction.clock()

	session, err := transaction.Store.BeginRelease(id)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Transaction: id, Kind: KindRelease, Phase: PhasePrepared}

	session, err = transaction.Store.Advance(session, PhaseStopping)
	if err != nil {
		return outcome, err
	}
	outcome.Phase = PhaseStopping
	if err := transaction.stopOwn(ctx, now); err != nil {
		return outcome, transaction.keep(ctx, &outcome, session,
			fmt.Sprintf("this runtime's tunnel did not stop: %v", err))
	}

	session, err = transaction.Store.Advance(session, PhaseReleased)
	if err != nil {
		return outcome, err
	}
	outcome.Phase = PhaseReleased
	if err := transaction.Claim.Release(); err != nil {
		return outcome, transaction.restore(ctx, &outcome, session,
			fmt.Sprintf("the claim could not be released: %v", err))
	}

	proofs, reason := transaction.prove(ctx, now)
	outcome.Proofs = proofs
	if proofs < transaction.Policy.Proofs {
		if reason == "" {
			reason = "the previous owner's tunnel did not carry traffic twice inside the deadline"
		}
		return outcome, transaction.restore(ctx, &outcome, session, reason)
	}

	if _, err := transaction.Store.Advance(session, PhaseProven); err != nil {
		return outcome, err
	}
	outcome.Phase, outcome.Completed = PhaseProven, true
	return outcome, transaction.Store.Clear()
}

func (transaction *Transaction) clock() func() time.Time {
	if transaction.Now == nil {
		return time.Now
	}
	return transaction.Now
}

// stopOwn stops this runtime's tunnel and waits until it is gone.
//
// Nothing running is not an error: a release run after this runtime's tunnel
// already died still has a claim to give back.
func (transaction *Transaction) stopOwn(ctx context.Context, now func() time.Time) error {
	pid, running, err := transaction.Own.Running(ctx)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	if err := transaction.Own.Stop(pid); err != nil {
		return err
	}
	deadline := now().Add(transaction.Policy.Possession)
	for {
		_, running, err := transaction.Own.Running(ctx)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		if !now().Before(deadline) {
			return fmt.Errorf("pid %d was still there after %s", pid, transaction.Policy.Possession)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupted while waiting for pid %d", pid)
		case <-time.After(transaction.Policy.Between):
		}
	}
}

// keep leaves the tunnel with this runtime when it could not be stopped.
//
// The claim was never released, so the previous owner still stands down. What
// is left to make sure of is that this runtime's tunnel runs: a stop that
// half-worked must not leave the machine with a claim and nothing behind it.
func (transaction *Transaction) keep(
	ctx context.Context, outcome *Outcome, session Session, reason string,
) error {
	outcome.Reason = reason
	if _, running, err := transaction.Own.Running(ctx); err != nil || !running {
		if _, err := transaction.Tunnel.Start(ctx); err != nil {
			return fmt.Errorf("%w: %s; and this runtime's tunnel could not be started again: %v",
				ErrNotReleased, reason, err)
		}
	}
	if _, err := transaction.Store.Advance(session, PhaseAborted); err != nil {
		return err
	}
	outcome.Phase = PhaseAborted
	return transaction.Store.Clear()
}

// restore takes the tunnel again after the previous owner did not.
//
// In the handover's order and for the handover's reasons: the claim first, so
// the previous owner stops starting a tunnel; then whatever it did raise, if it
// raised one late; then this runtime's own.
func (transaction *Transaction) restore(
	ctx context.Context, outcome *Outcome, session Session, reason string,
) error {
	outcome.Reason = reason
	if _, err := transaction.Store.Advance(session, PhaseRestored); err != nil {
		return err
	}
	outcome.Phase = PhaseRestored
	// A claim already on disk is the state restore wants, not a failure: a
	// release whose claim could not be removed restores over it.
	if err := transaction.Claim.Place(session.Transaction); err != nil && !errors.Is(err, tunnelclaim.ErrHeld) {
		return fmt.Errorf("%w: %s; and the claim could not be placed again: %v", ErrNotReleased, reason, err)
	}
	if err := transaction.possess(ctx, transaction.clock()); err != nil {
		return fmt.Errorf("%w: %s; and the previous owner's late tunnel could not be taken: %v",
			ErrNotReleased, reason, err)
	}
	if _, running, err := transaction.Own.Running(ctx); err != nil || !running {
		if _, err := transaction.Tunnel.Start(ctx); err != nil {
			return fmt.Errorf("%w: %s; and this runtime's tunnel could not be started again: %v",
				ErrNotReleased, reason, err)
		}
	}
	return transaction.Store.Clear()
}

// abortRelease undoes a release a closed terminal left behind, by its phase.
func (transaction *Transaction) abortRelease(
	ctx context.Context, outcome Outcome, session Session,
) (Outcome, error) {
	if transaction.Own == nil || transaction.Tunnel == nil || transaction.Incumbent == nil {
		return outcome, ErrNoOwnTunnel
	}
	reason := fmt.Sprintf("release abandoned at %s", session.Phase)
	switch session.Phase {
	case PhasePrepared:
		outcome.Reason = reason
		return outcome, transaction.Store.Clear()
	case PhaseStopping:
		return outcome, transaction.keep(ctx, &outcome, session, reason)
	default:
		return outcome, transaction.restore(ctx, &outcome, session, reason)
	}
}
