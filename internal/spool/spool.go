package spool

import (
	"bytes"
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

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	DefaultMaxBytes = int64(100 * 1024 * 1024)
	ownerSchema     = "hexroute.spool-owner.v1"
	entrySchema     = "hexroute.spool-entry.v1"
	ownerFilename   = ".owner.json"
)

type Owner string

const (
	OwnerRoot Owner = "root"
	OwnerUser Owner = "user"
)

type Options struct {
	MaxBytes int64
	NodeID   metadata.UUID
	Clock    metadata.Clock
	Random   io.Reader
}

type Entry struct {
	Sequence uint64
	Priority event.Priority
	Metadata metadata.Metadata
	Event    json.RawMessage
	Size     int64
}

type ownerMarker struct {
	Schema string `json:"schema"`
	Owner  Owner  `json:"owner"`
}

type wireEntry struct {
	Schema   string            `json:"schema"`
	Sequence uint64            `json:"sequence"`
	Priority event.Priority    `json:"priority"`
	Metadata metadata.Metadata `json:"metadata"`
	Event    json.RawMessage   `json:"event"`
}

type Spool struct {
	mu       sync.Mutex
	path     string
	owner    Owner
	maxBytes int64
	metadata *metadata.Generator
}

var (
	ErrInvalidOwner   = errors.New("invalid spool owner")
	ErrOwnerMismatch  = errors.New("spool owner does not match")
	ErrCorruptSpool   = errors.New("corrupt spool")
	ErrSpoolFull      = errors.New("spool cannot retain lower-priority event")
	ErrRecordTooLarge = errors.New("spool record exceeds hard limit")
)

func Open(path string, owner Owner, options Options) (*Spool, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrCorruptSpool)
	}
	if !validOwner(owner) {
		return nil, ErrInvalidOwner
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxBytes < 1 {
		return nil, fmt.Errorf("%w: invalid maximum size", ErrCorruptSpool)
	}

	if err := ensureDirectory(path); err != nil {
		return nil, err
	}
	if err := ensureOwner(path, owner); err != nil {
		return nil, err
	}

	spool := &Spool{
		path:     path,
		owner:    owner,
		maxBytes: maxBytes,
	}
	if err := spool.recover(); err != nil {
		return nil, err
	}
	entries, err := spool.scanStable()
	if err != nil {
		return nil, err
	}
	generator, err := metadata.NewGenerator(
		options.NodeID,
		lastSequence(entries),
		options.Clock,
		options.Random,
	)
	if err != nil {
		return nil, err
	}
	spool.metadata = generator
	return spool, nil
}

