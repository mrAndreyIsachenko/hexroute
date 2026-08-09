//go:build !darwin && !linux

package policyclock

import "time"

func ContinuousNow() (time.Duration, error) {
	return 0, ErrInvalidClock
}
