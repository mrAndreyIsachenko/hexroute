package connectivitycheckpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Operation names which of the three durable writes is in progress.
type Operation string

const (
	OpCheckpoint Operation = "checkpoint"
	OpIndex      Operation = "index"
	OpPointer    Operation = "pointer"
)

// Boundary names a point in the write sequence a crash can land on.
type Boundary string

const (
	BeforeFileSync      Boundary = "before_file_fsync"
	AfterFileSync       Boundary = "after_file_fsync"
	BeforeRename        Boundary = "before_rename"
	AfterRename         Boundary = "after_rename"
	BeforeDirectorySync Boundary = "before_directory_fsync"
	AfterDirectorySync  Boundary = "after_directory_fsync"
)

// Fault stops a write at one boundary. It exists so the recovery paths can be
// exercised at every point a real crash could interrupt them.
type Fault struct {
	Operation Operation
	Boundary  Boundary
}

// Options configures a store.
type Options struct {
	// MaxRecoveryDepth bounds the backwards search for a provable ancestor.
	MaxRecoveryDepth int
	// Faults interrupt writes. Production leaves this empty.
	Faults []Fault
}

// Store is the durable read-model lineage on disk.
type Store struct {
	mu     sync.Mutex
	root   string
	depth  int
	faults map[Fault]struct{}
}

const (
	checkpointDir = "checkpoints"
	indexDir      = "index"
	pointerName   = "latest.json"
)

// Open prepares the store, creating its directories if needed.
func Open(root string, options Options) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrStoreUnavailable)
	}
	depth := options.MaxRecoveryDepth
	if depth <= 0 {
		depth = DefaultMaxRecoveryDepth
	}
	store := &Store{root: root, depth: depth, faults: make(map[Fault]struct{}, len(options.Faults))}
	for _, fault := range options.Faults {
		store.faults[fault] = struct{}{}
	}
	for _, directory := range []string{root, filepath.Join(root, checkpointDir), filepath.Join(root, indexDir)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
	}
	return store, nil
}

// MaxRecoveryDepth reports the configured bound on the backwards search.
func (store *Store) MaxRecoveryDepth() int { return store.depth }

func (store *Store) fires(operation Operation, boundary Boundary) bool {
	_, found := store.faults[Fault{Operation: operation, Boundary: boundary}]
	return found
}

// writeRecord performs one durable write: staged file, file sync, rename,
// directory sync. A fault at any boundary leaves exactly the state a crash at
// that point would leave.
func (store *Store) writeRecord(
	operation Operation,
	directory string,
	name string,
	content []byte,
	replace bool,
) error {
	staged := filepath.Join(directory, "."+name+".tmp")
	_ = os.Remove(staged)
	file, err := os.OpenFile(staged,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("%w: stage %s: %v", ErrStoreUnavailable, name, err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(staged)
		return fmt.Errorf("%w: write %s: %v", ErrStoreUnavailable, name, err)
	}
	if store.fires(operation, BeforeFileSync) {
		_ = file.Close()
		return ErrInjectedFault
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("%w: sync %s: %v", ErrStoreUnavailable, name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%w: close %s: %v", ErrStoreUnavailable, name, err)
	}
	if store.fires(operation, AfterFileSync) {
		return ErrInjectedFault
	}
	if store.fires(operation, BeforeRename) {
		return ErrInjectedFault
	}

	target := filepath.Join(directory, name)
	if replace {
		err = os.Rename(staged, target)
	} else {
		err = renameNoReplace(staged, target)
	}
	if err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("%w: publish %s: %v", ErrStoreUnavailable, name, err)
	}
	if store.fires(operation, AfterRename) {
		return ErrInjectedFault
	}
	if store.fires(operation, BeforeDirectorySync) {
		return ErrInjectedFault
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if store.fires(operation, AfterDirectorySync) {
		return ErrInjectedFault
	}
	return nil
}

func syncDirectory(path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open dir: %v", ErrStoreUnavailable, err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("%w: sync dir: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func readJSON(path string, destination any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if len(content) == 0 || len(content) > MaxCheckpointBytes {
		return fmt.Errorf("%w: size", ErrInvalidCheckpoint)
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCheckpoint, err)
	}
	if decoder.More() {
		return fmt.Errorf("%w: trailing data", ErrInvalidCheckpoint)
	}
	return nil
}

// Append adds a checkpoint to the lineage.
//
// The write order is deliberate: the record first, then the lineage entry,
// then the pointer. A crash before the pointer moves leaves a complete record
// that nothing yet refers to, which is recoverable. The reverse order would
// leave a pointer to something that may not exist.
func (store *Store) Append(checkpoint Checkpoint) error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	pointer, err := store.pointerLocked()
	switch {
	case err == nil:
		if checkpoint.Parent != pointer.ID {
			return fmt.Errorf("%w: parent %q, latest is %q",
				ErrGenerationGuard, checkpoint.Parent, pointer.ID)
		}
		if checkpoint.ParentDigest != pointer.Digest {
			return fmt.Errorf("%w: parent digest", ErrGenerationGuard)
		}
		if checkpoint.SnapshotGeneration < pointer.Generation {
			return fmt.Errorf("%w: generation %d is behind %d",
				ErrGenerationGuard, checkpoint.SnapshotGeneration, pointer.Generation)
		}
	case err == ErrNotFound:
		if checkpoint.Parent != "" {
			return fmt.Errorf("%w: first checkpoint claims a parent", ErrGenerationGuard)
		}
	default:
		return err
	}

	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCheckpoint, err)
	}
	if len(encoded) > MaxCheckpointBytes {
		return fmt.Errorf("%w: record too large", ErrInvalidCheckpoint)
	}
	if err := store.writeRecord(OpCheckpoint, filepath.Join(store.root, checkpointDir),
		checkpoint.ID+".json", encoded, false); err != nil {
		return err
	}

	sequence := pointer.Sequence + 1
	entry := IndexEntry{
		Schema: IndexSchema, Sequence: sequence, ID: checkpoint.ID,
		Parent: checkpoint.Parent, Digest: checkpoint.Digest,
		Generation: checkpoint.SnapshotGeneration, ConsumedTo: checkpoint.ConsumedTo,
	}
	encodedEntry, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCheckpoint, err)
	}
	if err := store.writeRecord(OpIndex, filepath.Join(store.root, indexDir),
		indexName(sequence), encodedEntry, false); err != nil {
		return err
	}
	overflow, err := store.evictIndexLocked()
	if err != nil {
		return err
	}

	next := Pointer{
		Schema: PointerSchema, ID: checkpoint.ID, Digest: checkpoint.Digest,
		Generation: checkpoint.SnapshotGeneration, Sequence: sequence,
		Overflow: pointer.Overflow || overflow,
	}
	encodedPointer, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCheckpoint, err)
	}
	return store.writeRecord(OpPointer, store.root, pointerName, encodedPointer, true)
}

