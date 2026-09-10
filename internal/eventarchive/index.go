package eventarchive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// stableEntry is what the archive can learn about a stored record without
// opening it: which sequence it holds, and how many bytes it occupies.
//
// Both come from the directory. The sequence is the filename and the size is
// what the filesystem reports, so neither needs the record to be decoded.
type stableEntry struct {
	Sequence uint64
	Size     int64
}

// index lists the stored records without reading any of them.
//
// The listing is taken once and then kept. Reading the directory is O(records
// stored), and an append needs the listing, so re-reading it per append made
// the cost O(records stored x records appended). On 2026-09-11 that was 22
// directory walks of 69,000 and 107,600 entries in one observation cycle, and
// it held the root runtime's operator loop for six to thirteen seconds at a
// time. The earlier fix here removed the decode and left the walk.
//
// What the archive knows about its own directory it now learns once, at the
// first question, and keeps current by amending it whenever it publishes or
// evicts a record. Anything that fails midway drops the listing rather than
// amending it, so the next question pays for a fresh walk and no answer is
// ever built on a directory the archive stopped being sure about.
//
// The trade is deliberate and worth stating: re-reading would notice a record
// added or removed by something other than this archive. Nothing does that —
// the store has one owner and its directory is not writable by anyone else —
// and the fresh walk after any failure is what covers the case where this
// archive itself did not finish.
func (archive *Archive) index() ([]stableEntry, error) {
	if archive.indexed != nil {
		return archive.indexed, nil
	}
	entries, err := archive.walk()
	if err != nil {
		return nil, err
	}
	archive.indexed = entries
	return entries, nil
}

// forgetIndex drops what the archive believed about its directory.
//
// It is called wherever a mutation may have half happened. Costing a walk is
// the right answer there: the alternative is an answer built on a listing that
// stopped being true.
func (archive *Archive) forgetIndex() {
	archive.indexed = nil
}

// noteAppended records a published record in the listing the archive holds.
func (archive *Archive) noteAppended(entry stableEntry) {
	if archive.indexed == nil {
		return
	}
	archive.indexed = append(archive.indexed, entry)
	sort.Slice(archive.indexed, func(one, other int) bool {
		return archive.indexed[one].Sequence < archive.indexed[other].Sequence
	})
}

// noteEvicted removes evicted records from the listing the archive holds.
func (archive *Archive) noteEvicted(records []Record) {
	if archive.indexed == nil || len(records) == 0 {
		return
	}
	gone := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		gone[record.Sequence] = struct{}{}
	}
	kept := archive.indexed[:0]
	for _, entry := range archive.indexed {
		if _, removed := gone[entry.Sequence]; removed {
			continue
		}
		kept = append(kept, entry)
	}
	archive.indexed = kept
}

// walk reads the directory.
//
// It exists because most of what the archive does never looks inside a record.
// Appending needs the next sequence and the total size; reporting size needs the
// total; the age walk needs to know where to start. Decoding forty thousand
// payloads to learn two numbers is the whole of the defect this replaces.
//
// What it still refuses is anything that makes the directory itself
// untrustworthy: a name that is not a record, a symlink, a subdirectory, a
// sequence claimed twice. Those are statements about the archive rather than
// about one record in it, and an archive whose shape is wrong cannot be reasoned
// about at all.
func (archive *Archive) walk() ([]stableEntry, error) {
	archive.walks++
	directoryEntries, err := os.ReadDir(archive.path)
	if err != nil {
		return nil, fmt.Errorf("%w: read directory: %v", ErrArchive, err)
	}

	entries := make([]stableEntry, 0, len(directoryEntries))
	seen := make(map[uint64]struct{}, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		fileName := directoryEntry.Name()
		if !strings.HasSuffix(fileName, stableSuffix) {
			continue
		}
		sequence, ok := parseStableName(fileName)
		if !ok || directoryEntry.IsDir() ||
			directoryEntry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: %s is not an archive record",
				ErrArchive, fileName)
		}
		if _, duplicate := seen[sequence]; duplicate {
			return nil, fmt.Errorf("%w: sequence %d is claimed twice",
				ErrArchive, sequence)
		}
		info, err := os.Lstat(filepath.Join(archive.path, fileName))
		if err != nil {
			return nil, fmt.Errorf("%w: inspect record: %v", ErrArchive, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s is not an archive record",
				ErrArchive, fileName)
		}
		seen[sequence] = struct{}{}
		entries = append(entries, stableEntry{
			Sequence: sequence, Size: info.Size(),
		})
	}
	sort.Slice(entries, func(one, other int) bool {
		return entries[one].Sequence < entries[other].Sequence
	})
	return entries, nil
}

// parseStableName reads the sequence out of a record's filename.
func parseStableName(fileName string) (uint64, bool) {
	trimmed := strings.TrimSuffix(fileName, stableSuffix)
	if len(trimmed) != nameWidth {
		return 0, false
	}
	var sequence uint64
	for _, digit := range trimmed {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		sequence = sequence*10 + uint64(digit-'0')
	}
	return sequence, true
}

func indexTotalSize(entries []stableEntry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Size
	}
	return total
}
