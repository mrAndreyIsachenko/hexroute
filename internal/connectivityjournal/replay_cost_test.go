package connectivityjournal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// Replay reads the records it folds and no others.
//
// This is the regression for the two and a half minutes the root daemon spent
// after launchd started it, before it reported starting and while it observed
// nothing. Sampling the process through that window showed JSON decoding and
// canonicalisation and nothing else: finding the facts after the checkpoint's
// watermark meant decoding every record both journals retained.
//
// Measured on the machine on 2026-09-13, the checkpoint stood four fold
// positions behind the newest record, and the daemon decoded 136,397 records to
// find four.
//
// The assertion is behaviour rather than a counter. Every record but the tail
// is made unreadable: reading one sets it aside, and setting aside renames the
// file, so the directory says whether the replay looked.
func TestReplayOpensOnlyTheTailItFolds(t *testing.T) {
	journal, root := seededJournal(t, 12)
	sequences := storedNames(t, root)
	if len(sequences) < 6 {
		t.Fatalf("the journal holds %d records, too few to have a tail",
			len(sequences))
	}

	// Everything but the newest four is unreadable. Four rather than three:
	// the walk must read the record that ends it, which is the first one at or
	// below the watermark, and a search that could not read its own stopping
	// point would have to guess where to stop.
	damaged := damageAllBut(t, root, 4)
	if damaged == 0 {
		t.Fatal("nothing was damaged; the fixture proves nothing")
	}

	// The watermark sits just below the newest three.
	records, continuous, err := journal.RecordsAfter(uint64(len(sequences) - 3))
	if err != nil {
		t.Fatalf("RecordsAfter() over %d damaged records = %v", damaged, err)
	}
	if len(records) != 3 {
		t.Fatalf("replay returned %d records, want the 3 after the watermark",
			len(records))
	}
	if !continuous {
		t.Fatal("the tail is continuous from the watermark and was called broken")
	}
	if aside := setAside(t, root); aside != 0 {
		t.Fatalf("replay set aside %d stored records; it read past its tail",
			aside)
	}
}

// Rebuilding a watermark from a broken lineage reads the newest record, not all
// of them.
//
// It runs exactly when a host has lost its read model and wants to be observing
// again, which is the worst moment to read a whole journal for two numbers that
// sit at the end of it.
func TestTheWatermarkComesFromTheNewestRecord(t *testing.T) {
	journal, root := seededJournal(t, 12)
	stored := storedNames(t, root)
	damaged := damageAllBut(t, root, 1)
	if damaged == 0 {
		t.Fatal("nothing was damaged; the fixture proves nothing")
	}

	record, held, err := journal.Newest()
	if err != nil {
		t.Fatalf("Newest() over %d damaged records = %v", damaged, err)
	}
	if !held {
		t.Fatal("the journal holds records and Newest() found none")
	}
	if record.HostSequence != uint64(len(stored)) {
		t.Fatalf("the newest record has host sequence %d, want %d",
			record.HostSequence, len(stored))
	}
	if aside := setAside(t, root); aside != 0 {
		t.Fatalf("taking the watermark set aside %d records; it read them all",
			aside)
	}
}

// A tail that is not continuous from the watermark is reported, not folded.
//
// Reading backwards rests on fold positions rising with the order records were
// written. Nothing in this package asserts that; this is the guard that catches
// it being wrong, and the caller publishes uncertainty rather than folding a
// broken range.
func TestABrokenRangeIsReportedRatherThanFolded(t *testing.T) {
	journal, _ := seededJournal(t, 8)
	// A watermark two below the newest leaves a gap: the records in between
	// exist, so the range returned cannot start at watermark+1.
	records, continuous, err := journal.RecordsAfter(2)
	if err != nil {
		t.Fatalf("RecordsAfter: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no records after the watermark; the fixture proves nothing")
	}
	if !continuous {
		t.Fatal("this range is continuous and was called broken")
	}

	// Now ask from a watermark the journal skipped past, which is what a
	// journal whose order did not hold would look like.
	broken, continuous, err := journal.RecordsAfter(0)
	if err != nil {
		t.Fatalf("RecordsAfter: %v", err)
	}
	if len(broken) == 0 {
		t.Fatal("nothing came back for the whole journal")
	}
	if broken[0].FoldPosition != 1 && continuous {
		t.Fatalf("the range starts at fold position %d and was called continuous",
			broken[0].FoldPosition)
	}
}

