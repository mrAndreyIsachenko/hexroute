// Package eventarchive keeps typed events on the host for as long as their age
// and size bounds allow, rather than until telemetry acknowledges them.
//
// The upload spool answers a different question. Its retention is a function of
// upload success: a record leaves the moment the cloud says it arrived, and
// raising its bound changes nothing, because the bound was never what removed
// the record. The consequence is a host that cannot answer questions about last
// week — which is the state every incident review on this machine has started
// from.
//
// The archive is a second sink for the same records, not a change to the first.
// Nothing here delays, duplicates or triggers an upload, and no acknowledgement
// reaches it.
//
// The write discipline is deliberately the spool's, reproduced rather than
// shared. internal/spool carries the connectivity read model's journal, and a
// shared abstraction extracted while that journal is under a running
// qualification would be a change to the one thing the soak is measuring. Two
// users with settled requirements are a better basis for the abstraction than
// one user and a guess.
package eventarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	// DefaultMaxBytes bounds the archive on disk.
	DefaultMaxBytes int64 = 256 * 1024 * 1024
	// DefaultMaxAge bounds how far back the archive answers for. It is the
	// window an incident review actually asks about.
	DefaultMaxAge = 30 * 24 * time.Hour

	stableSuffix  = ".event"
	pendingSuffix = ".pending"
)

var (
	// ErrArchive is a failure of the archive itself.
	ErrArchive = errors.New("event archive is unusable")
	// ErrArchiveFull means the append could not be satisfied without dropping
	// a critical record, so it was refused instead.
	ErrArchiveFull = errors.New("event archive is full of critical records")
	// ErrRecordTooLarge means one record cannot fit the configured bound at
	// any occupancy.
	ErrRecordTooLarge = errors.New("event exceeds the archive size bound")
)

// Options configure one archive.
type Options struct {
	MaxBytes int64
	MaxAge   time.Duration
	NodeID   metadata.UUID
	Clock    metadata.Clock
	Random   io.Reader
}

// Record is one archived event as the archive holds it.
type Record struct {
	Sequence uint64
	Priority event.Priority
	Metadata metadata.Metadata
	Event    json.RawMessage
	Size     int64
}

// Window is what the archive can actually answer for.
//
// It is reported rather than assumed because a bounded archive that has evicted
// its oldest records still answers every query — it just answers about a
// shorter period than was asked for, and a reader who cannot see that reads an
// eviction as a quiet week.
type Window struct {
	Records uint32
	First   uint64
	Last    uint64
	Oldest  time.Time
	Newest  time.Time
	// Empty distinguishes an archive holding nothing from one whose window
	// happens to start at the zero time.
	Empty bool
}

type wireRecord struct {
	Schema   string            `json:"schema"`
	Sequence uint64            `json:"sequence"`
	Priority event.Priority    `json:"priority"`
	Metadata metadata.Metadata `json:"metadata"`
	Event    json.RawMessage   `json:"event"`
}

// RecordSchema names the shape of one archived record.
const RecordSchema = "hexroute.event-archive.record.v1"

// Archive is the durable local event archive.
type Archive struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxAge   time.Duration
	clock    metadata.Clock
	metadata *metadata.Generator
	// refused counts records no registered schema described. It is reported
	// at each doubling rather than per refusal: a source emitting malformed
	// records emits many, and one diagnostic each would let a broken producer
	// evict the evidence the archive exists to keep.
	refused uint32
}

// Open prepares the archive, removing any staged record an interrupted write
// left behind.
func Open(path string, options Options) (*Archive, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrArchive)
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	maxAge := options.MaxAge
	if maxAge == 0 {
		maxAge = DefaultMaxAge
	}
	if maxBytes < 1 || maxAge < 0 {
		return nil, fmt.Errorf("%w: invalid bounds", ErrArchive)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArchive, err)
	}

	clock := options.Clock
	if clock == nil {
		clock = metadata.NewSystemClock()
	}
	archive := &Archive{
		path: path, maxBytes: maxBytes, maxAge: maxAge, clock: clock,
	}
	if err := archive.discardStaged(); err != nil {
		return nil, err
	}
	records, err := archive.scan()
	if err != nil {
		return nil, err
	}
	generator, err := metadata.NewGenerator(
		options.NodeID, highest(records), clock, options.Random)
	if err != nil {
		return nil, err
	}
	archive.metadata = generator
	return archive, nil
}

