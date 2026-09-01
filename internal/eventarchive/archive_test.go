package eventarchive

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const testNodeID = metadata.UUID("44444444-4444-4444-8444-444444444444")

// steppingClock advances on every read so records are ordered in wall time,
// and can be pushed forward to make the age bound reachable without waiting.
type steppingClock struct {
	base time.Time
	step time.Duration
	tick int64
}

func newClock() *steppingClock {
	return &steppingClock{
		base: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
		step: time.Second,
	}
}

func (clock *steppingClock) WallNow() time.Time {
	clock.tick++
	return clock.base.Add(time.Duration(clock.tick) * clock.step)
}

func (clock *steppingClock) MonotonicNow() time.Duration {
	return time.Duration(clock.tick) * clock.step
}

func (clock *steppingClock) advance(by time.Duration) { clock.base = clock.base.Add(by) }

func openArchive(t *testing.T, root string, clock metadata.Clock, options Options) *Archive {
	t.Helper()
	options.NodeID = testNodeID
	options.Clock = clock
	archive, err := Open(root, options)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return archive
}

func diagnostic(count uint32) []byte {
	encoded, err := event.Encode(event.SchemaDiagnostic, event.Diagnostic{
		Component: control.ComponentRuntime,
		Code:      event.DiagnosticAdapterSampled,
		Count:     count,
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

func operational(index int) []byte {
	encoded, err := event.Encode(event.SchemaObservation, event.Observation{
		Component:           control.ComponentNetwork,
		Health:              control.HealthReady,
		Reason:              control.ReasonProbeSucceeded,
		ConsecutiveFailures: uint32(index % 250),
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

func critical(index int) []byte {
	encoded, err := event.Encode(event.SchemaIncident, event.Incident{
		IncidentID: fmt.Sprintf("archive-test-%d", index),
		Status:     event.IncidentOpened,
		Severity:   event.SeverityCritical,
		Category:   event.IncidentAvailability,
		Component:  control.ComponentRuntime,
		Generation: uint64(index) + 1,
	})
	if err != nil {
		panic(err)
	}
	return encoded
}

// 1.1 — the sequence is append-only, and a restart continues it rather than
// numbering from its own beginning.
func TestSequenceIsAppendOnlyAcrossRestarts(t *testing.T) {
	root := t.TempDir()
	clock := newClock()

	first := openArchive(t, root, clock, Options{})
	var written []uint64
	for index := 0; index < 4; index++ {
		sequence, err := first.Append(operational(index))
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		written = append(written, sequence)
	}

	second := openArchive(t, root, clock, Options{})
	for index := 0; index < 4; index++ {
		sequence, err := second.Append(operational(index))
		if err != nil {
			t.Fatalf("append after restart: %v", err)
		}
		written = append(written, sequence)
	}

	for index := 1; index < len(written); index++ {
		if written[index] <= written[index-1] {
			t.Fatalf("sequence went %d then %d; a restarted archive reused a position",
				written[index-1], written[index])
		}
	}
	records, err := second.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 8 {
		t.Fatalf("holds %d records, wrote 8", len(records))
	}
}

// 1.2 — only what a registered schema describes may enter, and a refusal is
// something the archive records rather than only something the caller saw.
func TestUnregisteredRecordsAreRefusedAndCounted(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{})

	if _, err := archive.Append([]byte(`{"schema":"not.a.schema"}`)); err == nil {
		t.Fatal("an unregistered record was accepted")
	}
	refusals := archiveDiagnostics(t, archive)
	if len(refusals) != 1 || refusals[0].Count != 1 {
		t.Fatalf("first refusal recorded %v, want one diagnostic counting 1", refusals)
	}

	// Bounded: a producer emitting malformed records must not be able to
	// evict the evidence by being noisy. Reports land at each doubling.
	for attempt := 0; attempt < 15; attempt++ {
		if _, err := archive.Append([]byte(`{"schema":"not.a.schema"}`)); err == nil {
			t.Fatal("an unregistered record was accepted")
		}
	}
	refusals = archiveDiagnostics(t, archive)
	var counts []uint32
	for _, refusal := range refusals {
		counts = append(counts, refusal.Count)
	}
	want := []uint32{1, 2, 4, 8, 16}
	if len(counts) != len(want) {
		t.Fatalf("16 refusals produced %v diagnostics, want %v", counts, want)
	}
	for index := range want {
		if counts[index] != want[index] {
			t.Fatalf("diagnostics counted %v, want %v", counts, want)
		}
	}
}

func archiveDiagnostics(t *testing.T, archive *Archive) []event.Diagnostic {
	t.Helper()
	records, err := archive.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	var found []event.Diagnostic
	for _, record := range records {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		payload, ok := decoded.Payload.(*event.Diagnostic)
		if !ok || payload.Code != event.DiagnosticArchiveRefusedRecord {
			continue
		}
		found = append(found, *payload)
	}
	return found
}

// 1.2 — a record read back is the event that was appended.
func TestARecordReadsBackAsWhatWasAppended(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{})
	offered := critical(7)
	if _, err := archive.Append(offered); err != nil {
		t.Fatalf("append: %v", err)
	}
	records, err := archive.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("holds %d records, wrote 1", len(records))
	}
	if string(records[0].Event) != string(offered) {
		t.Fatal("the archived record is not the event that was appended")
	}
	if records[0].Priority != event.PriorityCritical {
		t.Fatalf("priority %q, want critical", records[0].Priority)
	}
}

// 1.3 — age eviction applies regardless of priority.
func TestAgeEvictionIgnoresPriority(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	archive := openArchive(t, root, clock, Options{MaxAge: time.Hour})

	if _, err := archive.Append(critical(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := archive.Append(diagnostic(1)); err != nil {
		t.Fatalf("append: %v", err)
	}

	clock.advance(2 * time.Hour)
	if _, err := archive.Append(operational(1)); err != nil {
		t.Fatalf("append after the window moved: %v", err)
	}

	overflows := archiveOverflows(t, archive)
	dropped := map[event.Priority]uint32{}
	for _, overflow := range overflows {
		if overflow.Reason != event.ArchiveOverflowAge {
			continue
		}
		dropped[overflow.Dropped] += overflow.Count
	}
	if dropped[event.PriorityCritical] != 1 || dropped[event.PriorityDiagnostic] != 1 {
		t.Fatalf("age eviction dropped %v, want one of each priority", dropped)
	}
	// The window the archive still covers has to be readable, or an eviction
	// is indistinguishable from a quiet period.
	window, err := archive.Window()
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if window.Empty {
		t.Fatal("the archive reported itself empty while holding records")
	}
}

// 1.3 — size eviction removes the cheapest evidence first.
func TestSizeEvictionRemovesDiagnosticsBeforeOperational(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	sizing := openArchive(t, clock2Root(t), clock, Options{})
	if _, err := sizing.Append(diagnostic(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	measured, err := sizing.Size()
	if err != nil {
		t.Fatalf("size: %v", err)
	}

	// Room for three records and no more, so the fourth must evict one.
	archive := openArchive(t, root, clock, Options{MaxBytes: measured * 7 / 2})
	if _, err := archive.Append(diagnostic(1)); err != nil {
		t.Fatalf("append diagnostic: %v", err)
	}
	if _, err := archive.Append(operational(1)); err != nil {
		t.Fatalf("append operational: %v", err)
	}
	if _, err := archive.Append(operational(2)); err != nil {
		t.Fatalf("append operational: %v", err)
	}
	if _, err := archive.Append(operational(3)); err != nil {
		t.Fatalf("append that must evict: %v", err)
	}

	for _, overflow := range archiveOverflows(t, archive) {
		if overflow.Reason != event.ArchiveOverflowSize {
			continue
		}
		if overflow.Dropped != event.PriorityDiagnostic {
			t.Fatalf("size eviction dropped %q before diagnostics", overflow.Dropped)
		}
		if overflow.FirstSequence == 0 || overflow.Count == 0 {
			t.Fatalf("overflow named no range: %+v", overflow)
		}
		return
	}
	t.Fatal("records were evicted for size and no overflow record says so")
}

func clock2Root(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// 1.4 — a critical record is refused rather than dropped, and the refusal is
// visible.
func TestACriticalRecordIsRefusedRatherThanDropped(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	sizing := openArchive(t, clock2Root(t), clock, Options{})
	if _, err := sizing.Append(critical(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	measured, err := sizing.Size()
	if err != nil {
		t.Fatalf("size: %v", err)
	}

	archive := openArchive(t, root, clock, Options{MaxBytes: measured * 2})
	if _, err := archive.Append(critical(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	// The second critical fits; the third cannot, and nothing evictable is
	// left, so the archive must refuse instead of dropping evidence.
	_, _ = archive.Append(critical(2))
	if _, err := archive.Append(critical(3)); !errors.Is(err, ErrArchiveFull) {
		t.Fatalf("got %v, want %v", err, ErrArchiveFull)
	}

	var refusals int
	for _, overflow := range archiveOverflows(t, archive) {
		if overflow.Reason == event.ArchiveOverflowRefused {
			refusals++
			if overflow.FirstSequence != 0 || overflow.Count != 0 {
				t.Fatalf("a refusal claimed a dropped range: %+v", overflow)
			}
		}
	}
	if refusals == 0 {
		t.Fatal("an append was refused for the bound and nothing records it")
	}

	// Nothing critical was lost to make room. Every incident this archive
	// accepted is still readable, which is the whole reason the append was
	// refused rather than satisfied.
	var incidents int
	for _, record := range mustRecords(t, archive) {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := decoded.Payload.(*event.Incident); ok {
			incidents++
		}
	}
	if incidents == 0 {
		t.Fatal("the archive dropped every critical record it had accepted")
	}
}

func mustRecords(t *testing.T, archive *Archive) []Record {
	t.Helper()
	records, err := archive.Records()
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	return records
}

func archiveOverflows(t *testing.T, archive *Archive) []event.ArchiveOverflow {
	t.Helper()
	var found []event.ArchiveOverflow
	for _, record := range mustRecords(t, archive) {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload, ok := decoded.Payload.(*event.ArchiveOverflow); ok {
			found = append(found, *payload)
		}
	}
	return found
}

// 1.5 — a staged file an interrupted write left is removed, and removing it
// does not put a hole in the retained sequence, because nothing ever read it.
func TestAStagedRecordFromACrashIsDiscarded(t *testing.T) {
	root := t.TempDir()
	clock := newClock()
	archive := openArchive(t, root, clock, Options{})
	if _, err := archive.Append(operational(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	before := mustRecords(t, archive)

	staged := filepath.Join(root, "00000000000000009999.pending")
	if err := os.WriteFile(staged, []byte(`{"schema":"partial`), 0o600); err != nil {
		t.Fatalf("stage: %v", err)
	}

	reopened := openArchive(t, root, clock, Options{})
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("a staged record survived the reopen")
	}
	after := mustRecords(t, reopened)
	if len(after) != len(before) {
		t.Fatalf("holds %d records after the reopen, held %d", len(after), len(before))
	}
	if _, err := reopened.Append(operational(2)); err != nil {
		t.Fatalf("append after discarding a staged record: %v", err)
	}
}

// 1.5 — a record this build cannot read is reported, not skipped. Skipping it
// would make a damaged archive read as a shorter one.
func TestACorruptRecordIsReportedRatherThanSkipped(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{})
	if _, err := archive.Append(operational(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	var damaged string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), stableSuffix) {
			damaged = filepath.Join(root, entry.Name())
			break
		}
	}
	if damaged == "" {
		t.Fatal("no stable record to damage")
	}
	if err := os.WriteFile(damaged, []byte(`{"schema":"wrong"}`), 0o600); err != nil {
		t.Fatalf("damage: %v", err)
	}
	if _, err := archive.Records(); !errors.Is(err, ErrArchive) {
		t.Fatalf("got %v, want %v", err, ErrArchive)
	}
}

// 1.5 — a record that cannot fit the bound at any occupancy is refused as
// such, rather than emptying the archive in a doomed attempt to make room.
func TestARecordLargerThanTheBoundIsRefusedWithoutEvicting(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{MaxBytes: 64})
	if _, err := archive.Append(operational(1)); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("got %v, want %v", err, ErrRecordTooLarge)
	}
	if records := mustRecords(t, archive); len(records) != 0 {
		t.Fatalf("a refused oversize append left %d records", len(records))
	}
}

// 1.1 — an empty archive says it is empty rather than reporting a window that
// happens to start at the zero time.
func TestAnEmptyArchiveSaysSo(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{})
	window, err := archive.Window()
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if !window.Empty || window.Records != 0 {
		t.Fatalf("empty archive reported %+v", window)
	}
}

// 4.3 — a reader holds a handle that cannot write. A review being careful not
// to append is not the same as a review that could not.
func TestAReaderCannotAppend(t *testing.T) {
	root := t.TempDir()
	writer := openArchive(t, root, newClock(), Options{})
	if _, err := writer.Append(operational(1)); err != nil {
		t.Fatalf("append: %v", err)
	}

	reader, err := OpenForReading(root)
	if err != nil {
		t.Fatalf("open for reading: %v", err)
	}
	if records := mustRecords(t, reader); len(records) != 1 {
		t.Fatalf("a reader saw %d records, the archive holds 1", len(records))
	}
	if _, err := reader.Append(operational(2)); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("got %v, want %v", err, ErrReadOnly)
	}
	if records := mustRecords(t, reader); len(records) != 1 {
		t.Fatal("a refused append still changed the archive")
	}
}

// An archive that is not there is a fact a reader reports rather than one it
// fixes. Creating it would turn a missing archive into an empty one, and those
// are different answers.
func TestAReaderDoesNotCreateAMissingArchive(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := OpenForReading(missing); !errors.Is(err, ErrArchive) {
		t.Fatalf("got %v, want %v", err, ErrArchive)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("a reader created the archive it could not find")
	}
}

// 1.6 — archiving and upload are independent, in both directions.
//
// The spool removes a record when telemetry acknowledges its event id. The
// archive must offer no such door at all: a test that called one and saw
// nothing happen would only prove the door was shut today.
func TestTheArchiveOffersNoWayToRemoveARecord(t *testing.T) {
	surface := reflect.TypeOf(&Archive{})
	// One door in, three ways to look, and nothing that takes a record out.
	// The list is exhaustive rather than a pattern: a removal named something
	// innocuous would pass any pattern I could write, and this fails on the
	// method appearing at all.
	allowed := map[string]struct{}{
		"Append":  {},
		"Read":    {},
		"Records": {},
		"Window":  {},
		"Size":    {},
	}
	for index := 0; index < surface.NumMethod(); index++ {
		name := surface.Method(index).Name
		if _, ok := allowed[name]; ok {
			continue
		}
		t.Fatalf("%s is exported and is not one of the methods the archive is "+
			"allowed to have; retention here is age and size only, and a way "+
			"to take a record out is how upload state gets back in", name)
	}
}

// 1.6 — and it cannot reach the upload path to cause one either.
func TestTheArchiveCannotReachTheUploadPath(t *testing.T) {
	banned := []string{
		"/internal/spool",
		"/internal/telemetry",
		"/internal/cloudingest",
		"net",
		"net/http",
		"os/exec",
	}
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("import path in %s: %v", entry.Name(), err)
			}
			for _, forbidden := range banned {
				if path == forbidden || strings.HasSuffix(path, forbidden) {
					t.Fatalf("%s imports %s; archiving must not be able to "+
						"delay, duplicate or trigger an upload",
						entry.Name(), path)
				}
			}
		}
	}
}
