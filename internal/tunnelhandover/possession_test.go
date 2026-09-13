package tunnelhandover

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The tunnel is taken from its holder before a second one is started.
//
// This is the defect the rehearsal could not see, because a rehearsal skips
// exactly the two phases involved. Without the possession step the transaction
// placed the claim and started sing-box while the incumbent sing-box was still
// running: two processes on one tunnel address, which is the one arrangement
// neither of them recovers from.
//
// The assertion is on the order, not on the counts. Counting would pass for a
// transaction that stopped the incumbent after starting its own.
func TestTheIncumbentIsStoppedBeforeTheNewTunnelStarts(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Incumbent = &holder{pid: 501, running: true, journal: journal}

	outcome, err := handover.Run(context.Background(), "handover-possess", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Completed {
		t.Fatalf("outcome = %+v", outcome)
	}
	if got := strings.Join(journal.steps, ","); got != "claim,possess,start" {
		t.Fatalf("the handover went %q, not claim,possess,start", got)
	}
}

// Nothing to take is not a refusal.
//
// On a machine whose previous owner has already stopped, there is no incumbent
// and the handover has nothing to do about it. Refusing here would make the
// second attempt after an abort impossible.
func TestAnAbsentIncumbentIsNotAnError(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	incumbent := &holder{running: false, journal: journal}
	handover.Incumbent = incumbent

	outcome, err := handover.Run(context.Background(), "handover-empty", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Completed {
		t.Fatalf("outcome = %+v", outcome)
	}
	if incumbent.signalled != 0 {
		t.Fatalf("something absent was signalled %d times", incumbent.signalled)
	}
	if got := strings.Join(journal.steps, ","); got != "claim,start" {
		t.Fatalf("the handover went %q, not claim,start", got)
	}
}

// A holder that will not go stops the handover, and no tunnel is started.
//
// Starting anyway would produce the arrangement this step exists to prevent,
// and the deadline is the only thing that can tell "slow to die" from "not
// dying". The claim is released, which is what returns the tunnel: the previous
// owner restarts what it finds missing once it is supervising again.
func TestAHolderThatWillNotGoAbortsBeforeStarting(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Incumbent = &holder{
		pid: 777, running: true, stopsAfter: 1_000_000, journal: journal,
	}

	outcome, err := handover.Run(context.Background(), "handover-stuck", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if !strings.Contains(outcome.Reason, "777") {
		t.Fatalf("the reason does not name what would not go: %q", outcome.Reason)
	}
	if tunnel.started != 0 {
		t.Fatalf("a tunnel was started beside one that had not stopped")
	}
	if claim.released != 1 || claim.held {
		t.Fatalf("the claim was released %d times and is held=%v", claim.released, claim.held)
	}
	if got := strings.Join(journal.steps, ","); got != "claim,possess" {
		t.Fatalf("the handover went %q, not claim,possess", got)
	}
}

// Not knowing whether anything holds the tunnel is not permission to start one.
//
// An observation that fails is the case where a second sing-box is most likely,
// because the reason the reading failed may be the machine being busy with the
// process it would have reported.
func TestAnUnreadableIncumbentAbortsBeforeStarting(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Incumbent = &holder{err: errors.New("ps refused"), journal: journal}

	outcome, err := handover.Run(context.Background(), "handover-blind", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if tunnel.started != 0 {
		t.Fatalf("a tunnel was started without knowing what else was running")
	}
	if claim.released != 1 {
		t.Fatalf("the claim was released %d times", claim.released)
	}
}

// A signal that cannot be delivered aborts, and says so.
//
// The abort would happen anyway when the deadline ran out, so counting the
// outcome is not enough: the transaction would report a holder that would not
// die where the truth is that this runtime was not allowed to ask it to. That
// sends the reader to the wrong half of the system, which is the failure this
// repository has paid for more than once.
func TestAHolderThatCannotBeSignalledAbortsBeforeStarting(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Incumbent = &holder{
		pid: 88, running: true, refuse: errors.New("not permitted"), journal: journal,
	}

	outcome, err := handover.Run(context.Background(), "handover-deaf", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed || tunnel.started != 0 || claim.released != 1 {
		t.Fatalf("outcome = %+v, started=%d, released=%d",
			outcome, tunnel.started, claim.released)
	}
	if !strings.Contains(outcome.Reason, "not permitted") {
		t.Fatalf("the reason does not name the refused signal: %q", outcome.Reason)
	}
}

// A real handover refuses when nothing can tell it who holds the tunnel.
//
// The same refusal as having nothing to start, and for the same reason: reaching
// the claim without it means the previous owner steps back and this runtime
// never takes what it stepped back from.
func TestARealHandoverRefusesWithoutAnIncumbent(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Incumbent = nil

	if _, err := handover.Run(context.Background(), "handover-nobody", false); !errors.Is(err, ErrNoIncumbent) {
		t.Fatalf("Run = %v, want ErrNoIncumbent", err)
	}
	if claim.placed != 0 {
		t.Fatalf("a claim was placed by a transaction that could not proceed")
	}
}

// A rehearsal stops nothing, the same way it starts nothing.
func TestARehearsalTakesTheTunnelFromNobody(t *testing.T) {
	journal := &order{}
	claim := &recordingClaim{journal: journal}
	handover := transaction(t, claim, nil, &answers{sequence: []bool{true, true}})
	incumbent := &holder{pid: 9, running: true, journal: journal}
	handover.Incumbent, handover.Tunnel = incumbent, nil

	outcome, err := handover.Run(context.Background(), "handover-rehearse", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Completed {
		t.Fatalf("outcome = %+v", outcome)
	}
	if incumbent.signalled != 0 || len(journal.steps) != 0 {
		t.Fatalf("a rehearsal touched the machine: signalled=%d steps=%v",
			incumbent.signalled, journal.steps)
	}
}

// A policy with no possession bound is refused rather than defaulted.
//
// A zero bound is a wait that ends on its first reading, which would start a
// second tunnel beside a holder that had merely not died yet.
func TestAPossessionBoundIsRequired(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Policy.Possession = 0

	if _, err := handover.Run(context.Background(), "handover-unbounded", false); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Run = %v, want ErrInvalidPolicy", err)
	}
}

// An interrupted transaction stops proving instead of running to its deadline.
//
// The operator holds this in the foreground; interrupting the terminal is how
// they take it back. The check was inside the branch that waits between proofs,
// so a policy with no wait could not be interrupted at all — found because the
// mutation that removed the deadline hung for ten minutes instead of failing,
// which is a test passing for the wrong reason.
func TestAnInterruptedTransactionStopsProving(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	// Always traverses but never twice in a row, so nothing but the deadline or
	// the interruption ends the loop.
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, false}})
	handover.Policy.Deadline = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	began := time.Now()
	outcome, err := handover.Run(ctx, "handover-interrupted", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed {
		t.Fatalf("an interrupted transaction completed: %+v", outcome)
	}
	if elapsed := time.Since(began); elapsed > 10*time.Second {
		t.Fatalf("it took %s to notice the interruption", elapsed)
	}
	if !strings.Contains(outcome.Reason, "interrupted") {
		t.Fatalf("the reason does not say it was interrupted: %q", outcome.Reason)
	}
}

// A policy with no wait between readings is refused.
//
// Both loops poll. A zero wait turns proving into a spin that takes its proofs
// as fast as the probe will answer, which is two readings of the same moment
// rather than two moments — exactly what the consecutive rule exists to stop.
func TestAWaitBetweenReadingsIsRequired(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	handover.Policy.Between = 0

	if _, err := handover.Run(context.Background(), "handover-spin", false); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Run = %v, want ErrInvalidPolicy", err)
	}
}
