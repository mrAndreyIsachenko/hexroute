//go:build darwin

package policyclock

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinContinuousClockIncludesSystemSleep(t *testing.T) {
	if continuousClockID != unix.CLOCK_MONOTONIC {
		t.Fatalf("continuous clock ID = %d, want CLOCK_MONOTONIC", continuousClockID)
	}
}
