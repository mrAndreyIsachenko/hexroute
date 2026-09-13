package eventarchive

import (
	"os"
	"path/filepath"
	"strings"
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

// Opening an archive reads its directory, not its records.
//
// Open needs one number, the highest sequence, and the filenames carry it. It
// used to take that maximum by reading and decoding every record, which cost a
// daemon about seventeen seconds of every start over forty-three thousand of
// them — while it observed nothing, because it had not finished opening.
//
// The assertion is behaviour rather than a counter: every stored record is made
// undecodable, and an open that read one would fail on it.
func TestOpeningReadsTheDirectoryRatherThanTheRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive")
	archive, err := Open(path, Options{NodeID: testNodeID})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for index := 1; index <= 40; index++ {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	names, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	damaged := 0
	for _, name := range names {
		if name.IsDir() || strings.HasPrefix(name.Name(), ".") {
			continue
		}
		if err := os.WriteFile(filepath.Join(path, name.Name()),
			[]byte(`{"schema":"not a record"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		damaged++
	}
	if damaged == 0 {
		t.Fatal("nothing was stored to damage; the fixture proves nothing")
	}

	reopened, err := Open(path, Options{NodeID: testNodeID})
	if err != nil {
		t.Fatalf("Open() over %d undecodable records = %v; opening reads them",
			damaged, err)
	}
	// And it still knows where the sequences got to, because the names say so.
	sequence, err := reopened.Append(operational(41))
	if err != nil {
		t.Fatalf("append after reopening: %v", err)
	}
	if sequence <= uint64(damaged) {
		t.Fatalf("the reopened archive issued sequence %d, at or below the %d "+
			"it already holds", sequence, damaged)
	}
}
