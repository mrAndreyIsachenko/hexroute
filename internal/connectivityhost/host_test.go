package connectivityhost

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

func reachedEvidence() Evidence {
	return Evidence{
		Reached: true,
		Physical: observe.PhysicalNetwork{
			Interface: "en0",
			Gateway:   netip.MustParseAddr("192.0.2.1"),
			Link:      observe.LinkStateUp,
		},
		ConfiguredRoutes: 1,
		Routes: []observe.RouteObservation{{
			Destination: netip.MustParseAddr("192.0.2.1"),
			Interface:   "en0",
		}},
	}
}

func TestReaderTurnsOneCycleIntoASnapshot(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	status, _, err := reader.Observe(reachedEvidence(), connectivityreduce.PolicyDescriptor{}, 1000)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(status.Components) != len(connectivity.Components()) {
		t.Fatalf("%d components, want %d",
			len(status.Components), len(connectivity.Components()))
	}
	if status.SnapshotGeneration == 0 {
		t.Fatal("the first cycle produced no snapshot generation")
	}
}

// The cycle can return before it has observed everything — no managed TUN
// means it stops there. The read model must still describe what was seen
// rather than refusing the whole publication.
func TestEarlyReturningCycleStillProducesASnapshot(t *testing.T) {
	reader, err := Open(t.TempDir(), "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	evidence := Evidence{
		Reached: true,
		Physical: observe.PhysicalNetwork{
			Interface: "en0",
			Gateway:   netip.MustParseAddr("192.0.2.1"),
			Link:      observe.LinkStateUp,
		},
		ConfiguredRoutes: 1,
		TUNError:         errNoManagedTUN,
	}
	status, _, err := reader.Observe(evidence, connectivityreduce.PolicyDescriptor{}, 1000)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(status.Components) != len(connectivity.Components()) {
		t.Fatalf("%d components, want %d",
			len(status.Components), len(connectivity.Components()))
	}
}

var errNoManagedTUN = errors.New("no managed TUN")

// The live failure. A reducer version moved, so every stored checkpoint was
// output from rules this build does not have and none of them could be
// resumed from. The read model started empty, which is correct, sealed a
// checkpoint with no parent, which is also correct — and the store refused to
// write it, because a parentless record appended onto an existing lineage
// would read ever after as though the lineage had always started there.
//
// That refusal is right and the wedge it caused is not: every later cycle
// produced the same parentless checkpoint against the same pointer, so the
// read model could never store anything again. It observed nothing, published
// nothing and described nothing, on a host where it had been working.
func TestALineageItCannotProveDoesNotWedgeTheReadModel(t *testing.T) {
	root := t.TempDir()
	reader, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	base, err := continuousTick()
	if err != nil {
		t.Skipf("no continuous clock: %v", err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if _, _, err := reader.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{}, base+control.Tick(cycle)); err != nil {
			t.Fatalf("building the lineage, cycle %d: %v", cycle, err)
		}
	}

	// Every record becomes something this build cannot read, which is what a
	// reducer version bump does to a stored lineage.
	checkpoints := filepath.Join(root, "readmodel", "checkpoints")
	entries, err := os.ReadDir(checkpoints)
	if err != nil {
		t.Fatalf("read checkpoints: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no lineage was built, so there is nothing to refuse")
	}
	for _, entry := range entries {
		if err := os.WriteFile(filepath.Join(checkpoints, entry.Name()),
			[]byte(`{"schema":"output from other rules"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	restarted, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if _, _, err := restarted.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{},
			base+control.Tick(10+cycle)); err != nil {
			t.Fatalf("the read model is wedged at cycle %d: %v", cycle, err)
		}
	}

	// And the restart is on the record rather than passed off as a beginning.
	store, err := connectivitycheckpoint.Open(
		filepath.Join(root, "readmodel"), connectivitycheckpoint.Options{})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resume.Usable() {
		t.Fatalf("the new lineage cannot be resumed from: %s", resume)
	}
	index, err := store.Index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	found := false
	for _, entry := range index {
		checkpoint, loadErr := store.Load(entry.ID)
		if loadErr != nil {
			continue
		}
		if checkpoint.Break == nil {
			continue
		}
		found = true
		if checkpoint.Break.Reason == connectivitycheckpoint.ResumeReasonNone {
			t.Fatal("the lineage was abandoned for no stated reason")
		}
	}
	if !found {
		t.Fatal("the read model started a new lineage without recording that it had")
	}
}

// A host sequence orders every fact this host ever accepted, and the journals
// hold the ones it issued. When the lineage is lost but the journals survive —
// a store restored from a partial backup, a lineage evicted, every checkpoint
// unreadable, or an operator moving the refused lineage out of the way — a
// read model starting its count from zero mints a second fact for a position
// already taken.
//
// Nothing notices at the time. The facts are written, the model folds them and
// the daemon looks well. It is the next restart that fails, when replay reaches
// the journal and finds one sequence with two meanings, and it fails at
// startup, so under launchd the daemon never comes up again.
func TestALostLineageDoesNotReissueSequencesTheJournalsHold(t *testing.T) {
	root := t.TempDir()
	base, err := continuousTick()
	if err != nil {
		t.Skipf("no continuous clock: %v", err)
	}
	reader, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if _, _, err := reader.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{}, base+control.Tick(cycle)); err != nil {
			t.Fatalf("building the journals, cycle %d: %v", cycle, err)
		}
	}
	// The lineage goes; the journals stay exactly where they were.
	if err := os.Rename(filepath.Join(root, "readmodel"),
		filepath.Join(root, "readmodel.gone")); err != nil {
		t.Fatal(err)
	}

	restarted, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("restart refused: %v", err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if _, _, err := restarted.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{},
			base+control.Tick(10+cycle)); err != nil {
			t.Fatalf("cannot observe after the lineage was lost: %v", err)
		}
	}

	// This is the part that failed on the host. launchd restarts this daemon
	// on every install, crash and reboot, so one restart proving nothing is
	// the difference between a working read model and one that never comes
	// back.
	for restart := 0; restart < 3; restart++ {
		again, err := Open(root, "boot-0000000000000000")
		if err != nil {
			t.Fatalf("restart %d refuses to start: %v", restart+2, err)
		}
		if _, _, err := again.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{},
			base+control.Tick(20+control.Tick(restart))); err != nil {
			t.Fatalf("restart %d cannot observe: %v", restart+2, err)
		}
	}
}
