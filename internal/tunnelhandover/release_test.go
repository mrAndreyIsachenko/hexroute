package tunnelhandover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// releasing builds a transaction that holds the tunnel: the claim is placed and
// this runtime's tunnel is running.
func releasing(t *testing.T, prover *answers) (*Transaction, *recordingClaim, *recordingTunnel, *holder, *holder, *order) {
	t.Helper()
	journal := &order{}
	claim := &recordingClaim{held: true, journal: journal}
	tunnel := &recordingTunnel{journal: journal}
	own := &holder{pid: 44094, running: true, journal: journal, label: "stop-own"}
	previous := &holder{journal: journal}
	handover := transaction(t, claim, tunnel, prover)
	handover.Own = own
	handover.Incumbent = previous
	return handover, claim, tunnel, own, previous, journal
}

// The tunnel goes back in the one order that leaves one tunnel.
//
// Stopping first leaves a moment with no tunnel while the claim is held, which
// the previous owner does not act on. Releasing first leaves both runtimes
// believing the tunnel is theirs, and the previous owner starts a second one
// beside this runtime's because it recognises only its own configuration.
func TestReleaseStopsItsOwnTunnelThenReleasesThenProves(t *testing.T) {
	handover, claim, tunnel, _, _, journal := releasing(t, &answers{sequence: []bool{true, true}})

	outcome, err := handover.Release(context.Background(), "release-1")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !outcome.Completed || outcome.Phase != PhaseProven || outcome.Kind != KindRelease {
		t.Fatalf("outcome = %+v", outcome)
	}
	if got := strings.Join(journal.steps, ","); got != "stop-own,release" {
		t.Fatalf("the release went %q, not stop-own,release", got)
	}
	if claim.held || tunnel.started != 0 {
		t.Fatalf("held=%v started=%d after a completed release", claim.held, tunnel.started)
	}
	if _, inFlight, err := handover.Store.Read(); err != nil || inFlight {
		t.Fatalf("a finished release left a record: %v %v", inFlight, err)
	}
}

// If the previous owner does not raise the tunnel in time, this runtime takes
// it back rather than leaving the machine with no tunnel and no owner.
func TestReleaseTakesTheTunnelAgainWhenThePreviousOwnerDoesNot(t *testing.T) {
	handover, claim, tunnel, _, _, journal := releasing(t, &answers{sequence: []bool{false}})

	outcome, err := handover.Release(context.Background(), "release-2")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseRestored {
		t.Fatalf("outcome = %+v", outcome)
	}
	if !strings.Contains(outcome.Reason, "did not carry traffic") {
		t.Fatalf("the reason does not say the previous owner did not take it: %q", outcome.Reason)
	}
	if got := strings.Join(journal.steps, ","); got != "stop-own,release,claim,start" {
		t.Fatalf("the restore went %q, not stop-own,release,claim,start", got)
	}
	if !claim.held || tunnel.started != 1 {
		t.Fatalf("held=%v started=%d after a restore", claim.held, tunnel.started)
	}
}

