//go:build linux

package policyclock

import (
	"time"

	"golang.org/x/sys/unix"
)

const continuousClockID = unix.CLOCK_BOOTTIME

// ContinuousNow includes time spent in system suspend on Linux.
func ContinuousNow() (time.Duration, error) {
	var sample unix.Timespec
	if err := unix.ClockGettime(continuousClockID, &sample); err != nil {
		return 0, ErrInvalidClock
	}
	nanoseconds := sample.Nano()
	if nanoseconds < 0 {
		return 0, ErrInvalidClock
	}
	return time.Duration(nanoseconds), nil
}
