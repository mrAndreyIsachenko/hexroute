package eventarchive

import (
	"testing"
	"time"
)

// Appending must not cost a directory walk.
//
// Reading the directory is O(records stored). An append needs to know what is
// stored, so taking that reading per append made the cost O(stored x appended),
// and a runtime that appends eleven records per cycle paid for eleven walks of
// everything it had ever kept.
//
// On 2026-09-11 the root runtime's archive held 69,000 records and its
// connectivity store 107,600. A profile of the live process, taken while its
// operator loop was unresponsive, put the time in `os.ReadDir` and in the sort
// inside it, with the garbage collector behind them clearing the entries. The
// loop was blocked six to thirteen seconds at a time, once a minute, against a
// fifteen-second deadline on the socket it was failing to answer.
//
// The assertion counts walks rather than seconds. A stopwatch would say the cost
// fell, and only by how much on this machine today.
func TestAppendingDoesNotWalkTheDirectory(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   30 * 24 * time.Hour,
	})

	const appends = 200
	for index := range appends {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}

	if archive.walks > 1 {
		t.Fatalf("%d appends took %d directory walks, want at most 1",
			appends, archive.walks)
	}

	// The listing it kept has to be the listing that is there, or the saving is
	// bought with a wrong answer.
	held, err := archive.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	archive.forgetIndex()
	fresh, err := archive.index()
	if err != nil {
		t.Fatalf("index after forgetting: %v", err)
	}
	if len(held) != len(fresh) {
		t.Fatalf("the archive believed it held %d records and holds %d",
			len(held), len(fresh))
	}
	for i := range held {
		if held[i] != fresh[i] {
			t.Fatalf("record %d: believed %+v, holds %+v", i, held[i], fresh[i])
		}
	}
}

// What it learned has to survive eviction too, and eviction is the case where
// it would be easiest to keep a record that is no longer there.
func TestWhatTheArchiveKeepsSurvivesEviction(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{
		// The clock steps a second per reading, so a short window makes the
		// age walk evict as the appends go on. Eviction by age is the path the
		// runtime actually takes.
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   60 * time.Second,
	})

	for index := range 200 {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}

	held, err := archive.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	archive.forgetIndex()
	fresh, err := archive.index()
	if err != nil {
		t.Fatalf("index after forgetting: %v", err)
	}
	if len(held) != len(fresh) {
		t.Fatalf("after eviction the archive believed it held %d records "+
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
