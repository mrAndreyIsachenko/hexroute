package diskusage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A bound that counts the length of records is not a bound on a disk. These
// stores hold one file per record at about 1.25 kilobytes against a
// four-kilobyte allocation unit, so what they occupy is more than three times
// what they contain.
func TestAStoredRecordOccupiesWhatTheFilesystemCharges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	const contents = 1258
	if err := os.WriteFile(path, make([]byte, contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Asserted against what the filesystem itself reports rather than against
	// the record's length, so that an implementation quietly falling back to
	// the length is caught rather than skipped over.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("this platform does not report allocation")
	}
	want := int64(stat.Blocks) * 512
	if got := Occupied(info); got != want {
		t.Fatalf("Occupied = %d, want %d — what the filesystem charges",
			got, want)
	}
	if want <= info.Size() {
		t.Fatalf("a record of %d bytes was charged %d; this test proves "+
			"nothing where the filesystem charges no more than it holds",
			info.Size(), want)
	}
}

func TestNothingStoredOccupiesNothing(t *testing.T) {
	if got := Occupied(nil); got != 0 {
		t.Fatalf("Occupied(nil) = %d, want 0", got)
	}
	if got := WillOccupy(0, 4096); got != 0 {
		t.Fatalf("WillOccupy(0) = %d, want 0", got)
	}
}

// A record not yet written is the one number the filesystem cannot be asked
// for. Counting it at its length would admit one record more than the bound
// meant to, on every append.
func TestARecordAboutToBeWrittenCountsWhatItWillTake(t *testing.T) {
	for _, testCase := range []struct{ length, unit, want int64 }{
		{length: 1, unit: 4096, want: 4096},
		{length: 4096, unit: 4096, want: 4096},
		{length: 4097, unit: 4096, want: 8192},
		{length: 1258, unit: 4096, want: 4096},
		{length: 1258, unit: 0, want: 4096},
	} {
		if got := WillOccupy(testCase.length, testCase.unit); got != testCase.want {
			t.Errorf("WillOccupy(%d, %d) = %d, want %d",
				testCase.length, testCase.unit, got, testCase.want)
		}
	}
}

// A store whose bound depends on this must keep bounding even where the
// question cannot be answered.
func TestAPathThatCannotBeAskedStillAnswers(t *testing.T) {
	if got := AllocationUnit(filepath.Join(t.TempDir(), "nothing here")); got <= 0 {
		t.Fatalf("AllocationUnit of an absent path = %d, want a usable unit", got)
	}
	if got := AllocationUnit(t.TempDir()); got <= 0 {
		t.Fatalf("AllocationUnit = %d, want a usable unit", got)
	}
}