// A tunnel the previous owner raised too late is taken before this runtime
// starts its own, for the reason the handover takes one.
func TestRestoreTakesALateTunnelBeforeStartingItsOwn(t *testing.T) {
	handover, _, tunnel, _, previous, journal := releasing(t, &answers{sequence: []bool{false}})
	previous.pid, previous.running = 777, true

	if _, err := handover.Release(context.Background(), "release-3"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := strings.Join(journal.steps, ","); got != "stop-own,release,claim,possess,start" {
		t.Fatalf("the restore went %q, not stop-own,release,claim,possess,start", got)
	}
	if tunnel.started != 1 {
		t.Fatalf("started %d tunnels", tunnel.started)
	}
}

// A tunnel that will not stop keeps its claim.
//
// Releasing a claim over a tunnel still running would be the arrangement this
// transaction exists to avoid, from the other side.
func TestATunnelThatWillNotStopKeepsItsClaim(t *testing.T) {
	handover, claim, tunnel, own, _, _ := releasing(t, &answers{sequence: []bool{true, true}})
	own.stopsAfter = 1_000_000

	outcome, err := handover.Release(context.Background(), "release-4")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if claim.released != 0 || !claim.held {
		t.Fatalf("the claim was released %d times over a tunnel that did not stop", claim.released)
	}
	if tunnel.started != 0 {
		t.Fatalf("a second tunnel was started beside one still running")
	}
}

// A tunnel that already died does not stop the claim being given back.
func TestAReleaseWithNothingRunningStillGivesTheClaimBack(t *testing.T) {
	handover, claim, _, own, _, journal := releasing(t, &answers{sequence: []bool{true, true}})
	own.running = false

	outcome, err := handover.Release(context.Background(), "release-5")
	if err != nil || !outcome.Completed {
		t.Fatalf("Release = %+v, %v", outcome, err)
	}
	if claim.held || strings.Join(journal.steps, ",") != "release" {
		t.Fatalf("held=%v steps=%v", claim.held, journal.steps)
	}
}

// A claim that could not be released is restored over, not failed on.
func TestAClaimThatCouldNotBeReleasedIsRestoredOver(t *testing.T) {
	handover, claim, tunnel, _, _, _ := releasing(t, &answers{sequence: []bool{true, true}})
	claim.refuseRelease = errors.New("read-only filesystem")

	outcome, err := handover.Release(context.Background(), "release-6")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if outcome.Completed || outcome.Phase != PhaseRestored {
		t.Fatalf("outcome = %+v", outcome)
	}
	if !claim.held || tunnel.started != 1 {
		t.Fatalf("held=%v started=%d: the tunnel stopped for a release that never happened",
			claim.held, tunnel.started)
	}
}

// A release needs to know which tunnel is its own, and touches nothing without.
func TestAReleaseRefusesWithoutItsOwnTunnel(t *testing.T) {
	handover, claim, _, _, _, journal := releasing(t, &answers{sequence: []bool{true, true}})
	handover.Own = nil
	if _, err := handover.Release(context.Background(), "release-7"); !errors.Is(err, ErrNoOwnTunnel) {
		t.Fatalf("Release = %v, want ErrNoOwnTunnel", err)
	}
	if !claim.held || len(journal.steps) != 0 {
		t.Fatalf("a refused release touched the machine: held=%v steps=%v", claim.held, journal.steps)
	}
}

// A later invocation undoes a release by the phase it finds.
func TestAbortingAReleaseUndoesItsPhase(t *testing.T) {
	for _, item := range []struct {
		name        string
		phase       Phase
		ownRunning  bool
		wantHeld    bool
		wantStarted int
	}{
		{"stopped own, claim still held", PhaseStopping, false, true, 1},
		{"claim released", PhaseReleased, false, true, 1},
		{"prepared", PhasePrepared, true, true, 0},
	} {
		t.Run(item.name, func(t *testing.T) {
			handover, claim, tunnel, own, _, _ := releasing(t, &answers{sequence: []bool{false}})
			own.running = item.ownRunning
			if item.phase == PhaseReleased {
				claim.held = false
			}
			session, err := handover.Store.BeginRelease("release-abandoned")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := handover.Store.Advance(session, item.phase); err != nil {
				t.Fatal(err)
			}

			outcome, err := handover.Abort()
			if err != nil {
				t.Fatalf("Abort: %v", err)
			}
			if outcome.Kind != KindRelease {
				t.Fatalf("outcome = %+v", outcome)
			}
			if claim.held != item.wantHeld || tunnel.started != item.wantStarted {
				t.Fatalf("held=%v started=%d, want held=%v started=%d",
					claim.held, tunnel.started, item.wantHeld, item.wantStarted)
			}
			if _, inFlight, err := handover.Store.Read(); err != nil || inFlight {
				t.Fatalf("an undone release left a record: %v %v", inFlight, err)
			}
		})
	}
}

// A session written before releases existed is a handover.
func TestASessionWithoutAKindIsAHandover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handover.json")
	body := `{"schema":"` + Schema + `","transaction":"handover-old","phase":"started","started_at":"2026-09-14T07:30:00Z","started_pid":4242}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	session, inFlight, err := store.Read()
	if err != nil || !inFlight {
		t.Fatalf("Read = %v, %v", inFlight, err)
	}
	if session.Release() {
		t.Fatal("a session with no kind was read as a release")
	}
}

// A release does not begin beside a transaction in flight.
func TestAReleaseIsRefusedBesideATransactionInFlight(t *testing.T) {
	handover, claim, _, _, _, journal := releasing(t, &answers{sequence: []bool{true, true}})
	if _, err := handover.Store.Begin("handover-in-flight", false); err != nil {
		t.Fatal(err)
	}
	if _, err := handover.Release(context.Background(), "release-8"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("Release = %v, want ErrInFlight", err)
	}
	if !claim.held || len(journal.steps) != 0 {
		t.Fatalf("held=%v steps=%v", claim.held, journal.steps)
	}
}

var _ = time.Second
