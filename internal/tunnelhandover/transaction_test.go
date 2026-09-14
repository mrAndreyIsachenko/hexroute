package tunnelhandover

import (
	"context"
	"errors"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// order is what the fakes write down as they are called. The defect this file
// grew to cover was an ordering one — a tunnel started beside the one it was
// replacing — and a count of calls cannot see it.
type order struct{ steps []string }

func (journal *order) note(step string) {
	if journal == nil {
		return
	}
	journal.steps = append(journal.steps, step)
}

type recordingClaim struct {
	placed, released int
	held             bool
	refuse           error
	refuseRelease    error
	journal          *order
}

// Place refuses over a held claim, as the real store does, so a restore that
// finds its claim still on disk meets the same answer it would on the machine.
func (claim *recordingClaim) Place(string) error {
	if claim.refuse != nil {
		return claim.refuse
	}
	if claim.held {
		return tunnelclaim.ErrHeld
	}
	claim.journal.note("claim")
	claim.placed++
	claim.held = true
	return nil
}

func (claim *recordingClaim) Release() error {
	if claim.refuseRelease != nil {
		return claim.refuseRelease
	}
	claim.journal.note("release")
	claim.released++
	claim.held = false
	return nil
}

type recordingTunnel struct {
	started, stopped int
	pid              int
	refuse           error
	journal          *order
}

func (tunnel *recordingTunnel) Start(context.Context) (int, error) {
	if tunnel.refuse != nil {
		return 0, tunnel.refuse
	}
	tunnel.journal.note("start")
	tunnel.started++
	if tunnel.pid == 0 {
		tunnel.pid = 4242
	}
	return tunnel.pid, nil
}

func (tunnel *recordingTunnel) Stop(int) error { tunnel.stopped++; return nil }

// answers hands back a fixed sequence of proofs, then repeats the last.
//
// It refuses after a bound, so a proving loop that never ends returns instead of
// hanging. Without that, a transaction with no deadline is caught only by the
// test runner's timeout, which reports "the test took too long" where the truth
// is "nothing bounds the attempt" — and takes ten minutes to say it.
type answers struct {
	sequence []bool
	asked    int
	err      error
	limit    int
}

func (prover *answers) Traversed(context.Context) (bool, error) {
	if prover.err != nil {
		return false, prover.err
	}
	limit := prover.limit
	if limit == 0 {
		limit = 500
	}
	if prover.asked >= limit {
		return false, errors.New("the prover was asked past any deadline")
	}
	index := prover.asked
	prover.asked++
	if index >= len(prover.sequence) {
		index = len(prover.sequence) - 1
	}
	return prover.sequence[index], nil
}

// holder is the sing-box that was there first.
//
// stopsAfter is how many further readings it takes to be gone once signalled:
// zero is a process that dies on the signal, and a large number is one that
// does not.
type holder struct {
	pid         int
	running     bool
	stopsAfter  int
	signalled   int
	signalledAt int
	readings    int
	err         error
	refuse      error
	journal     *order
	label       string
}

func (incumbent *holder) Running(context.Context) (int, bool, error) {
	if incumbent.err != nil {
		return 0, false, incumbent.err
	}
	incumbent.readings++
	if !incumbent.running {
		return 0, false, nil
	}
	if incumbent.signalled > 0 && incumbent.readings > incumbent.signalledAt+incumbent.stopsAfter {
		incumbent.running = false
		return 0, false, nil
	}
	return incumbent.pid, true, nil
}

func (incumbent *holder) Stop(pid int) error {
	if incumbent.refuse != nil {
		return incumbent.refuse
	}
	step := "possess"
	if incumbent.label != "" {
		step = incumbent.label
	}
	incumbent.journal.note(step)
	incumbent.signalled++
	incumbent.signalledAt = incumbent.readings
	return nil
}

func transaction(t *testing.T, claim *recordingClaim, tunnel *recordingTunnel, prover *answers) *Transaction {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "handover.json"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	moment := time.Date(2026, 9, 13, 19, 0, 0, 0, time.UTC)
	return &Transaction{
		Store: store, Claim: claim, Tunnel: tunnel, Prover: prover,
		Incumbent: &holder{},
		Policy: Policy{
			Proofs: 2, Deadline: 120 * time.Second, Possession: 30 * time.Second,
			Between: time.Millisecond,
		},
		Now: func() time.Time {
			moment = moment.Add(time.Second)
			return moment
		},
	}
}

// A handover completes on two consecutive proofs that traffic traversed.
func TestAHandoverCompletesOnTwoProofs(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})

	outcome, err := handover.Run(context.Background(), "handover-1", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Completed || outcome.Phase != PhaseProven {
		t.Fatalf("outcome = %+v", outcome)
	}
	if claim.placed != 1 || !claim.held {
		t.Fatalf("the claim was placed %d times and is held=%v", claim.placed, claim.held)
	}
	if tunnel.started != 1 || tunnel.stopped != 0 {
		t.Fatalf("the tunnel was started %d and stopped %d", tunnel.started, tunnel.stopped)
	}
	if _, inFlight, err := handover.Store.Read(); err != nil || inFlight {
		t.Fatalf("a finished transaction left a record: %v %v", inFlight, err)
	}
}

