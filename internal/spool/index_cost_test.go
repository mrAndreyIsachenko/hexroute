package spool

import (
	"path/filepath"
	"testing"
)

// Appending must not cost a directory listing.
//
// Listing is O(records stored) and an append needs the listing, so taking it
// per append made the cost O(stored x appended). The runtime appends eleven
// records per cycle, into a spool that had reached six figures of files inside
// the connectivity state directory.
//
// This is the sibling of the same defect in the event archive, and it was found
// the same way: a profile of the live root process, taken on 2026-09-11 while
// its operator loop had been unresponsive for six to thirteen seconds at a
// time, with `spool.parseStableName` and `spool.scanIndex` in the tree beside
// the archive's.
//
// The assertion counts listings rather than seconds. A stopwatch would say the
// cost fell, and only by how much on this machine today.
func TestAppendingDoesNotListTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	store, err := Open(path, OwnerRoot, testOptions(64*1024*1024))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	before := store.scans

	const appends = 200
	for index := 1; index <= appends; index++ {
		if _, err := store.Append(costEvent(t, index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}

	if taken := store.scans - before; taken > 1 {
		t.Fatalf("%d appends took %d directory listings, want at most 1",
			appends, taken)
	}

	// The listing it kept has to be the listing that is there, or the saving is
	// bought with a wrong answer.
	held, err := store.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	store.forgetIndex()
	fresh, err := store.index()
	if err != nil {
		t.Fatalf("index after forgetting: %v", err)
	}
	assertListingIsTrue(t, store, held, fresh)
}

// Eviction is where a kept listing would be easiest to leave wrong.
func TestWhatTheSpoolKeepsSurvivesEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	store, err := Open(path, OwnerRoot, testOptions(32*1024))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for index := 1; index <= 200; index++ {
		if _, err := store.Append(costEvent(t, index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	held, err := store.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	store.forgetIndex()
	fresh, err := store.index()
	if err != nil {
		t.Fatalf("index after forgetting: %v", err)
	}
	assertListingIsTrue(t, store, held, fresh)
	if len(fresh) == 200 {
		t.Fatal("nothing was evicted, so this proves nothing about eviction")
	}
}

// assertListingIsTrue checks what the spool believes about its directory
// against the directory.
//
// A fresh listing reports what the directory reports and no class: classifying
// costs a read, and a spool being listed has not been asked to evict. So the
// directory facts must agree exactly, and any class the spool believes — either
// read once or carried over from the append that wrote the record — must agree
// with what the record says about itself.
func assertListingIsTrue(t *testing.T, store *Spool, held, fresh []stableRecord) {
	t.Helper()
	if len(held) != len(fresh) {
		t.Fatalf("the spool believed it held %d records and holds %d",
			len(held), len(fresh))
	}
	for index := range held {
		if held[index].Sequence != fresh[index].Sequence ||
			held[index].Size != fresh[index].Size {
			t.Fatalf("record %d: believed %+v, holds %+v",
				index, held[index], fresh[index])
		}
		if held[index].Priority == "" {
			continue
		}
		stated, err := store.readPriority(held[index].Sequence)
		if err != nil {
			t.Fatalf("record %d: %v", index, err)
		}
		if held[index].Priority != stated {
			t.Fatalf("record %d: the spool believes it is %q and it says %q",
				index, held[index].Priority, stated)
		}
	}
}