func (spool *Spool) Append(encodedEvent []byte) (uint64, error) {
	record, err := event.Decode(encodedEvent)
	if err != nil {
		return 0, err
	}

	spool.mu.Lock()
	defer spool.mu.Unlock()

	entries, err := spool.scanStable()
	if err != nil {
		return 0, err
	}
	eventMetadata, err := spool.metadata.Next()
	if err != nil {
		return 0, err
	}
	sequence := eventMetadata.Sequence
	if sequence != nextSequence(entries) {
		return 0, ErrCorruptSpool
	}
	incoming, err := newEntry(eventMetadata, record.Priority, encodedEvent)
	if err != nil {
		return 0, err
	}
	if incoming.Size > spool.maxBytes {
		return 0, ErrRecordTooLarge
	}
	if err := spool.stage(incoming); err != nil {
		return 0, err
	}

	required := excessBytes(totalSize(entries)+incoming.Size, spool.maxBytes)
	evictions, covered := chooseEvictions(entries, required, false)
	if covered {
		if err := spool.commit([]Entry{incoming}, evictions); err != nil {
			return 0, err
		}
		return sequence, nil
	}

	if record.Priority != event.PriorityCritical {
		_ = os.Remove(spool.pendingPath(sequence))
		if err := spool.recordOverflow(entries); err != nil {
			return 0, errors.Join(ErrSpoolFull, err)
		}
		return 0, ErrSpoolFull
	}

	overflow, err := spool.overflowEntry()
	if err != nil {
		return 0, err
	}
	if incoming.Size+overflow.Size > spool.maxBytes {
		_ = os.Remove(spool.pendingPath(sequence))
		return 0, ErrRecordTooLarge
	}
	if err := spool.stage(overflow); err != nil {
		_ = os.Remove(spool.pendingPath(sequence))
		return 0, err
	}

	required = excessBytes(
		totalSize(entries)+incoming.Size+overflow.Size,
		spool.maxBytes,
	)
	evictions, covered = chooseEvictions(entries, required, true)
	if !covered {
		_ = os.Remove(spool.pendingPath(sequence))
		_ = os.Remove(spool.pendingPath(overflow.Sequence))
		return 0, ErrRecordTooLarge
	}
	if err := spool.commit([]Entry{incoming, overflow}, evictions); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (spool *Spool) Entries() ([]Entry, error) {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	return spool.scanStable()
}

func (spool *Spool) Size() (int64, error) {
	entries, err := spool.Entries()
	if err != nil {
		return 0, err
	}
	return totalSize(entries), nil
}

func (spool *Spool) Acknowledge(eventIDs []metadata.UUID) (int, error) {
	acknowledged := make(map[metadata.UUID]struct{}, len(eventIDs))
	for _, eventID := range eventIDs {
		if _, err := metadata.ParseUUID(string(eventID)); err != nil {
			return 0, err
		}
		acknowledged[eventID] = struct{}{}
	}
	if len(acknowledged) == 0 {
		return 0, nil
	}

	spool.mu.Lock()
	defer spool.mu.Unlock()

	entries, err := spool.scanStable()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if _, ok := acknowledged[entry.Metadata.EventID]; !ok {
			continue
		}
		if err := os.Remove(spool.stablePath(entry.Sequence)); err != nil {
			return removed, fmt.Errorf("remove acknowledged spool record: %w", err)
		}
		removed++
	}
	if removed > 0 {
		if err := syncDirectory(spool.path); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (spool *Spool) recover() error {
	spool.mu.Lock()
	defer spool.mu.Unlock()

	directoryEntries, err := os.ReadDir(spool.path)
	if err != nil {
		return fmt.Errorf("read spool directory: %w", err)
	}
	pendingNames := make([]string, 0)
	for _, directoryEntry := range directoryEntries {
		name := directoryEntry.Name()
		if strings.HasPrefix(name, ".owner-") {
			if err := os.Remove(filepath.Join(spool.path, name)); err != nil {
				return fmt.Errorf("remove owner temporary file: %w", err)
			}
			continue
		}
		if strings.HasPrefix(name, ".pending-") {
			pendingNames = append(pendingNames, name)
		}
	}
	sort.Strings(pendingNames)

	for _, name := range pendingNames {
		path := filepath.Join(spool.path, name)
		pending, err := readEntry(path)
		if err != nil {
			if removeErr := os.Remove(path); removeErr != nil {
				return fmt.Errorf("remove incomplete spool record: %w", removeErr)
			}
			continue
		}
		if name != pendingName(pending.Sequence) {
			return ErrCorruptSpool
		}

		stablePath := spool.stablePath(pending.Sequence)
		if stableData, err := os.ReadFile(stablePath); err == nil {
			pendingData, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(stableData, pendingData) {
				return ErrCorruptSpool
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove duplicate pending record: %w", err)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect stable spool record: %w", err)
		}

		entries, err := spool.scanStable()
		if err != nil {
			return err
		}
		required := excessBytes(totalSize(entries)+pending.Size, spool.maxBytes)
		evictions, covered := chooseEvictions(
			entries,
			required,
			pending.Priority == event.PriorityCritical,
		)
		if !covered {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove unrecoverable pending record: %w", err)
			}
			continue
		}
		if err := spool.commit([]Entry{pending}, evictions); err != nil {
			return err
		}
	}

	entries, err := spool.scanStable()
	if err != nil {
		return err
	}
	if totalSize(entries) > spool.maxBytes {
		return ErrCorruptSpool
	}
	return nil
}

func (spool *Spool) recordOverflow(entries []Entry) error {
	overflow, err := spool.overflowEntry()
	if err != nil {
		return err
	}
	if overflow.Size > spool.maxBytes {
		return ErrRecordTooLarge
	}
	if err := spool.stage(overflow); err != nil {
		return err
	}
	required := excessBytes(totalSize(entries)+overflow.Size, spool.maxBytes)
	evictions, covered := chooseEvictions(entries, required, true)
	if !covered {
		_ = os.Remove(spool.pendingPath(overflow.Sequence))
		return ErrRecordTooLarge
	}
	return spool.commit([]Entry{overflow}, evictions)
}

func (spool *Spool) overflowEntry() (Entry, error) {
	eventMetadata, err := spool.metadata.Next()
	if err != nil {
		return Entry{}, err
	}
	sequence := eventMetadata.Sequence
	encoded, err := event.Encode(event.SchemaIncident, event.Incident{
		IncidentID: "spool-overflow-" + strconv.FormatUint(sequence, 10),
		Status:     event.IncidentOpened,
		Severity:   event.SeverityCritical,
		Category:   event.IncidentSpoolOverflow,
		Component:  control.ComponentRuntime,
		Generation: sequence,
	})
	if err != nil {
		return Entry{}, err
	}
	return newEntry(eventMetadata, event.PriorityCritical, encoded)
}

func (spool *Spool) stage(entry Entry) error {
	path := spool.pendingPath(entry.Sequence)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create pending spool record: %w", err)
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()

	encoded, err := marshalEntry(entry)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write pending spool record: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync pending spool record: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close pending spool record: %w", err)
	}
	if err := syncDirectory(spool.path); err != nil {
		return err
	}
	success = true
	return nil
}

