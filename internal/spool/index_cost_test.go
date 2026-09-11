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
	if len(held) != len(fresh) {
		t.Fatalf("the spool believed it held %d records and holds %d",
			len(held), len(fresh))
	}
	for i := range held {
		if held[i] != fresh[i] {
			t.Fatalf("record %d: believed %+v, holds %+v", i, held[i], fresh[i])
		}
	}
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
	if len(held) != len(fresh) {
		t.Fatalf("after eviction the spool believed it held %d records "+
			"and holds %d", len(held), len(fresh))
	}
	for i := range held {
		if held[i] != fresh[i] {
			t.Fatalf("record %d: believed %+v, holds %+v", i, held[i], fresh[i])
		}
	}
	if len(fresh) == 200 {
		t.Fatal("nothing was evicted, so this proves nothing about eviction")
	}
}