// Append records one encoded event.
//
// The record must decode under a registered schema. Age eviction runs first and
// ignores priority; size eviction then removes diagnostics before operational
// records and operational before critical. Whatever either removes is named in
// a durable overflow record, and an append that could only be satisfied by
// dropping a critical record is refused instead.
func (archive *Archive) Append(encoded []byte) (uint64, error) {
	decoded, err := event.Decode(encoded)
	if err != nil {
		// The caller is told, and so is the archive. A refusal only the
		// caller saw is a record of nothing to whoever reads the archive
		// afterwards, and "no such record was ever offered" is exactly the
		// wrong conclusion to leave available.
		archive.mu.Lock()
		defer archive.mu.Unlock()
		if refusalErr := archive.recordRefusedRecord(); refusalErr != nil {
			return 0, errors.Join(err, refusalErr)
		}
		return 0, err
	}

	archive.mu.Lock()
	defer archive.mu.Unlock()

	held, err := archive.scan()
	if err != nil {
		return 0, err
	}
	expired, kept := archive.partitionByAge(held)
	if len(expired) > 0 {
		if err := archive.evict(expired); err != nil {
			return 0, err
		}
		if err := archive.recordOverflow(
			event.ArchiveOverflowAge, expired); err != nil {
			return 0, err
		}
		kept, err = archive.scan()
		if err != nil {
			return 0, err
		}
	}

	stamp, err := archive.metadata.Next()
	if err != nil {
		return 0, err
	}
	incoming, err := newRecord(stamp, decoded.Priority, encoded)
	if err != nil {
		return 0, err
	}
	if incoming.Size > archive.maxBytes {
		return 0, ErrRecordTooLarge
	}

	excess := totalSize(kept) + incoming.Size - archive.maxBytes
	if excess > 0 {
		evictions, covered := chooseEvictions(kept, excess)
		if !covered {
			// Refusing is the answer the bound demands. Dropping a critical
			// record to make room would trade the evidence for the sample.
			if err := archive.recordRefusal(incoming.Priority); err != nil {
				return 0, errors.Join(ErrArchiveFull, err)
			}
			return 0, ErrArchiveFull
		}
		if err := archive.stage(incoming); err != nil {
			return 0, err
		}
		if err := archive.commit(incoming, evictions); err != nil {
			return 0, err
		}
		if err := archive.recordOverflow(
			event.ArchiveOverflowSize, evictions); err != nil {
			return 0, err
		}
		return incoming.Sequence, nil
	}

	if err := archive.stage(incoming); err != nil {
		return 0, err
	}
	if err := archive.commit(incoming, nil); err != nil {
		return 0, err
	}
	return incoming.Sequence, nil
}

// Records returns everything retained, oldest first.
func (archive *Archive) Records() ([]Record, error) {
	archive.mu.Lock()
	defer archive.mu.Unlock()
	return archive.scan()
}

// Window reports what the archive can answer for.
func (archive *Archive) Window() (Window, error) {
	archive.mu.Lock()
	defer archive.mu.Unlock()
	records, err := archive.scan()
	if err != nil {
		return Window{}, err
	}
	if len(records) == 0 {
		return Window{Empty: true}, nil
	}
	window := Window{
		Records: uint32(len(records)),
		First:   records[0].Sequence,
		Last:    records[len(records)-1].Sequence,
		Oldest:  records[0].Metadata.WallClock.UTC(),
		Newest:  records[len(records)-1].Metadata.WallClock.UTC(),
	}
	return window, nil
}

// Size reports the bytes the archive currently occupies.
func (archive *Archive) Size() (int64, error) {
	archive.mu.Lock()
	defer archive.mu.Unlock()
	records, err := archive.scan()
	if err != nil {
		return 0, err
	}
	return totalSize(records), nil
}