func (spool *Spool) commit(staged []Entry, evictions []Entry) error {
	for _, entry := range evictions {
		if err := os.Remove(spool.stablePath(entry.Sequence)); err != nil {
			return fmt.Errorf("evict spool record: %w", err)
		}
	}
	for _, entry := range staged {
		if err := os.Rename(
			spool.pendingPath(entry.Sequence),
			spool.stablePath(entry.Sequence),
		); err != nil {
			return fmt.Errorf("commit spool record: %w", err)
		}
	}
	return syncDirectory(spool.path)
}

func (spool *Spool) scanStable() ([]Entry, error) {
	directoryEntries, err := os.ReadDir(spool.path)
	if err != nil {
		return nil, fmt.Errorf("read spool directory: %w", err)
	}

	entries := make([]Entry, 0)
	seen := make(map[uint64]struct{})
	seenEventIDs := make(map[metadata.UUID]struct{})
	for _, directoryEntry := range directoryEntries {
		name := directoryEntry.Name()
		if name == ownerFilename ||
			strings.HasPrefix(name, ".owner-") ||
			strings.HasPrefix(name, ".pending-") {
			continue
		}
		sequence, ok := parseStableName(name)
		if !ok || directoryEntry.Type()&os.ModeSymlink != 0 || directoryEntry.IsDir() {
			return nil, ErrCorruptSpool
		}
		entry, err := readEntry(filepath.Join(spool.path, name))
		if err != nil || entry.Sequence != sequence {
			return nil, ErrCorruptSpool
		}
		if _, duplicate := seen[sequence]; duplicate {
			return nil, ErrCorruptSpool
		}
		if _, duplicate := seenEventIDs[entry.Metadata.EventID]; duplicate {
			return nil, ErrCorruptSpool
		}
		seen[sequence] = struct{}{}
		seenEventIDs[entry.Metadata.EventID] = struct{}{}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Sequence < entries[right].Sequence
	})
	return entries, nil
}

func newEntry(eventMetadata metadata.Metadata, priority event.Priority, encodedEvent []byte) (Entry, error) {
	if err := metadata.Validate(eventMetadata); err != nil {
		return Entry{}, ErrCorruptSpool
	}
	record, err := event.Decode(encodedEvent)
	if err != nil || record.Priority != priority {
		return Entry{}, ErrCorruptSpool
	}
	entry := Entry{
		Sequence: eventMetadata.Sequence,
		Priority: priority,
		Metadata: eventMetadata,
		Event:    append(json.RawMessage(nil), encodedEvent...),
	}
	encoded, err := marshalEntry(entry)
	if err != nil {
		return Entry{}, err
	}
	entry.Size = int64(len(encoded))
	return entry, nil
}

