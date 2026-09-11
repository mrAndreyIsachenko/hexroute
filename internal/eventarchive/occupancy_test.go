package eventarchive

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// A byte bound is a promise about a disk.
//
// These stores hold one file per record at about 1.25 kilobytes against a
// four-kilobyte allocation unit. Counting the length of records made the
// promise wrong by more than three times: on 2026-09-11 the live archive,
// bounded at 256 megabytes, held 96.7 megabytes of records and occupied 315
// megabytes of disk. At its bound it would have taken about 840.
func TestTheBoundCountsWhatTheDiskCharges(t *testing.T) {
	root := t.TempDir()
	archive := openArchive(t, root, newClock(), Options{
		MaxBytes: 64 * 1024 * 1024,
		MaxAge:   30 * 24 * time.Hour,
	})
	if _, err := archive.Append(operational(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	entries, err := archive.index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the archive holds %d records, want 1", len(entries))
	}
	info, err := os.Lstat(archive.stablePath(entries[0].Sequence))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Asserted against what the filesystem itself charges rather than against
	// the record's length, so that an implementation quietly counting the
	// length is caught rather than skipped over.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("this platform does not report allocation")
	}
	want := int64(stat.Blocks) * 512
	if want <= info.Size() {
		t.Fatalf("a record of %d bytes is charged %d; this test proves "+
			"nothing where the filesystem charges no more than it holds",
			info.Size(), want)
	}
	if entries[0].Size != want {
		t.Fatalf("the archive counts %d against its bound and the disk "+
			"charges %d", entries[0].Size, want)
	}
	// Twice, because the archive learns a record's size in two places: when it
	// publishes one, and when it reads its directory. They have to agree, or
	// what the bound counts depends on whether the archive has been restarted.
	archive.forgetIndex()
	walked, err := archive.index()
	if err != nil {
		t.Fatalf("index after forgetting: %v", err)
	}
	if len(walked) != 1 || walked[0] != entries[0] {
		t.Fatalf("published as %+v and read back as %+v", entries, walked)
	}

	size, err := archive.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != entries[0].Size {
		t.Fatalf("the archive reports %d and counts %d against its bound; "+
			"a reader cannot tell which the bound means", size, entries[0].Size)
	}
}

// The window the configuration states has to be the one that applies. Thirty
// days never applied: at twelve megabytes a day the live archive reached its
// byte bound in about twenty-one, so the age was a number nothing ever used.
func TestTheAgeWindowIsSevenDays(t *testing.T) {
	if DefaultMaxAge != 7*24*time.Hour {
		t.Fatalf("the default age window is %v, want 7 days", DefaultMaxAge)
	}
}

// A record too large for the bound is refused. What decides that is what it
// will take once written, not what it contains: a bound of two kilobytes
// cannot hold a record that occupies four, however little the record says.
func TestARecordIsWeighedByWhatItWillTake(t *testing.T) {
	archive := openArchive(t, t.TempDir(), newClock(), Options{
		MaxBytes: 2048,
		MaxAge:   7 * 24 * time.Hour,
	})
	encoded := operational(1)
	// The premise, asserted rather than skipped: a fixture larger than the
	// bound would be refused on its contents and prove nothing about what it
	// occupies.
	if int64(len(encoded)) > 2048 {
		t.Fatalf("the fixture record is %d bytes and would be refused on its "+
			"contents alone; this test needs one smaller than the bound",
			len(encoded))
	}
	if _, err := archive.Append(encoded); err == nil {
		t.Fatalf("a record of %d bytes was accepted into a bound of 2048, "+
			"which cannot hold the block it occupies", len(encoded))
	}
}
