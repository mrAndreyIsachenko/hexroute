package policyclock

import (
	"errors"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const DefaultMaxWallSkew = 2 * time.Minute

type Sample struct {
	WallUTC     time.Time
	MonotonicNS int64
}

type LeaseSample struct {
	MonotonicNS int64
	BootID      metadata.UUID
}

type Guard struct {
	mu                  sync.Mutex
	maxWallSkew         time.Duration
	hasPrevious         bool
	previousWall        time.Time
	previousMonotonicNS int64
}

var (
	ErrClockAnomaly      = errors.New("policy clock anomaly")
	ErrInvalidClock      = errors.New("invalid policy clock")
	ErrLeaseExpired      = errors.New("action lease expired")
	ErrLeaseBootMismatch = errors.New("action lease boot mismatch")
	ErrInvalidLeaseTime  = errors.New("invalid action lease time")
)

func NewGuard(maxWallSkew time.Duration) (*Guard, error) {
	if maxWallSkew <= 0 || maxWallSkew > 10*time.Minute {
		return nil, ErrInvalidClock
	}
	return &Guard{maxWallSkew: maxWallSkew}, nil
}

func (guard *Guard) Observe(sample Sample) error {
	if guard == nil || sample.WallUTC.IsZero() || sample.MonotonicNS < 0 {
		return ErrInvalidClock
	}
	wall := sample.WallUTC.UTC()
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if !guard.hasPrevious {
		guard.hasPrevious = true
		guard.previousWall = wall
		guard.previousMonotonicNS = sample.MonotonicNS
		return nil
	}
	if sample.MonotonicNS < guard.previousMonotonicNS {
		return ErrClockAnomaly
	}
	elapsed := time.Duration(sample.MonotonicNS - guard.previousMonotonicNS)
	expected := guard.previousWall.Add(elapsed)
	skew := wall.Sub(expected)
	if skew < -guard.maxWallSkew || skew > guard.maxWallSkew {
		return ErrClockAnomaly
	}
	guard.previousWall = wall
	guard.previousMonotonicNS = sample.MonotonicNS
	return nil
}

func ValidatePendingLease(lease policy.ActionLease, sample LeaseSample) error {
	if lease.Validate() != nil || lease.Status != policy.LeasePending || sample.MonotonicNS < 0 {
		return ErrInvalidLeaseTime
	}
	if _, err := metadata.ParseUUID(string(sample.BootID)); err != nil {
		return ErrInvalidLeaseTime
	}
	if sample.BootID != lease.BootID {
		return ErrLeaseBootMismatch
	}
	if sample.MonotonicNS < lease.IssuedMonotonicNS {
		return ErrClockAnomaly
	}
	if sample.MonotonicNS >= lease.ExpiresMonotonicNS {
		return ErrLeaseExpired
	}
	return nil
}
