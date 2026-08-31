package connectivitycheckpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

const replayTick = control.Tick(1100)

func testPolicy() connectivityreduce.PolicyDescriptor {
	return connectivityreduce.PolicyDescriptor{
		Present: true, Valid: true,
		BundleGeneration: 7, RootGeneration: 3, UserGeneration: 2,
		ManifestDigest: "b8f1c0d2e3a4956677889900aabbccddeeff00112233445566778899aabbccdd",
	}
}

// lineage builds a chain of real reductions so checkpoints describe something
// that actually happened rather than hand-written fields.
type lineage struct {
	t        *testing.T
	acceptor *connectivityaccept.Acceptor
	prior    *connectivityreduce.Snapshot
	parent   *Checkpoint
	broken   *LineageBreak
	consumed uint64
	sequence uint64
	// factsByHostSequence lets a replay test rebuild the journal the run
	// would have written.
	factsByHostSequence map[uint64]connectivity.Fact
}

func newLineage(t *testing.T) *lineage {
	t.Helper()
	return &lineage{t: t, acceptor: connectivityaccept.New(),
		factsByHostSequence: make(map[uint64]connectivity.Fact)}
}

func (l *lineage) next(components ...connectivity.Component) Checkpoint {
	l.t.Helper()
	if len(components) == 0 {
		components = connectivity.Components()
	}
	events := make([]connectivityreduce.Event, 0, len(components))
	for _, component := range components {
		l.sequence++
		fact := connectivity.FixtureBaseline(component, l.sequence)
		acceptance, err := l.acceptor.Accept(fact, fact.Domain)
		if err != nil {
			l.t.Fatalf("accept: %v", err)
		}
		l.factsByHostSequence[acceptance.HostSequence] = fact
		events = append(events, connectivityreduce.Event{Acceptance: acceptance, Fact: fact})
	}
	from := l.consumed + 1
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Prior: l.prior, Events: events, Policy: testPolicy(),
		BootID: connectivity.FixtureBootID, EvaluationTick: replayTick,
	})
	if err != nil {
		l.t.Fatalf("reduce: %v", err)
	}
	l.prior = &output.Snapshot
	l.consumed = output.Snapshot.ConsumedHostSequence

	id := fmt.Sprintf("cp-%04d", l.sequence)
	checkpoint, err := SealFrom(l.parent, l.broken, id, output, from, nil)
	if err != nil {
		l.t.Fatalf("seal: %v", err)
	}
	l.broken = nil
	stored := checkpoint
	l.parent = &stored
	return checkpoint
}

// nextBroken builds the checkpoint a read model produces when it could not
// prove what was already stored.
func (l *lineage) nextBroken(broken *LineageBreak) Checkpoint {
	l.t.Helper()
	// A restarted lineage numbers from its own beginning, so without this its
	// identifiers would collide with the lineage it is abandoning and the
	// record would name itself as what it left behind.
	if l.sequence < 100 {
		l.sequence = 100
	}
	l.broken = broken
	return l.next(connectivity.ComponentDNS)
}

// sealBroken returns the sealing error instead of failing on it, for the
// breaks that must never become records at all.
func (l *lineage) sealBroken(broken *LineageBreak) (Checkpoint, error) {
	l.t.Helper()
	l.broken = broken
	defer func() { l.broken = nil }()
	l.sequence++
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, l.sequence)
	acceptance, err := l.acceptor.Accept(fact, fact.Domain)
	if err != nil {
		l.t.Fatalf("accept: %v", err)
	}
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Prior: l.prior,
		Events: []connectivityreduce.Event{
			{Acceptance: acceptance, Fact: fact},
		},
		Policy: testPolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: replayTick,
	})
	if err != nil {
		l.t.Fatalf("reduce: %v", err)
	}
	return SealFrom(nil, broken, fmt.Sprintf("cp-%04d", l.sequence), output, 1, nil)
}

func openStore(t *testing.T, options Options) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "readmodel")
	store, err := Open(root, options)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return store, root
}

