//go:build !darwin && !linux

package policyclock

import "time"

func AwakeNow() (time.Duration, error) {
	return 0, ErrInvalidClock
}