func marshalEntry(entry Entry) ([]byte, error) {
	return json.Marshal(wireEntry{
		Schema:   entrySchema,
		Sequence: entry.Sequence,
		Priority: entry.Priority,
		Metadata: entry.Metadata,
		Event:    entry.Event,
	})
}

func readEntry(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return Entry{}, ErrCorruptSpool
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}

	var wire wireEntry
	if err := decodeStrict(data, &wire); err != nil || wire.Schema != entrySchema {
		return Entry{}, ErrCorruptSpool
	}
	if wire.Metadata.Sequence != wire.Sequence {
		return Entry{}, ErrCorruptSpool
	}
	entry, err := newEntry(wire.Metadata, wire.Priority, wire.Event)
	if err != nil {
		return Entry{}, err
	}
	if entry.Size != info.Size() {
		return Entry{}, ErrCorruptSpool
	}
	return entry, nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrCorruptSpool
	}
	return nil
}

func chooseEvictions(entries []Entry, required int64, includeCritical bool) ([]Entry, bool) {
	if required <= 0 {
		return nil, true
	}
	priorities := []event.Priority{
		event.PriorityDiagnostic,
		event.PriorityOperational,
	}
	if includeCritical {
		priorities = append(priorities, event.PriorityCritical)
	}

	var reclaimed int64
	evictions := make([]Entry, 0)
	for _, priority := range priorities {
		for _, entry := range entries {
			if entry.Priority != priority {
				continue
			}
			evictions = append(evictions, entry)
			reclaimed += entry.Size
			if reclaimed >= required {
				return evictions, true
			}
		}
	}
	return evictions, false
}

func totalSize(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Size
	}
	return total
}

func excessBytes(size, maximum int64) int64 {
	if size <= maximum {
		return 0
	}
	return size - maximum
}

func nextSequence(entries []Entry) uint64 {
	if len(entries) == 0 {
		return 1
	}
	return entries[len(entries)-1].Sequence + 1
}

func lastSequence(entries []Entry) uint64 {
	if len(entries) == 0 {
		return 0
	}
	return entries[len(entries)-1].Sequence
}

func (spool *Spool) stablePath(sequence uint64) string {
	return filepath.Join(spool.path, stableName(sequence))
}

func (spool *Spool) pendingPath(sequence uint64) string {
	return filepath.Join(spool.path, pendingName(sequence))
}

func stableName(sequence uint64) string {
	return fmt.Sprintf("%020d.event", sequence)
}

func pendingName(sequence uint64) string {
	return fmt.Sprintf(".pending-%020d.event", sequence)
}

func parseStableName(name string) (uint64, bool) {
	if len(name) != len("00000000000000000000.event") || !strings.HasSuffix(name, ".event") {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSuffix(name, ".event"), 10, 64)
	return value, err == nil && value > 0
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create spool directory: %w", err)
		}
	case err != nil:
		return fmt.Errorf("inspect spool directory: %w", err)
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return ErrCorruptSpool
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure spool directory: %w", err)
	}
	return nil
}

func ensureOwner(path string, owner Owner) error {
	markerPath := filepath.Join(path, ownerFilename)
	data, err := os.ReadFile(markerPath)
	if err == nil {
		var marker ownerMarker
		if decodeErr := decodeStrict(data, &marker); decodeErr != nil ||
			marker.Schema != ownerSchema {
			return ErrCorruptSpool
		}
		if marker.Owner != owner {
			return ErrOwnerMismatch
		}
		info, statErr := os.Lstat(markerPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return ErrCorruptSpool
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read spool owner: %w", err)
	}

	encoded, err := json.Marshal(ownerMarker{Schema: ownerSchema, Owner: owner})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(path, ".owner-*")
	if err != nil {
		return fmt.Errorf("create spool owner: %w", err)
	}
	tempPath := file.Name()
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure spool owner: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write spool owner: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync spool owner: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close spool owner: %w", err)
	}
	if err := os.Rename(tempPath, markerPath); err != nil {
		return fmt.Errorf("commit spool owner: %w", err)
	}
	if err := syncDirectory(path); err != nil {
		return err
	}
	success = true
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open spool directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync spool directory: %w", err)
	}
	return nil
}

func validOwner(owner Owner) bool {
	return owner == OwnerRoot || owner == OwnerUser
}