func TestAppendAndResumeTheNewestCheckpoint(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	var last Checkpoint
	for step := 0; step < 3; step++ {
		last = chain.next(connectivity.ComponentDNS)
		if err := store.Append(last); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Status != ResumeLatest || !resume.Usable() {
		t.Fatalf("resume %s", resume)
	}
	if resume.Checkpoint.ID != last.ID {
		t.Fatalf("resumed %s, want %s", resume.Checkpoint.ID, last.ID)
	}
}

func TestEmptyStoreResumesAtGenesis(t *testing.T) {
	store, _ := openStore(t, Options{})
	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Status != ResumeGenesis || resume.Checkpoint != nil {
		t.Fatalf("resume %s", resume)
	}
}

// A checkpoint may only extend the lineage it claims to extend.
func TestGenerationGuardRefusesAForeignParent(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}

	orphan := newLineage(t)
	stray := orphan.next(connectivity.ComponentRelays)
	if err := store.Append(stray); !errors.Is(err, ErrGenerationGuard) {
		t.Fatalf("got %v, want %v", err, ErrGenerationGuard)
	}
}

func TestFirstCheckpointCannotClaimAParent(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	chain.next(connectivity.ComponentDNS)
	second := chain.next(connectivity.ComponentDNS)
	if err := store.Append(second); !errors.Is(err, ErrGenerationGuard) {
		t.Fatalf("got %v, want %v", err, ErrGenerationGuard)
	}
}

// Every point a crash can land on during a durable write, for each of the
// three writes. None may leave a store that resumes into something unproven.
func TestCrashAtEveryWriteBoundaryLeavesAProvableStore(t *testing.T) {
	boundaries := []Boundary{
		BeforeFileSync, AfterFileSync, BeforeRename,
		AfterRename, BeforeDirectorySync, AfterDirectorySync,
	}
	operations := []Operation{OpCheckpoint, OpIndex, OpPointer}

	for _, operation := range operations {
		for _, boundary := range boundaries {
			t.Run(string(operation)+"/"+string(boundary), func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "readmodel")
				healthy, err := Open(root, Options{})
				if err != nil {
					t.Fatalf("open: %v", err)
				}
				chain := newLineage(t)
				first := chain.next(connectivity.ComponentDNS)
				if err := healthy.Append(first); err != nil {
					t.Fatalf("append first: %v", err)
				}

				crashing, err := Open(root, Options{
					Faults: []Fault{{Operation: operation, Boundary: boundary}},
				})
				if err != nil {
					t.Fatalf("open crashing: %v", err)
				}
				second := chain.next(connectivity.ComponentDNS)
				appendErr := crashing.Append(second)
				if appendErr == nil && boundary != AfterDirectorySync {
					t.Fatalf("the fault at %s/%s did not interrupt the write",
						operation, boundary)
				}

				restarted, err := Open(root, Options{})
				if err != nil {
					t.Fatalf("reopen: %v", err)
				}
				resume, err := restarted.Resume()
				if err != nil {
					t.Fatalf("resume: %v", err)
				}
				if !resume.Usable() {
					t.Fatalf("a crash at %s/%s left nothing provable: %s",
						operation, boundary, resume)
				}
				// Whatever survived must verify on its own terms.
				if err := resume.Checkpoint.Validate(); err != nil {
					t.Fatalf("resumed an unverifiable checkpoint: %v", err)
				}
				// The store may resume the old or the new checkpoint, but
				// never something that was never written.
				if resume.Checkpoint.ID != first.ID && resume.Checkpoint.ID != second.ID {
					t.Fatalf("resumed an unknown checkpoint %s", resume.Checkpoint.ID)
				}
			})
		}
	}
}

func corrupt(t *testing.T, root, id string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, checkpointDir, id+".json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(content, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mutate(generic)
	updated, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestTamperedLatestFallsBackToAProvableAncestor(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"parent link", func(record map[string]any) { record["parent"] = "cp-9999" }},
		{"parent digest", func(record map[string]any) {
			record["parent_digest"] = "00000000000000000000000000000000" +
				"00000000000000000000000000000000"
		}},
		{"snapshot digest", func(record map[string]any) {
			record["snapshot_digest"] = "11111111111111111111111111111111" +
				"11111111111111111111111111111111"
		}},
		{"diff digest", func(record map[string]any) {
			record["diff_digest"] = "22222222222222222222222222222222" +
				"22222222222222222222222222222222"
		}},
		{"generation", func(record map[string]any) { record["snapshot_generation"] = 99 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, root := openStore(t, Options{})
			chain := newLineage(t)
			first := chain.next(connectivity.ComponentDNS)
			if err := store.Append(first); err != nil {
				t.Fatalf("append: %v", err)
			}
			second := chain.next(connectivity.ComponentDNS)
			if err := store.Append(second); err != nil {
				t.Fatalf("append: %v", err)
			}
			corrupt(t, root, second.ID, test.mutate)

			restarted, err := Open(root, Options{})
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			resume, err := restarted.Resume()
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if resume.Status != ResumeAncestor {
				t.Fatalf("status %s, want recovered_ancestor (%s)", resume.Status, resume)
			}
			if resume.Checkpoint.ID != first.ID {
				t.Fatalf("recovered %s, want %s", resume.Checkpoint.ID, first.ID)
			}
		})
	}
}

