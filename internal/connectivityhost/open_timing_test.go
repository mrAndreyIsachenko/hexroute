package connectivityhost

import (
	"path/filepath"
	"testing"
)

// The total must reach the caller.
//
// It did not. The total was set in a deferred call while the function returned
// its timings by value, so the copy left before the defer ran and every total
// was reported as under a millisecond beside seventeen seconds of parts. The
// live machine said so the first time the instrument was read, which is the
// argument for the instrument and also for this test.
func TestOpeningReportsATotalThatCoversItsParts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	archive := filepath.Join(t.TempDir(), "archive")
	reader, timings, err := OpenTimed(root, "boot-identity-for-timing", archive)
	if err != nil {
		t.Fatalf("OpenTimed: %v", err)
	}
	if reader == nil {
		t.Fatal("OpenTimed returned no reader and no error")
	}
	if timings.Total <= 0 {
		t.Fatal("the total is not reported at all")
	}
	parts := timings.Checkpoints + timings.EventArchive +
		timings.RootJournal + timings.UserJournal + timings.Replay
	if timings.Total < parts {
		t.Fatalf("the total is %v and its parts add to %v; the total does not "+
			"cover the work it is the total of", timings.Total, parts)
	}
}