func (archive *Archive) partitionByAge(records []Record) (expired, kept []Record) {
	if archive.maxAge == 0 {
		return nil, records
	}
	boundary := archive.clock.WallNow().UTC().Add(-archive.maxAge)
	for _, record := range records {
		if record.Metadata.WallClock.UTC().Before(boundary) {
			expired = append(expired, record)
			continue
		}
		kept = append(kept, record)
	}
	return expired, kept
}

// chooseEvictions removes the cheapest evidence first and never offers a
// critical record, so a caller that cannot cover its excess is told so rather
// than handed a plan that costs more than the append is worth.
func chooseEvictions(records []Record, excess int64) ([]Record, bool) {
	order := []event.Priority{
		event.PriorityDiagnostic,
		event.PriorityOperational,
	}
	var chosen []Record
	var freed int64
	for _, priority := range order {
		for _, record := range records {
			if record.Priority != priority {
				continue
			}
			chosen = append(chosen, record)
			freed += record.Size
			if freed >= excess {
				return chosen, true
			}
		}
	}
	return chosen, false
}

func (archive *Archive) recordOverflow(
	reason event.ArchiveOverflowReason, dropped []Record,
) error {
	byPriority := map[event.Priority][]Record{}
	for _, record := range dropped {
		byPriority[record.Priority] = append(byPriority[record.Priority], record)
	}
	for _, priority := range []event.Priority{
		event.PriorityDiagnostic,
		event.PriorityOperational,
		event.PriorityCritical,
	} {
		group := byPriority[priority]
		if len(group) == 0 {
			continue
		}
		first, last := group[0].Sequence, group[0].Sequence
		for _, record := range group {
			if record.Sequence < first {
				first = record.Sequence
			}
			if record.Sequence > last {
				last = record.Sequence
			}
		}
		encoded, err := event.Encode(
			event.SchemaArchiveOverflow, event.ArchiveOverflow{
				Reason: reason, Dropped: priority,
				FirstSequence: first, LastSequence: last,
				Count: uint32(len(group)),
			})
		if err != nil {
			return fmt.Errorf("%w: %v", ErrArchive, err)
		}
		if err := archive.appendOverflow(encoded); err != nil {
			return err
		}
	}
	return nil
}

// recordRefusedRecord notes an offered record no schema described, at the
// first refusal and at each doubling after it.
func (archive *Archive) recordRefusedRecord() error {
	archive.refused++
	if archive.refused&(archive.refused-1) != 0 {
		return nil
	}
	encoded, err := event.Encode(event.SchemaDiagnostic, event.Diagnostic{
		Component: control.ComponentRuntime,
		Code:      event.DiagnosticArchiveRefusedRecord,
		Count:     archive.refused,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchive, err)
	}
	stamp, err := archive.metadata.Next()
	if err != nil {
		return err
	}
	record, err := newRecord(stamp, event.PriorityDiagnostic, encoded)
	if err != nil {
		return err
	}
	if err := archive.stage(record); err != nil {
		return err
	}
	return archive.commit(record, nil)
}

func (archive *Archive) recordRefusal(priority event.Priority) error {
	encoded, err := event.Encode(
		event.SchemaArchiveOverflow, event.ArchiveOverflow{
			Reason: event.ArchiveOverflowRefused, Dropped: priority,
		})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchive, err)
	}
	return archive.appendOverflow(encoded)
}

// appendOverflow writes an overflow record without consulting the bounds.
//
// An overflow that could not be written because the archive was full would
// leave exactly the condition it exists to report invisible. It is critical and
// small, and the size it can add is bounded by how often a bound is reached.
func (archive *Archive) appendOverflow(encoded []byte) error {
	stamp, err := archive.metadata.Next()
	if err != nil {
		return err
	}
	record, err := newRecord(stamp, event.PriorityCritical, encoded)
	if err != nil {
		return err
	}
	if err := archive.stage(record); err != nil {
		return err
	}
	return archive.commit(record, nil)
}

