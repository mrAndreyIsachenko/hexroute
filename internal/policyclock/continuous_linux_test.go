//go:build linux

package policyclock

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxContinuousClockIncludesSystemSuspend(t *testing.T) {
	if continuousClockID != unix.CLOCK_BOOTTIME {
		t.Fatalf("continuous clock ID = %d, want CLOCK_BOOTTIME", continuousClockID)
	}
}
