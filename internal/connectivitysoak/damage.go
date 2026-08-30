package connectivitysoak

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
)

// Store faults are injected by editing the lineage on disk, because that is
// how they happen: a truncated write, a filesystem that lost a file, an editor
// pointed at the wrong directory. Reaching through the store's own API would
// only inject faults the API already refuses, which proves nothing.
//
// Files are found by content rather than by directory name. The soak has no
// business knowing the store's layout, and a layout change that silently made
// every fault a no-op is exactly the failure that would leave a qualification
// passing on nothing.

// damage injects one store-layer fault into the scratch lineage.
func (current *session) damage(fault connectivitytrace.Fault) error {
	entries, err := current.store.Index()
	if err != nil {
		return fmt.Errorf("%w: index: %v", ErrInject, err)
	}
	// Every store fault needs an ancestor to be about. The recovery bound
	// plus the newest is the deepest any of them reaches.
	needed := current.store.MaxRecoveryDepth() + 1
	if len(entries) < needed {
		return fmt.Errorf("%w: lineage holds %d checkpoints, %s needs %d",
			ErrInject, len(entries), fault, needed)
	}
	newest := entries[len(entries)-1]
	switch fault {
	case connectivitytrace.FaultParentTamper:
		return current.substituteParent(newest)
	case connectivitytrace.FaultOutputTamper:
		return current.rewriteOutputDigest(newest)
	case connectivitytrace.FaultMissingAncestor:
		return current.removeRecord(entries[len(entries)-2])
	case connectivitytrace.FaultCheckpointCorruption:
		return current.shredRecord(newest)
	case connectivitytrace.FaultDepthExhaustion:
		// One more than the search may walk back, so no candidate it is
		// allowed to consider can be proven. An intact ancestor still exists
		// underneath: the point is the bound, not the absence of evidence.
		for offset := 0; offset <= current.store.MaxRecoveryDepth(); offset++ {
			if err := current.shredRecord(entries[len(entries)-1-offset]); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %s is not a store fault", ErrInject, fault)
	}
}

// substituteParent points a checkpoint at ancestry it was not sealed against,
// and reseals it so the record is internally perfect. Only the parent-digest
// link catches this one; a store that verified records alone would resume it.
func (current *session) substituteParent(entry connectivitycheckpoint.IndexEntry) error {
	checkpoint, err := current.store.Load(entry.ID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInject, err)
	}
	if checkpoint.Parent == "" {
		return fmt.Errorf("%w: genesis has no parent to substitute", ErrInject)
	}
	checkpoint.ParentDigest = strings.Repeat("a", 64)
	resealed, err := connectivitycheckpoint.Seal(checkpoint)
	if err != nil {
		return fmt.Errorf("%w: reseal: %v", ErrInject, err)
	}
	if err := current.writeRecord(entry.ID, resealed); err != nil {
		return err
	}
	// The lineage entry names the record's address, so a tamperer who left it
	// alone would be caught by the index rather than by the parent link, and
	// the trace would be about the wrong defence.
	entry.Digest = resealed.Digest
	return current.writeIndexEntry(entry)
}

// rewriteOutputDigest makes a record name an output it did not produce, and
// reseals it so the record is beyond reproach on its own terms.
//
// Nothing inside the record can catch this: the diff digest is the address of
// something the record does not carry, so there is nothing to compare it
// against. What catches it is the lineage, which recorded the address this
// record had when it was appended and no longer agrees with it. That is a
// different defence from the one an unreadable record meets, which is why
// this is a separate fault rather than a second name for corruption.
func (current *session) rewriteOutputDigest(entry connectivitycheckpoint.IndexEntry) error {
	checkpoint, err := current.store.Load(entry.ID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInject, err)
	}
	checkpoint.DiffDigest = strings.Repeat("c", 64)
	resealed, err := connectivitycheckpoint.Seal(checkpoint)
	if err != nil {
		return fmt.Errorf("%w: reseal: %v", ErrInject, err)
	}
	return current.writeRecord(entry.ID, resealed)
}

func (current *session) removeRecord(entry connectivitycheckpoint.IndexEntry) error {
	path, err := current.recordPath(entry.ID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("%w: remove %s: %v", ErrInject, entry.ID, err)
	}
	return nil
}

func (current *session) shredRecord(entry connectivitycheckpoint.IndexEntry) error {
	path, err := current.recordPath(entry.ID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("{\"schema\":\"truncated\""), 0o600); err != nil {
		return fmt.Errorf("%w: shred %s: %v", ErrInject, entry.ID, err)
	}
	return nil
}

func (current *session) writeRecord(id string, checkpoint connectivitycheckpoint.Checkpoint) error {
	path, err := current.recordPath(id)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("%w: encode: %v", ErrInject, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: write %s: %v", ErrInject, id, err)
	}
	return nil
}

func (current *session) writeIndexEntry(entry connectivitycheckpoint.IndexEntry) error {
	path, err := current.indexPath(entry.ID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("%w: encode: %v", ErrInject, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: write index %s: %v", ErrInject, entry.ID, err)
	}
	return nil
}

// recordPath finds the file holding one checkpoint, by what is in it.
func (current *session) recordPath(id string) (string, error) {
	return current.find(id, true)
}

// indexPath finds the file holding one lineage entry, by what is in it.
func (current *session) indexPath(id string) (string, error) {
	return current.find(id, false)
}

// find walks the scratch store for the file whose JSON names this identifier.
//
// A checkpoint record carries the snapshot; a lineage entry does not. That is
// the whole difference between the two, and it is a difference in content, so
// it survives any rearrangement of directories.
func (current *session) find(id string, wantRecord bool) (string, error) {
	root := filepath.Join(current.root, storeDirectory)
	found := ""
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found != "" ||
			!strings.HasSuffix(entry.Name(), ".json") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return nil
		}
		var named string
		if json.Unmarshal(fields["id"], &named) != nil || named != id {
			return nil
		}
		_, isRecord := fields["snapshot"]
		// The pointer also names an id and carries no snapshot, so a lineage
		// entry is told from it by the sequence only an entry has.
		_, isEntry := fields["sequence"]
		if isRecord == wantRecord && (wantRecord || isEntry) {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("%w: search %s: %v", ErrInject, root, err)
	}
	if found == "" {
		return "", fmt.Errorf("%w: nothing in the store names %s", ErrInject, id)
	}
	return found, nil
}
