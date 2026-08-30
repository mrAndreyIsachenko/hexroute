//go:build darwin

package policyclock

import (
	"time"

	"golang.org/x/sys/unix"
)

// awakeClockID excludes time the system spent asleep. On Darwin
// CLOCK_MONOTONIC keeps counting through sleep, and CLOCK_UPTIME_RAW is the
// one that stops.
const awakeClockID = unix.CLOCK_UPTIME_RAW

// AwakeNow excludes time spent in system sleep on Darwin.
//
// Read beside ContinuousNow it says how long a host was asleep without asking
// anything to announce a wake: the difference between the two is the sleep.
// That matters because eligible time is awake, observing time, and a host that
// slept did not fail to observe — it was not there to observe.
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
