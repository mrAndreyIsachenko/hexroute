//go:build linux

package policyclock

import (
	"time"

	"golang.org/x/sys/unix"
)

// awakeClockID excludes time the system spent suspended. On Linux
// CLOCK_BOOTTIME keeps counting through suspend, and CLOCK_MONOTONIC stops.
const awakeClockID = unix.CLOCK_MONOTONIC

// AwakeNow excludes time spent in system suspend on Linux.
func AwakeNow() (time.Duration, error) {
	var sample unix.Timespec
	if err := unix.ClockGettime(awakeClockID, &sample); err != nil {
		return 0, ErrInvalidClock
	}
	nanoseconds := sample.Nano()
	if nanoseconds < 0 {
		return 0, ErrInvalidClock
	}
	return time.Duration(nanoseconds), nil
}
