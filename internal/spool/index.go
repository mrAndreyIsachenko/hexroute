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
// index answers what the spool holds, listing the directory only when it does
// not already know.
//
// Listing is O(records stored) and an append needs the listing, so taking it
// per append made the cost O(stored x appended). A runtime appends eleven
// records per cycle into a spool that had reached six figures, and the listing
// is where a profile of the live process found it standing.
//
// The spool keeps what it learned and amends it as it publishes and evicts.
// Anything else that touches the stable records drops it instead: acknowledging
// an upload and quarantining a record are rare beside appending, and paying a
// listing there is cheaper than reasoning about whether the amendment is right.
func (spool *Spool) index() ([]stableRecord, error) {
	if spool.indexed != nil {
		return spool.indexed, nil
	}
	records, err := spool.scanIndex()
	if err != nil {
		return nil, err
	}
	spool.indexed = records
	return records, nil
}

// forgetIndex drops what the spool believed about its directory.
func (spool *Spool) forgetIndex() {
	spool.indexed = nil
}

// noteCommitted amends the listing with what was just published and without
// what was just evicted.
func (spool *Spool) noteCommitted(staged, evictions []Entry) {
	if spool.indexed == nil {
		return
	}
	gone := make(map[uint64]struct{}, len(evictions))
	for _, entry := range evictions {
		gone[entry.Sequence] = struct{}{}
	}
	kept := spool.indexed[:0]
	for _, record := range spool.indexed {
		if _, removed := gone[record.Sequence]; removed {
			continue
		}
		kept = append(kept, record)
	}
	for _, entry := range staged {
		info, err := os.Lstat(spool.stablePath(entry.Sequence))
		if err != nil {
			// It is published and its size is not known. Rather than guess,
			// the spool stops claiming to know its directory.
			spool.indexed = nil
			return
		}
		kept = append(kept, stableRecord{
			Sequence: entry.Sequence, Size: info.Size(),
		})
	}
	sort.Slice(kept, func(one, other int) bool {
		return kept[one].Sequence < kept[other].Sequence
	})
	spool.indexed = kept
}

func (spool *Spool) scanIndex() ([]stableRecord, error) {
	spool.scans++
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