func TestMissingAncestorIsNotPapedOver(t *testing.T) {
	store, root := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}
	second := chain.next(connectivity.ComponentDNS)
	if err := store.Append(second); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Remove the parent record without touching the index.
	if err := os.Remove(filepath.Join(root, checkpointDir, first.ID+".json")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	restarted, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Usable() {
		t.Fatalf("a broken chain was resumed: %s", resume)
	}
	if resume.Reason != ResumeReasonParentBroken && resume.Reason != ResumeReasonRecordMissing {
		t.Fatalf("reason %s, want a lineage failure", resume.Reason)
	}
}

func TestRecoveryDepthIsBoundedAndReported(t *testing.T) {
	store, root := openStore(t, Options{MaxRecoveryDepth: 2})
	chain := newLineage(t)
	written := make([]Checkpoint, 0, 6)
	for step := 0; step < 6; step++ {
		checkpoint := chain.next(connectivity.ComponentDNS)
		if err := store.Append(checkpoint); err != nil {
			t.Fatalf("append: %v", err)
		}
		written = append(written, checkpoint)
	}
	// Corrupt more checkpoints than the search is allowed to walk past.
	for _, checkpoint := range written[3:] {
		corrupt(t, root, checkpoint.ID, func(record map[string]any) {
			record["snapshot_generation"] = 99
		})
	}
	restarted, err := Open(root, Options{MaxRecoveryDepth: 2})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Usable() {
		t.Fatalf("the search walked past its bound: %s", resume)
	}
	if resume.Reason != ResumeReasonDepthExhausted {
		t.Fatalf("reason %s, want recovery_depth_exhausted", resume.Reason)
	}
}

// Losing older lineage is a bounded, expected condition. It must be visible,
// and it must not turn a missing ancestor into a broken chain.
func TestLineageOverflowIsVisible(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	for step := 0; step < MaxIndexEntries+5; step++ {
		checkpoint := chain.next(connectivity.ComponentDNS)
		if err := store.Append(checkpoint); err != nil {
			t.Fatalf("append %d: %v", step, err)
		}
	}
	pointer, err := store.Pointer()
	if err != nil {
		t.Fatalf("pointer: %v", err)
	}
	if !pointer.Overflow {
		t.Fatal("lineage was evicted without recording the loss")
	}
	entries, err := store.Index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(entries) > MaxIndexEntries {
		t.Fatalf("index holds %d entries, bound is %d", len(entries), MaxIndexEntries)
	}
	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resume.LineageOverflow {
		t.Fatal("resume did not carry the overflow forward")
	}
	if !resume.Usable() {
		t.Fatalf("a bounded eviction made the store unusable: %s", resume)
	}
}

func TestPointerLossFallsBackToTheRetainedLineage(t *testing.T) {
	store, root := openStore(t, Options{})
	chain := newLineage(t)
	last := chain.next(connectivity.ComponentDNS)
	if err := store.Append(last); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.Remove(filepath.Join(root, pointerName)); err != nil {
		t.Fatalf("remove pointer: %v", err)
	}
	restarted, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resume.Usable() || resume.Checkpoint.ID != last.ID {
		t.Fatalf("resume %s, want the retained lineage", resume)
	}
	if resume.Reason != ResumeReasonNoPointer {
		t.Fatalf("reason %s, want no_pointer", resume.Reason)
	}
}

