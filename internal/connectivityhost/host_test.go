package connectivityhost

import (
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
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

// The pointer is a mutable convenience: a crash between the index write and
// the pointer write can leave it absent, and a partial write can leave it
// truncated or holding something that is not a pointer at all. The lineage is
// what survives, and resume has always known that.
//
// Append did not. It read a missing pointer as an empty store, so a read model
// whose pointer was lost refused every checkpoint from then on — starting
// normally, observing normally, and storing nothing, for ever.
func TestALostPointerDoesNotStopTheReadModelStoring(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, pointer string)
	}{
		{"lost between two writes", func(t *testing.T, pointer string) {
			if err := os.Remove(pointer); err != nil {
				t.Fatal(err)
			}
		}},
		{"written half way", func(t *testing.T, pointer string) {
			if err := os.WriteFile(pointer, []byte(`{"schema":"hexro`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"readable but not a pointer", func(t *testing.T, pointer string) {
			if err := os.WriteFile(pointer, []byte(`{"schema":"other"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
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
					connectivityreduce.PolicyDescriptor{},
					base+control.Tick(cycle)); err != nil {
					t.Fatalf("building the lineage: %v", err)
				}
			}
			testCase.spoil(t, filepath.Join(root, "readmodel", "latest.json"))

			restarted, err := Open(root, "boot-0000000000000000")
			if err != nil {
				t.Fatalf("restart refused: %v", err)
			}
			for cycle := 0; cycle < 3; cycle++ {
				if _, _, err := restarted.Observe(reachedEvidence(),
					connectivityreduce.PolicyDescriptor{},
					base+control.Tick(10+cycle)); err != nil {
					t.Fatalf("cannot store after the pointer was %s: %v",
						testCase.name, err)
				}
			}
			// And the pointer heals: the next start needs no fallback.
			store, err := connectivitycheckpoint.Open(
				filepath.Join(root, "readmodel"), connectivitycheckpoint.Options{})
			if err != nil {
				t.Fatalf("store: %v", err)
			}
			if _, err := store.Pointer(); err != nil {
				t.Fatalf("the pointer was never rewritten: %v", err)
			}
		})
	}
}

// The rollback for this whole change is to stop passing the arguments. That
// claim is only worth as much as the evidence for it, and the evidence is
// that with them absent nothing of the read model exists: no reader, no store,
// no journals, no chain, and not one file where they would have been.
func TestRolledBackReadModelTouchesNothing(t *testing.T) {
	root := t.TempDir()

	reader, err := Open("", "boot-0000000000000000")
	if err != nil {
		t.Fatalf("a reader with no root refused instead of standing aside: %v", err)
	}
	if reader != nil {
		t.Fatal("a reader was built with no root configured")
	}

	// Every entry point the daemon uses, against the reader it actually has.
	if err := reader.AttachQualifier("", ""); err != nil {
		t.Fatalf("attaching nothing: %v", err)
	}
	status, changed, err := reader.Observe(reachedEvidence(),
		connectivityreduce.PolicyDescriptor{}, 1000)
	if err != nil || changed || len(status.Components) != 0 {
		t.Fatalf("a rolled-back reader produced a status: %+v %v %v",
			status, changed, err)
	}
	if _, err := reader.PublishUser(nil); err == nil {
		t.Fatal("a rolled-back reader accepted a publication")
	}
	quiet, err := logging.New(io.Discard, logging.ComponentDaemon)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	if err := Fold(reader, reachedEvidence(), nil, quiet); err != nil {
		t.Fatalf("folding into nothing: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the rolled-back path left %d entries on disk", len(entries))
	}
}

// A checkpoint has to carry everything the reduction that made it read, or it
// is an assertion rather than a record. A wake is an input like the policy and
// the evaluation tick: it changes what the reduction concludes.
//
// It was not recorded, so a replay could not be given it, produced a different
// snapshot, and reported the difference as the conclusion contradicting its own
// evidence — the one finding this lineage exists to make meaningful, arriving
// for a reason that had nothing to do with the host.
//
// The shape that shows it is a wake nobody answered: when the same cycle
// restates every component, the requirement the wake raised is cleared inside
// the batch and the snapshot ends up where a replay without the wake would put
// it anyway.
func TestAWakeIsCarriedByTheCheckpointItProduced(t *testing.T) {
	run := newSoak(t)
	for cycle := 0; cycle < 4; cycle++ {
		run.advance(time.Minute)
		run.flip(uint64(cycle + 1))
		run.cycle()
	}
	before := verifyStore(t, run)
	if before.Reproduced == 0 {
		t.Fatal("no link was verified before the sleep, so this measures nothing")
	}
	if before.Diverged != 0 {
		t.Fatalf("%d links diverged before any sleep", before.Diverged)
	}

	run.sleep(2 * time.Hour)
	run.advance(time.Minute)
	run.observeNothing()
	run.sample()

	after := verifyStore(t, run)
	if after.Diverged != 0 {
		t.Fatalf("a wake made %d link(s) unreproducible: the checkpoint does "+
			"not carry the input the reduction was given", after.Diverged)
	}
	if after.Reproduced <= before.Reproduced {
		t.Fatalf("the cycle after the wake produced no link to verify "+
			"(%d then %d)", before.Reproduced, after.Reproduced)
	}
}

func verifyStore(t *testing.T, run *soak) connectivitycheckpoint.VerifyResult {
	t.Helper()
	store, err := connectivitycheckpoint.Open(
		filepath.Join(run.storeRoot, "readmodel"), connectivitycheckpoint.Options{})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	result, err := connectivitycheckpoint.Verify(
		store, run.reader.rootJournal, run.reader.userJournal, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return result
}

// The read model has to come up on a host whose journals were written by an
// older build. It did not: their records were unreadable, so the store could
// not be opened, so the daemon was rejected — every ten seconds under
// launchd, for as long as the files were there. The lineage already knew how
// to abandon what it could not prove; the journals did not.
func TestAReadModelStartsOnJournalsFromAnOlderBuild(t *testing.T) {
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
			t.Fatalf("building the journals: %v", err)
		}
	}

	// What an older build leaves behind: records this one cannot read, and
	// nothing saying which format they are.
	for _, domain := range []string{"root", "user"} {
		if err := os.Remove(filepath.Join(root, domain, ".record-format")); err != nil {
			t.Fatalf("clearing the marker: %v", err)
		}
	}

	restarted, err := Open(root, "boot-0000000000000000")
	if err != nil {
		t.Fatalf("the read model refused to start on older journals: %v", err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if _, _, err := restarted.Observe(reachedEvidence(),
			connectivityreduce.PolicyDescriptor{},
			base+control.Tick(10+cycle)); err != nil {
			t.Fatalf("cannot observe after the journals were superseded: %v", err)
		}
	}
	// The records are kept, not destroyed.
	if _, err := os.Stat(filepath.Join(root, "root.superseded")); err != nil {
		t.Fatalf("the superseded journal was not kept: %v", err)
	}
}
