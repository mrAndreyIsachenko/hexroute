package spool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// stableRecord is what the spool can learn about a stored record without
// opening it: which sequence it holds, and how many bytes it occupies.
//
// Both come from the directory itself. The sequence is the filename, and the
// size is what the filesystem reports — the same number readEntry recomputes by
// re-marshalling the record and then compares against this one.
type stableRecord struct {
	Sequence uint64
	Size     int64
}

// scanIndex lists the stable records without reading any of them.
//
// It exists because most of what the spool does never looks inside a record.
// Appending needs the highest sequence and the total size; reporting size needs
// the total; opening needs the highest. Decoding sixty thousand payloads to
// learn two numbers is the whole of the defect this replaces.
//
// What it still refuses is anything that makes the directory itself
// untrustworthy: a name that is not a record, a symlink, a subdirectory. Those
// are statements about the spool rather than about one record in it, and a
// spool whose shape is wrong cannot be reasoned about at all.
func (spool *Spool) scanIndex() ([]stableRecord, error) {
	directoryEntries, err := os.ReadDir(spool.path)
	if err != nil {
		return nil, fmt.Errorf("read spool directory: %w", err)
	}

	records := make([]stableRecord, 0, len(directoryEntries))
	seen := make(map[uint64]struct{}, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		name := directoryEntry.Name()
		if isReservedName(name) {
			continue
		}
		sequence, ok := parseStableName(name)
		if !ok || directoryEntry.Type()&os.ModeSymlink != 0 || directoryEntry.IsDir() {
			return nil, ErrCorruptSpool
		}
		if _, duplicate := seen[sequence]; duplicate {
			return nil, ErrCorruptSpool
		}
		info, err := os.Lstat(filepath.Join(spool.path, name))
		if err != nil {
			return nil, fmt.Errorf("inspect spool record: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, ErrCorruptSpool
		}
		seen[sequence] = struct{}{}
		records = append(records, stableRecord{Sequence: sequence, Size: info.Size()})
	}
	sort.Slice(records, func(left, right int) bool {
		return records[left].Sequence < records[right].Sequence
	})
	return records, nil
}

func indexLastSequence(records []stableRecord) uint64 {
	if len(records) == 0 {
		return 0
	}
	return records[len(records)-1].Sequence
}

func indexTotalSize(records []stableRecord) int64 {
	var total int64
	for _, record := range records {
		total += record.Size
	}
	return total
}
