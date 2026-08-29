package connectivitycheckpoint

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// journalled turns the facts a lineage accepted after a checkpoint into the
// records a journal would have kept.
func journalled(t *testing.T, chain *lineage, from uint64) []connectivityjournal.Record {
	t.Helper()
	records := make([]connectivityjournal.Record, 0)
	for sequence := from; sequence <= chain.consumed; sequence++ {
		fact := chain.factsByHostSequence[sequence]
		digest, err := connectivity.Digest(fact)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		records = append(records, connectivityjournal.Record{
			HostSequence: sequence, Role: safety.RoleAuthoritative,
			Digest: digest, Fact: fact,
		})
	}
	return records
}

// Folding a retained journal onto a proven checkpoint must reproduce exactly
// what the original run produced.
func TestReplayReproducesTheOriginalReduction(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}
	before := chain.consumed

	second := chain.next(connectivity.ComponentRelays, connectivity.ComponentTransports)
	expectedSnapshot := second.SnapshotDigest
	expectedDiff := second.DiffDigest

	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	result, err := Replay(ReplayInput{
		Resume: resume, Records: journalled(t, chain, before+1), Continuous: true,
		Policy: testPolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: replayTick,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.Status != ReplayComplete {
		t.Fatalf("status %q, want complete", result.Status)
	}
	snapshotDigest, err := result.Output.Snapshot.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if snapshotDigest != expectedSnapshot {
		t.Fatal("replay produced a different snapshot")
	}
	diffDigest, err := result.Output.Diff.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if diffDigest != expectedDiff {
		t.Fatal("replay produced a different diff")
	}
}

func TestReplayRefusesADiscontinuousJournal(t *testing.T) {
	store, _ := openStore(t, Options{})
	chain := newLineage(t)
	first := chain.next(connectivity.ComponentDNS)
	if err := store.Append(first); err != nil {
		t.Fatalf("append: %v", err)
	}
	before := chain.consumed
	chain.next(connectivity.ComponentRelays, connectivity.ComponentTransports)

	resume, err := store.Resume()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	records := journalled(t, chain, before+1)
	if len(records) < 2 {
		t.Fatal("the fixture did not produce enough records")
	}

	// The journal itself reports the hole; replay must not fold over it.
	result, err := Replay(ReplayInput{
		Resume: resume, Records: records, Continuous: false,
		Policy: testPolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: replayTick,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.Status != ReplayJournalGap {
		t.Fatalf("status %q, want journal_gap", result.Status)
	}

	// Even when the journal believes it is continuous, a missing sequence in
	// the records themselves is caught.
	truncated := append([]connectivityjournal.Record(nil), records[0])
	truncated = append(truncated, records[len(records)-1])
	if truncated[1].HostSequence == truncated[0].HostSequence+1 {
		t.Skip("the fixture produced no removable middle record")
	}
	result, err = Replay(ReplayInput{
		Resume: resume, Records: truncated, Continuous: true,
		Policy: testPolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: replayTick,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.Status != ReplayJournalGap {
		t.Fatalf("status %q on a truncated journal, want journal_gap", result.Status)
	}
}

func TestReplayWithoutAProvableCheckpointProducesNothing(t *testing.T) {
	result, err := Replay(ReplayInput{
		Resume: Resume{Status: ResumeUnrecoverable, Reason: ResumeReasonDepthExhausted},
		Policy: testPolicy(), BootID: connectivity.FixtureBootID,
		EvaluationTick: replayTick,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.Status != ReplayUnavailable {
		t.Fatalf("status %q, want unavailable", result.Status)
	}
	if result.Output.Snapshot.Generation != 0 {
		t.Fatal("an unprovable lineage produced a snapshot anyway")
	}
}

// Replaying an older read model must never become a way to authorize an older
// policy. The simplest guarantee is that this package cannot reach the policy
// store at all.
func TestReplayCannotMoveThePolicyPointer(t *testing.T) {
	forbidden := map[string]struct{}{
		"github.com/mrAndreyIsachenko/hexroute/internal/policystore":     {},
		"github.com/mrAndreyIsachenko/hexroute/internal/policycontrol":   {},
		"github.com/mrAndreyIsachenko/hexroute/internal/policyinstaller": {},
	}
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, parsed := range packages {
		for name, file := range parsed.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if _, banned := forbidden[path]; banned {
					t.Fatalf("%s imports %q; read-model recovery must not touch policy",
						filepath.Base(name), path)
				}
			}
		}
	}
}

var _ = connectivityreduce.ReducerID
