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
func (archive *Archive) index() ([]stableEntry, error) {
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