func TestSealRefusesASnapshotItDoesNotHold(t *testing.T) {
	chain := newLineage(t)
	checkpoint := chain.next(connectivity.ComponentDNS)
	checkpoint.SnapshotDigest = "33333333333333333333333333333333" +
		"33333333333333333333333333333333"
	if err := checkpoint.Validate(); !errors.Is(err, ErrInvalidCheckpoint) {
		t.Fatalf("got %v, want %v", err, ErrInvalidCheckpoint)
	}
}

// The mirrored watermarks exist for readers who do not decode the snapshot.
// They may not say something the snapshot does not.
func TestWatermarksCannotDisagreeWithTheSnapshot(t *testing.T) {
	chain := newLineage(t)
	checkpoint := chain.next(connectivity.ComponentDNS)
	if len(checkpoint.SourceWatermarks) == 0 {
		t.Skip("no watermarks in this fixture")
	}
	altered := checkpoint
	altered.SourceWatermarks = append(
		[]connectivityreduce.SourceWatermark(nil), checkpoint.SourceWatermarks...)
	altered.SourceWatermarks[0].LastSequence += 5
	if err := altered.Validate(); !errors.Is(err, ErrInvalidCheckpoint) {
		t.Fatalf("got %v, want %v", err, ErrInvalidCheckpoint)
	}
}

// reseal rewrites a stored checkpoint so that it stays self-consistent — its
// own digest matches its own content — while no longer agreeing with what the
// lineage says about it. Corrupting a field in place is caught by the record's
// self-address long before the cross-checks are reached, so it cannot test
// them.
func reseal(t *testing.T, root, id string, mutate func(*Checkpoint)) Checkpoint {
	t.Helper()
	path := filepath.Join(root, checkpointDir, id+".json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(content, &checkpoint); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mutate(&checkpoint)
	sealed, err := Seal(checkpoint)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return sealed
}

// A record that verifies on its own terms but is not the record the lineage
// names is a substitution, and the index is what catches it.
func TestRecordThatDisagreesWithTheIndexIsRefused(t *testing.T) {
	store, root := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}
	second := chain.next(connectivity.ComponentDNS)
	if err := store.Append(second); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Same identity, self-consistent, different consumed watermark.
	resealed := reseal(t, root, second.ID, func(checkpoint *Checkpoint) {
		checkpoint.ConsumedTo++
		checkpoint.Snapshot.ConsumedHostSequence++
		digest, err := checkpoint.Snapshot.Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		checkpoint.SnapshotDigest = digest
	})
	if err := resealed.Validate(); err != nil {
		t.Fatalf("the substituted record is not self-consistent: %v", err)
	}

	restarted, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Status != ResumeAncestor || resume.Checkpoint.ID != first.ID {
		t.Fatalf("a substituted record was resumed: %s", resume)
	}
}