func indexName(sequence uint64) string {
	return fmt.Sprintf("%020d.json", sequence)
}

// evictIndexLocked keeps the lineage bounded and reports whether anything was
// dropped, so a walk that runs out of ancestors can tell "never existed" from
// "was evicted".
func (store *Store) evictIndexLocked() (bool, error) {
	entries, err := store.indexLocked()
	if err != nil {
		return false, err
	}
	if len(entries) <= MaxIndexEntries {
		return false, nil
	}
	directory := filepath.Join(store.root, indexDir)
	dropped := false
	for _, entry := range entries[:len(entries)-MaxIndexEntries] {
		if err := os.Remove(filepath.Join(directory, indexName(entry.Sequence))); err != nil &&
			!os.IsNotExist(err) {
			return false, fmt.Errorf("%w: evict index: %v", ErrStoreUnavailable, err)
		}
		dropped = true
	}
	if dropped {
		if err := syncDirectory(directory); err != nil {
			return false, err
		}
	}
	return dropped, nil
}

// Pointer returns the newest checkpoint the store believes in.
func (store *Store) Pointer() (Pointer, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.pointerLocked()
}

func (store *Store) pointerLocked() (Pointer, error) {
	var pointer Pointer
	if err := readJSON(filepath.Join(store.root, pointerName), &pointer); err != nil {
		return Pointer{}, err
	}
	if pointer.Schema != PointerSchema || !validIdentifier(pointer.ID) ||
		len(pointer.Digest) != 64 || pointer.Generation == 0 || pointer.Sequence == 0 {
		return Pointer{}, fmt.Errorf("%w: pointer", ErrInvalidCheckpoint)
	}
	return pointer, nil
}

// Load returns one checkpoint by identity, verifying its own address.
func (store *Store) Load(id string) (Checkpoint, error) {
	if !validIdentifier(id) {
		return Checkpoint{}, fmt.Errorf("%w: id", ErrInvalidCheckpoint)
	}
	var checkpoint Checkpoint
	err := readJSON(filepath.Join(store.root, checkpointDir, id+".json"), &checkpoint)
	if err != nil {
		return Checkpoint{}, err
	}
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	if checkpoint.ID != id {
		return Checkpoint{}, fmt.Errorf("%w: record %q is filed as %q",
			ErrInvalidCheckpoint, checkpoint.ID, id)
	}
	return checkpoint, nil
}

// Index returns the retained lineage entries oldest first.
func (store *Store) Index() ([]IndexEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.indexLocked()
}

func (store *Store) indexLocked() ([]IndexEntry, error) {
	directory := filepath.Join(store.root, indexDir)
	listing, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	entries := make([]IndexEntry, 0, len(listing))
	for _, item := range listing {
		name := item.Name()
		if item.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 64); err != nil {
			continue
		}
		var entry IndexEntry
		if err := readJSON(filepath.Join(directory, name), &entry); err != nil {
			return nil, err
		}
		if entry.Schema != IndexSchema || !validIdentifier(entry.ID) || len(entry.Digest) != 64 {
			return nil, fmt.Errorf("%w: index entry %s", ErrInvalidCheckpoint, name)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	return entries, nil
}