func (archive *Archive) stage(record Record) error {
	path := archive.pendingPath(record.Sequence)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: stage record: %v", ErrArchive, err)
	}
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = os.Remove(path)
		}
	}()

	encoded, err := json.Marshal(wireRecord{
		Schema: RecordSchema, Sequence: record.Sequence,
		Priority: record.Priority, Metadata: record.Metadata,
		Event: record.Event,
	})
	if err != nil {
		return fmt.Errorf("%w: encode record: %v", ErrArchive, err)
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("%w: write record: %v", ErrArchive, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: sync record: %v", ErrArchive, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%w: close record: %v", ErrArchive, err)
	}
	if err := syncDirectory(archive.path); err != nil {
		return err
	}
	published = true
	return nil
}

func (archive *Archive) commit(record Record, evictions []Record) error {
	if err := archive.evict(evictions); err != nil {
		return err
	}
	if err := os.Rename(
		archive.pendingPath(record.Sequence),
		archive.stablePath(record.Sequence),
	); err != nil {
		return fmt.Errorf("%w: publish record: %v", ErrArchive, err)
	}
	return syncDirectory(archive.path)
}

func (archive *Archive) evict(records []Record) error {
	for _, record := range records {
		if err := os.Remove(archive.stablePath(record.Sequence)); err != nil &&
			!os.IsNotExist(err) {
			return fmt.Errorf("%w: evict record: %v", ErrArchive, err)
		}
	}
	if len(records) == 0 {
		return nil
	}
	return syncDirectory(archive.path)
}

// discardStaged removes what an interrupted write left. A staged file was never
// published, so removing it cannot create a gap in the retained sequence: no
// reader ever saw it.
func (archive *Archive) discardStaged() error {
	entries, err := os.ReadDir(archive.path)
	if err != nil {
		return fmt.Errorf("%w: read directory: %v", ErrArchive, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pendingSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(archive.path, entry.Name())); err != nil {
			return fmt.Errorf("%w: discard staged record: %v", ErrArchive, err)
		}
	}
	return nil
}

func (archive *Archive) scan() ([]Record, error) {
	entries, err := os.ReadDir(archive.path)
	if err != nil {
		return nil, fmt.Errorf("%w: read directory: %v", ErrArchive, err)
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), stableSuffix) {
			continue
		}
		path := filepath.Join(archive.path, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%w: read record: %v", ErrArchive, err)
		}
		var wire wireRecord
		if err := json.Unmarshal(raw, &wire); err != nil ||
			wire.Schema != RecordSchema {
			return nil, fmt.Errorf("%w: %s is not an archive record",
				ErrArchive, entry.Name())
		}
		records = append(records, Record{
			Sequence: wire.Sequence, Priority: wire.Priority,
			Metadata: wire.Metadata, Event: wire.Event,
			Size: int64(len(raw)),
		})
	}
	sort.Slice(records, func(one, other int) bool {
		return records[one].Sequence < records[other].Sequence
	})
	return records, nil
}

func newRecord(
	stamp metadata.Metadata, priority event.Priority, encoded []byte,
) (Record, error) {
	held := make(json.RawMessage, len(encoded))
	copy(held, encoded)
	record := Record{
		Sequence: stamp.Sequence, Priority: priority,
		Metadata: stamp, Event: held,
	}
	measured, err := json.Marshal(wireRecord{
		Schema: RecordSchema, Sequence: record.Sequence,
		Priority: record.Priority, Metadata: record.Metadata,
		Event: record.Event,
	})
	if err != nil {
		return Record{}, fmt.Errorf("%w: encode record: %v", ErrArchive, err)
	}
	record.Size = int64(len(measured))
	return record, nil
}

func (archive *Archive) stablePath(sequence uint64) string {
	return filepath.Join(archive.path, name(sequence)+stableSuffix)
}

func (archive *Archive) pendingPath(sequence uint64) string {
	return filepath.Join(archive.path, name(sequence)+pendingSuffix)
}

func name(sequence uint64) string {
	return fmt.Sprintf("%020s", strconv.FormatUint(sequence, 10))
}

func highest(records []Record) uint64 {
	var top uint64
	for _, record := range records {
		if record.Sequence > top {
			top = record.Sequence
		}
	}
	return top
}

func totalSize(records []Record) int64 {
	var total int64
	for _, record := range records {
		total += record.Size
	}
	return total
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open directory: %v", ErrArchive, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("%w: sync directory: %v", ErrArchive, err)
	}
	return nil
}