// A parent that verifies on its own terms but is not the parent the child was
// sealed against breaks the chain, and only the parent-digest link catches it.
func TestSubstitutedAncestorBreaksTheChain(t *testing.T) {
	store, root := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}
	second := chain.next(connectivity.ComponentDNS)
	if err := store.Append(second); err != nil {
		t.Fatalf("append: %v", err)
	}
	third := chain.next(connectivity.ComponentDNS)
	if err := store.Append(third); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Rewrite the middle record so it is valid and indexed correctly, but is
	// no longer the content its child bound itself to.
	reseal(t, root, second.ID, func(checkpoint *Checkpoint) {
		checkpoint.PriorSnapshotDigest = "44444444444444444444444444444444" +
			"44444444444444444444444444444444"
	})
	// Keep the index agreeing with the rewritten record so the only broken
	// thing is the child's parent binding.
	indexPath := filepath.Join(root, indexDir)
	listing, err := os.ReadDir(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(root, checkpointDir, second.ID+".json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rewritten Checkpoint
	if err := json.Unmarshal(updated, &rewritten); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, item := range listing {
		path := filepath.Join(indexPath, item.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read entry: %v", err)
		}
		var entry IndexEntry
		if err := json.Unmarshal(content, &entry); err != nil {
			t.Fatalf("unmarshal entry: %v", err)
		}
		if entry.ID != second.ID {
			continue
		}
		entry.Digest = rewritten.Digest
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}

	restarted, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	// The newest checkpoint is bound to a parent that no longer exists as
	// sealed, so it cannot be proven and the search must move past it.
	if resume.Checkpoint != nil && resume.Checkpoint.ID == third.ID {
		t.Fatalf("a checkpoint with a substituted ancestor was resumed: %s", resume)
	}
}

// Falling back to an ancestor without saying why leaves an operator told only
// that the newest checkpoint was not used. A shredded record, a deleted
// ancestor and a substituted parent call for three different responses, and
// the resume was reporting all three as "none".
func TestRecoveringAnAncestorSaysWhyTheNewestWasRefused(t *testing.T) {
	store, root := openStore(t, Options{})
	chain := newLineage(t)
	var newest Checkpoint
	for step := 0; step < 3; step++ {
		newest = chain.next(connectivity.ComponentDNS)
		if err := store.Append(newest); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	path := filepath.Join(root, "checkpoints", newest.ID+".json")
	if err := os.WriteFile(path, []byte("{\"schema\":\"truncated\""), 0o600); err != nil {
		t.Fatalf("shred: %v", err)
	}

	restarted, err := Open(root, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resume, err := restarted.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Status != ResumeAncestor {
		t.Fatalf("status %s, want recovered_ancestor", resume.Status)
	}
	if resume.Reason != ResumeReasonRecordInvalid {
		t.Fatalf("recovered an ancestor citing %q; the record that was refused "+
			"was unreadable, and nothing said so", resume.Reason)
	}
}

// The guard was right to refuse a parentless record onto an existing lineage.
// Nothing here loosens that: what changes is that a restart can now be
// recorded, and an unrecorded one is refused exactly as before.
func TestASilentRestartIsStillRefused(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	if err := store.Append(chain.next(connectivity.ComponentDNS)); err != nil {
		t.Fatalf("append: %v", err)
	}
	fresh := newLineage(t)
	orphan := fresh.next(connectivity.ComponentDNS)
	if orphan.Parent != "" {
		t.Fatal("the orphan is not parentless, so this test proves nothing")
	}
	if err := store.Append(orphan); !errors.Is(err, ErrGenerationGuard) {
		t.Fatalf("a parentless checkpoint with no break was accepted: %v", err)
	}
}

func TestARecordedRestartIsAcceptedAndSaysWhatItAbandoned(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}

	fresh := newLineage(t)
	restarted := fresh.nextBroken(&LineageBreak{
		AfterID: first.ID, Reason: ResumeReasonRecordInvalid,
	})
	if err := store.Append(restarted); err != nil {
		t.Fatalf("a recorded restart was refused: %v", err)
	}
	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resume.Status != ResumeLatest || resume.Checkpoint.ID != restarted.ID {
		t.Fatalf("the new lineage was not resumed from: %s", resume)
	}
	if resume.Checkpoint.Break == nil ||
		resume.Checkpoint.Break.AfterID != first.ID {
		t.Fatal("the restart does not name what it abandoned")
	}
}

func TestARestartMustNameTheLineageItActuallyAbandons(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	if err := store.Append(chain.next(connectivity.ComponentDNS)); err != nil {
		t.Fatalf("append: %v", err)
	}
	fresh := newLineage(t)
	// A break naming some other checkpoint would let a restart be moved onto
	// a lineage it never saw.
	wrong := fresh.nextBroken(&LineageBreak{
		AfterID: "11111111-2222-4333-8444-555555555555",
		Reason:  ResumeReasonRecordInvalid,
	})
	if err := store.Append(wrong); !errors.Is(err, ErrGenerationGuard) {
		t.Fatalf("a break naming another lineage was accepted: %v", err)
	}
}

func TestABreakWithoutAReasonIsNotARecord(t *testing.T) {
	chain := newLineage(t)
	if _, err := chain.sealBroken(&LineageBreak{
		AfterID: "11111111-2222-4333-8444-555555555555",
		Reason:  ResumeReasonNone,
	}); err == nil {
		t.Fatal("a lineage was abandoned for no stated reason")
	}
}

func TestTheFirstCheckpointCannotAbandonALineage(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	orphan := chain.nextBroken(&LineageBreak{
		AfterID: "11111111-2222-4333-8444-555555555555",
		Reason:  ResumeReasonRecordInvalid,
	})
	if err := store.Append(orphan); !errors.Is(err, ErrGenerationGuard) {
		t.Fatalf("the first checkpoint abandoned a lineage that was never there: %v", err)
	}
}
