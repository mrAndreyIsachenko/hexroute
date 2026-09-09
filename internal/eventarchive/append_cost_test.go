package eventarchive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// An append must not pay for records it is not using.
//
// Archive.scan read and decoded every stored record, and Append called it
// unconditionally. At the 41,492 records the live root archive reached that was
// about 900ms a scan, and the connectivity journal mirrors every fact into the
// archive synchronously — so a published fact paid it twice. The root daemon
// burned 23.3 CPU-seconds in every 30 of wall time and could not answer a
// publication inside the five seconds its caller allowed, for about a day.
//
// The assertion is behavioural rather than timed. A stopwatch would say the cost
// fell, but only by how much on this machine today. So the stored records above
// the oldest are made undecodable: an append that reads them cannot stay silent,
// and an append that does not read them cannot tell they are there.
func TestAppendDoesNotReadTheRecordsItIsNotUsing(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	archive := openArchive(t, root, clock, Options{
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   30 * 24 * time.Hour,
	})

	for index := range 200 {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}

	// Everything except the oldest is made unreadable. The oldest is what the
	// age bound is established from, and reading exactly that one is the
	// design; reading any of the others is the defect.
	stored := storedSequences(t, root)
	if len(stored) < 2 {
		t.Fatalf("expected a seeded archive, found %d records", len(stored))
	}
	for _, sequence := range stored[1:] {
		corruptRecord(t, root, sequence)
	}

	if _, err := archive.Append(operational(1000)); err != nil {
		t.Fatalf("Append() over undecodable stored records = %v", err)
	}

	// Nothing was evicted, so nothing needed to be read to choose a victim.
	after := storedSequences(t, root)
	if len(after) != len(stored)+1 {
		t.Fatalf("archive holds %d records after the append, want %d",
			len(after), len(stored)+1)
	}
}

// The age walk reads the expired records and the first one that is not.
//
// It must not read past that. Records above the stop point are made undecodable
// here: the walk is correct only if it never touches them, and the eviction set
// proves it stopped in the right place.
//
// The aged records are written to the directory rather than appended, because
// appending is what runs the walk — a record cannot be made to expire through
// the same call that would immediately evict it.
func TestTheAgeWalkStopsAtTheFirstRetainedRecord(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	archive := openArchive(t, root, clock, Options{
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   time.Hour,
	})

	// One real record supplies the shape; the rest are that shape restamped.
	if _, err := archive.Append(operational(0)); err != nil {
		t.Fatalf("template: %v", err)
	}
	templateSequence := storedSequences(t, root)[0]
	template := readRaw(t, root, templateSequence)
	// It is removed so the planted records are the oldest: the walk starts at
	// the lowest sequence, and a fresh record below them would stop it there.
	if err := os.Remove(
		filepath.Join(root, name(templateSequence)+stableSuffix),
	); err != nil {
		t.Fatalf("remove template: %v", err)
	}
	now := clock.WallNow().UTC()

	const aged = 5
	const fresh = 40
	expired := []uint64{}
	retained := []uint64{}
	for index := 1; index <= aged; index++ {
		sequence := uint64(100 + index)
		plantRecord(t, root, template, sequence, now.Add(-4*time.Hour))
		expired = append(expired, sequence)
	}
	for index := 1; index <= fresh; index++ {
		sequence := uint64(200 + index)
		plantRecord(t, root, template, sequence, now.Add(-time.Minute))
		retained = append(retained, sequence)
	}

	// One record above the stop point is decodable and outside the window. A
	// walk that stops where it should never sees it and leaves it alone; a walk
	// that keeps reading evicts it. That is the difference between the two, and
	// nothing else in the eviction set shows it.
	const strandedSequence = uint64(300)
	plantRecord(t, root, template, strandedSequence, now.Add(-4*time.Hour))
	retained = append(retained, strandedSequence)

	// Everything else above the stop point is made undecodable, so reading it
	// cannot be mistaken for reading nothing.
	for _, sequence := range retained[1 : len(retained)-1] {
		corruptRecord(t, root, sequence)
	}

	if _, err := archive.Append(operational(9000)); err != nil {
		t.Fatalf("Append() while records were expiring = %v", err)
	}

	after := storedSequences(t, root)
	for _, sequence := range expired {
		if contains(after, sequence) {
			t.Fatalf("record %d was outside the window and was not evicted", sequence)
		}
	}
	for _, sequence := range retained {
		if contains(after, sequence) {
			continue
		}
		if sequence == strandedSequence {
			t.Fatal("the walk read past the first record inside the window; " +
				"it evicted one it should never have reached")
		}
		t.Fatalf("record %d was inside the window and was evicted", sequence)
	}
}

// A record that cannot be decoded must not cost the observation being recorded.
//
// The archive keeps no set-aside for stored records, so the walk cannot prove
// this one's age and must not conclude anything from that: it neither treats it
// as expired nor lets it stop the walk.
func TestADamagedRecordDoesNotFailTheAppend(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	archive := openArchive(t, root, clock, Options{
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   time.Hour,
	})

	for index := range 10 {
		if _, err := archive.Append(operational(index)); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}
	stored := storedSequences(t, root)
	// The oldest record is the one the age bound is read from. Damaging it is
	// the case that would otherwise stop every future append.
	corruptRecord(t, root, stored[0])

	if _, err := archive.Append(operational(500)); err != nil {
		t.Fatalf("Append() with an undecodable oldest record = %v", err)
	}
	if !contains(storedSequences(t, root), stored[0]) {
		t.Fatal("the damaged record was removed; it is the only evidence of what happened")
	}
}

func storedSequences(t *testing.T, root string) []uint64 {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sequences := []uint64{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), stableSuffix) {
			continue
		}
		trimmed := strings.TrimSuffix(entry.Name(), stableSuffix)
		sequence, err := strconv.ParseUint(strings.TrimLeft(trimmed, "0"), 10, 64)
		if err != nil {
			t.Fatalf("unexpected record name %q: %v", entry.Name(), err)
		}
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(one, other int) bool {
		return sequences[one] < sequences[other]
	})
	return sequences
}

// readRaw returns one stored record's bytes, to be restamped into others.
func readRaw(t *testing.T, root string, sequence uint64) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, name(sequence)+stableSuffix))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	return decoded
}

// plantRecord writes a valid stored record with a chosen sequence and stamp.
func plantRecord(
	t *testing.T,
	root string,
	template map[string]any,
	sequence uint64,
	wallClock time.Time,
) {
	t.Helper()
	record := map[string]any{}
	for key, value := range template {
		record[key] = value
	}
	record["sequence"] = sequence
	meta := map[string]any{}
	for key, value := range template["metadata"].(map[string]any) {
		meta[key] = value
	}
	meta["wall_clock"] = wallClock.UTC().Format(time.RFC3339Nano)
	meta["sequence"] = sequence
	record["metadata"] = meta

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode planted record: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, name(sequence)+stableSuffix), encoded, 0o600,
	); err != nil {
		t.Fatalf("plant %d: %v", sequence, err)
	}
}

func corruptRecord(t *testing.T, root string, sequence uint64) {
	t.Helper()
	path := filepath.Join(root, name(sequence)+stableSuffix)
	if err := os.WriteFile(path, []byte(`{"schema":"not a record at all"}`), 0o600); err != nil {
		t.Fatalf("corrupt %d: %v", sequence, err)
	}
}

func contains(sequences []uint64, wanted uint64) bool {
	for _, sequence := range sequences {
		if sequence == wanted {
			return true
		}
	}
	return false
}
