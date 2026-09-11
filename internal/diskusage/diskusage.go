// Package diskusage answers how much room a stored record actually takes.
//
// It exists because a byte bound that counts the length of records promises
// something the disk does not honour. These stores hold one file per record at
// about 1.25 kilobytes against a four-kilobyte allocation unit, so on
// 2026-09-11 an archive bounded at 256 megabytes held 96.7 megabytes of records
// and occupied 315 megabytes of disk. At its bound it would have occupied about
// 840, and the two journals beside it another 660 — a gigabyte and a half where
// the configuration said 456 megabytes.
//
// A bound is a promise about a disk. This makes it one.
package diskusage

import (
	"os"
	"syscall"
)

// blockUnit is what st_blocks counts in. It is fixed by the stat interface
// rather than by the filesystem, whatever the filesystem's own allocation unit
// turns out to be.
const blockUnit = 512

// DefaultAllocationUnit is used for a record not yet written, when the
// filesystem has not been asked. Four kilobytes is what APFS and every
// filesystem this runs on allocate in.
const DefaultAllocationUnit int64 = 4096

// Occupied reports what a stored file takes from the disk, which is what the
// filesystem charges rather than what the record contains.
//
// A filesystem that does not report allocation falls back to the file's length.
// Reporting nothing would make a bound silently stop bounding, and reporting
// the length is what every caller did before this existed.
func Occupied(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Blocks < 0 {
		return info.Size()
	}
	occupied := int64(stat.Blocks) * blockUnit
	if occupied < info.Size() {
		// A compressed or inlined file occupies less than it contains. The
		// bound is about the disk, so the disk's number is the right one, but
		// zero blocks for a file with contents is a filesystem answering a
		// question it does not track.
		if occupied == 0 {
			return info.Size()
		}
	}
	return occupied
}

// WillOccupy reports what a record of this length will take once written.
//
// It rounds up to the allocation unit, because a record about to be written is
// the one number the filesystem cannot yet be asked for, and a bound that
// counted it at its length would admit one record more than it meant to on
// every append.
func WillOccupy(length, unit int64) int64 {
	if unit <= 0 {
		unit = DefaultAllocationUnit
	}
	if length <= 0 {
		return 0
	}
	return ((length + unit - 1) / unit) * unit
}

// AllocationUnit reports what the filesystem holding a path allocates in.
//
// A path it cannot ask about answers the default rather than zero: a store
// whose bound depends on this should keep bounding.
func AllocationUnit(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil || stat.Bsize <= 0 {
		return DefaultAllocationUnit
	}
	return int64(stat.Bsize)
}
