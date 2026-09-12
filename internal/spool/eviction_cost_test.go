package spool

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A spool at its bound must pay for what it evicts, not for what it holds.
//
// This is the regression for a live stall. On 2026-09-12 the root daemon came
// up at 17:32:58, wrote its last heartbeat at 17:35:54, and four hours later
// was still running with a core busy in JSON canonicalisation under
// spool.scanStable, reached from spool.Append. The user journal's spool stood
// at 104,858,244 bytes against a bound of 104,857,600, holding 84,067 records,
// and every append decoded all of them.
//
// Nothing in the observing runtime acknowledges an upload, so the spool never
// drains: once full it is full for good, and the expensive path is not an edge
// case but the only case.
//
// The assertion counts records opened rather than seconds. A stopwatch would
// say the cost fell, and only by how much on this machine today.
func TestAFullSpoolStopsOpeningItsRecords(t *testing.T) {
	store, path := fullSpool(t)

	reopened, err := Open(path, OwnerRoot, testOptions(evictionBound))
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	stored, err := reopened.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(stored) < 2 {
		t.Fatalf("the spool holds %d records, which proves nothing about "+
			"cost that grows with how many it holds", len(stored))
	}
	_ = store

	const appends = 8
	for index := 1; index <= appends; index++ {
		if _, err := reopened.Append(costEvent(t, 10_000+index)); err != nil {
			t.Fatalf("append %d to a full spool: %v", index, err)
		}
	}

	// Classifying what was already on disk is allowed to open each record once.
	// Anything beyond that is a cost paid per append.
	if opened := reopened.opens; opened > len(stored) {
		t.Fatalf("%d appends to a full spool opened %d records, want at most "+
			"%d — one per record already stored", appends, opened, len(stored))
	}
}

// The second append must open nothing at all.
//
// The first may classify; after that the spool knows every class it needs, and
// a record it published itself was never opened even once.
func TestAppendingAgainToAFullSpoolOpensNothing(t *testing.T) {
	store, path := fullSpool(t)
	reopened, err := Open(path, OwnerRoot, testOptions(evictionBound))
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	_ = store

	if _, err := reopened.Append(costEvent(t, 20_001)); err != nil {
		t.Fatalf("first append to a full spool: %v", err)
	}
	settled := reopened.opens
	if _, err := reopened.Append(costEvent(t, 20_002)); err != nil {
		t.Fatalf("second append to a full spool: %v", err)
	}
	if opened := reopened.opens - settled; opened != 0 {
		t.Fatalf("a second append to a full spool opened %d records, want 0",
			opened)
	}
}

// Eviction does not use the event a record carries, so it must not prove it.
//
// Every stored record here has an intact envelope and an event that cannot be
// decoded. The path that decoded them would set every one aside and then have
// nothing left to evict, so the append would fail on damage it had no reason to
// look at.
func TestAFullSpoolDoesNotDecodeTheEventsItEvicts(t *testing.T) {
	store, path := fullSpool(t)
	_ = store
	spoiled := spoilStoredEvents(t, path)
	if spoiled < 2 {
		t.Fatalf("spoiled %d stored events, which proves nothing", spoiled)
	}

	reopened, err := Open(path, OwnerRoot, testOptions(evictionBound))
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	if _, err := reopened.Append(costEvent(t, 30_001)); err != nil {
		t.Fatalf("Append() to a full spool of undecodable events = %v", err)
	}
	if aside := countQuarantined(t, path); aside != 0 {
		t.Fatalf("appending set aside %d stored records; eviction proved "+
			"events it does not use", aside)
	}
}

// A record eviction cannot classify is set aside, and the append still lands.
//
// The class is the one fact eviction needs that the directory cannot report, so
// a record whose envelope is unreadable cannot be placed. Refusing the append
// would lose a new observation to an old damaged one.
func TestAnUnclassifiableRecordIsSetAsideAndTheAppendLands(t *testing.T) {
	store, path := fullSpool(t)
	_ = store

	names := stableNames(t, path)
	if len(names) < 3 {
		t.Fatalf("the spool holds %d records, too few to damage one", len(names))
	}
	damaged := damageRecord(t, filepath.Join(path, names[0]))

	reopened, err := Open(path, OwnerRoot, testOptions(evictionBound))
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	if _, err := reopened.Append(costEvent(t, 40_001)); err != nil {
		t.Fatalf("Append() with one unclassifiable record stored = %v", err)
	}
	if aside := countQuarantined(t, path); aside != 1 {
		t.Fatalf("%d records set aside, want exactly the one that could not "+
			"be classified", aside)
	}
	// Setting aside is not deleting: the damaged record is the only evidence of
	// what happened.
	if _, err := os.Stat(damaged); !os.IsNotExist(err) {
		t.Fatalf("the damaged record is still filed as a live one: %v", err)
	}
}

// damageRecord makes one stored record unreadable without making the spool
// smaller.
//
// The replacement is the same length as the original, because a shorter one
// would leave room under the bound and the append would have nothing to evict —
// which is not the case this test is about.
func damageRecord(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	opening := []byte(`{"schema":"not a record","pad":"`)
	closing := []byte(`"}`)
	padding := int(info.Size()) - len(opening) - len(closing)
	if padding < 0 {
		t.Fatalf("%s holds %d bytes, too few to damage in place", path, info.Size())
	}
	content := append(opening, bytes.Repeat([]byte("x"), padding)...)
	content = append(content, closing...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The bound is small so that filling it is quick and eviction is certain.
const evictionBound = int64(24 * 1024)

// fullSpool returns a spool that has reached its bound and the directory it
// occupies.
func fullSpool(t *testing.T) (*Spool, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spool")
	store, err := Open(path, OwnerRoot, testOptions(evictionBound))
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for index := 1; index <= 120; index++ {
		if _, err := store.Append(costEvent(t, index)); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	size, err := store.Size()
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if size <= evictionBound/2 {
		t.Fatalf("the spool holds %d bytes against a bound of %d and is not "+
			"full, so nothing here is about eviction", size, evictionBound)
	}
	return store, path
}

func stableNames(t *testing.T, path string) []string {
	t.Helper()
	directoryEntries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if _, ok := parseStableName(directoryEntry.Name()); ok {
			names = append(names, directoryEntry.Name())
		}
	}
	return names
}

// spoilStoredEvents leaves every envelope intact and every event undecodable.
//
// The event is replaced by a JSON document of the same length, so the record's
// size on disk is unchanged and nothing but decoding the event can tell.
func spoilStoredEvents(t *testing.T, path string) int {
	t.Helper()
	spoiled := 0
	for _, name := range stableNames(t, path) {
		full := filepath.Join(path, name)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		original, ok := wire["event"]
		if !ok {
			t.Fatalf("%s carries no event", name)
		}
		compact := bytes.NewBuffer(nil)
		if err := json.Compact(compact, original); err != nil {
			t.Fatal(err)
		}
		filler := bytes.Repeat([]byte("x"), compact.Len()-len(`{"":""}`))
		wire["event"] = append(append([]byte(`{"`), filler...), []byte(`":""}`)...)
		rewritten, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, rewritten, 0o600); err != nil {
			t.Fatal(err)
		}
		spoiled++
	}
	return spoiled
}