// One proof is not enough, and the count starts over after a failure.
//
// A path that traverses, fails, and traverses again has not been shown to hold.
// Counting cumulatively would complete the handover on exactly the evidence the
// link cause was completing on when it was wrong six times in half an hour.
func TestProofsMustBeConsecutive(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	// true, false, true — never two in a row, and the deadline runs out.
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, false, true, false}})

	outcome, err := handover.Run(context.Background(), "handover-2", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed {
		t.Fatalf("a handover completed without two consecutive proofs: %+v", outcome)
	}
	if outcome.Phase != PhaseAborted {
		t.Fatalf("phase = %q, want aborted", outcome.Phase)
	}
	if claim.released != 1 || claim.held {
		t.Fatalf("the claim was not released: released=%d held=%v", claim.released, claim.held)
	}
	if tunnel.stopped != 1 {
		t.Fatalf("what was started was not stopped: %d", tunnel.stopped)
	}
}

// The deadline aborts, and aborting returns the tunnel rather than releasing it.
func TestTheDeadlineReturnsTheTunnel(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{false}})

	outcome, err := handover.Run(context.Background(), "handover-3", false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if claim.released != 1 {
		t.Fatal("the deadline passed and the claim was left in place")
	}
	if tunnel.stopped != 1 {
		t.Fatal("the deadline passed and the process this runtime started was left running")
	}
	if outcome.Reason == "" {
		t.Fatal("an abort with no reason")
	}
	// It stopped because the deadline passed, not because the prover gave up.
	// Those are the same outcome and different systems, and a transaction that
	// bounds nothing looks exactly like one that bounds correctly until the
	// difference is asserted.
	if strings.Contains(outcome.Reason, "past any deadline") {
		t.Fatalf("nothing bounded the attempt; it ran until the prover refused: %q", outcome.Reason)
	}
}

// A rehearsal performs every phase except the two that change the machine.
func TestARehearsalClaimsNothingAndStartsNothing(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})

	outcome, err := handover.Run(context.Background(), "rehearsal-1", true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Rehearsal {
		t.Fatal("a rehearsal did not say it was one")
	}
	if claim.placed != 0 || tunnel.started != 0 {
		t.Fatalf("a rehearsal claimed %d and started %d", claim.placed, tunnel.started)
	}
	if !outcome.Completed {
		t.Fatalf("a rehearsal whose proofs held reported failure: %+v", outcome)
	}
}

// A rehearsal needs no tunnel; a real handover refuses without one.
func TestARealHandoverRefusesWithoutATunnel(t *testing.T) {
	claim := &recordingClaim{}
	handover := transaction(t, claim, nil, &answers{sequence: []bool{true, true}})
	handover.Tunnel = nil

	if _, err := handover.Run(context.Background(), "handover-4", false); !errors.Is(err, ErrNoTunnel) {
		t.Fatalf("a real handover ran with nothing to start: %v", err)
	}
	if claim.placed != 0 {
		t.Fatal("it claimed the tunnel before finding it had nothing to start")
	}
	if _, err := handover.Run(context.Background(), "rehearsal-2", true); err != nil {
		t.Fatalf("a rehearsal refused for want of a tunnel: %v", err)
	}
}

// Two transactions do not overlap.
func TestASecondTransactionIsRefused(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})
	if _, err := handover.Store.Begin("handover-5", false); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := handover.Run(context.Background(), "handover-6", false); !errors.Is(err, ErrInFlight) {
		t.Fatalf("a second transaction started beside the first: %v", err)
	}
}

// A later invocation undoes what a closed terminal left behind.
func TestALaterInvocationAbortsWhatItFinds(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true, true}})

	session, err := handover.Store.Begin("handover-7", false)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	session, err = handover.Store.Advance(session, PhaseStarted)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	session.StartedPID = 9001
	if _, err := handover.Store.Advance(session, PhaseStarted); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	outcome, err := handover.Abort()
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if outcome.Transaction != "handover-7" {
		t.Fatalf("it aborted %q", outcome.Transaction)
	}
	if tunnel.stopped != 1 {
		t.Fatal("the process the abandoned transaction started was left running")
	}
	if claim.released != 1 {
		t.Fatal("the abandoned claim was left in place")
	}
	if _, inFlight, _ := handover.Store.Read(); inFlight {
		t.Fatal("the abandoned record survived its own abort")
	}
}

// Aborting when nothing is in flight is not an error.
func TestAbortingNothingIsNotAnError(t *testing.T) {
	claim, tunnel := &recordingClaim{}, &recordingTunnel{}
	handover := transaction(t, claim, tunnel, &answers{sequence: []bool{true}})
	outcome, err := handover.Abort()
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if outcome.Completed {
		t.Fatal("aborting nothing reported a completed handover")
	}
}