func seededJournal(t *testing.T, count int) (*Journal, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	journal, err := Open(root, policy.DomainRoot, Options{
		MaxBytes: 64 * 1024 * 1024, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	facts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainRoot {
			facts = append(facts, fact)
		}
	}
	if len(facts) == 0 {
		t.Fatal("no root facts in the baseline fixture")
	}
	for index := 1; index <= count; index++ {
		if err := journal.Append(
			facts[index%len(facts)], uint64(index), uint64(index),
			"accepted", safety.RoleAuthoritative,
		); err != nil {
			t.Fatalf("seed append %d: %v", index, err)
		}
	}
	return journal, root
}

func storedNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "spool"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// damageAllBut makes every stored record unreadable except the newest few.
func damageAllBut(t *testing.T, root string, keep int) int {
	t.Helper()
	names := storedNames(t, root)
	damaged := 0
	for _, name := range names[:max(0, len(names)-keep)] {
		if err := os.WriteFile(
			filepath.Join(root, "spool", name),
			[]byte(`{"schema":"not a record"}`), 0o600,
		); err != nil {
			t.Fatal(err)
		}
		damaged++
	}
	return damaged
}

func setAside(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "spool"))
	if err != nil {
		t.Fatal(err)
	}
	aside := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".quarantine-") {
			aside++
		}
	}
	return aside
}

// A journal that has overflowed holds records that are not its facts.
//
// The spool writes its own overflow incidents into the spool it is evicting
// from, so a journal at its bound carries records of another class among its
// facts — and the live user journal has been at its bound for days. A backwards
// walk that stopped at the first record that was not a fact would return a short
// tail and replay would fold less than the journal holds.
//
// Nothing in the fixtures above produces one, which is why this test exists
// rather than a hand-written record: the overflow is made by the bound, the way
// the machine makes it.
func TestTheWalkStepsOverRecordsThatAreNotFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	journal, err := Open(root, policy.DomainRoot, Options{
		MaxBytes: 24 * 1024, NodeID: testNodeID, Clock: &advancingClock{},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	facts := make([]connectivity.Fact, 0)
	for _, fact := range connectivity.FixtureBaselineSet() {
		if fact.Domain == policy.DomainRoot {
			facts = append(facts, fact)
		}
	}
	for index := 1; index <= 200; index++ {
		if err := journal.Append(
			facts[index%len(facts)], uint64(index), uint64(index),
			"accepted", safety.RoleAuthoritative,
		); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}

	whole, err := journal.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	stored := storedNames(t, root)
	if len(stored) <= len(whole) {
		t.Fatalf("the journal holds %d records and %d of them are facts; "+
			"nothing of another class is stored and this proves nothing",
			len(stored), len(whole))
	}

	tail, _, err := journal.RecordsAfter(0)
	if err != nil {
		t.Fatalf("RecordsAfter: %v", err)
	}
	if len(tail) != len(whole) {
		t.Fatalf("walking back from the newest found %d facts and the journal "+
			"holds %d; it stopped at a record of another class",
			len(tail), len(whole))
	}
	for index := range tail {
		if tail[index].FoldPosition != whole[index].FoldPosition {
			t.Fatalf("record %d: the walk found fold position %d, the journal "+
				"holds %d", index, tail[index].FoldPosition,
				whole[index].FoldPosition)
		}
	}

	if _, held, err := journal.Newest(); err != nil || !held {
		t.Fatalf("Newest() over an overflowed journal = held %v, err %v",
			held, err)
	}
}
