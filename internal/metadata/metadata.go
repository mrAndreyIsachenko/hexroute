package metadata

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

type UUID string

type Metadata struct {
	EventID        UUID      `json:"event_id"`
	NodeID         UUID      `json:"node_id"`
	SessionID      UUID      `json:"session_id"`
	Sequence       uint64    `json:"sequence"`
	WallClock      time.Time `json:"wall_clock"`
	MonotonicNanos int64     `json:"monotonic_nanos"`
}

type Clock interface {
	WallNow() time.Time
	MonotonicNow() time.Duration
}

type systemClock struct {
	start time.Time
}

type Generator struct {
	mu            sync.Mutex
	nodeID        UUID
	sessionID     UUID
	sequence      uint64
	startMono     time.Duration
	lastMonotonic time.Duration
	clock         Clock
	random        io.Reader
}

var (
	ErrInvalidUUID       = errors.New("invalid UUID")
	ErrSequenceExhausted = errors.New("event sequence exhausted")
	ErrNonMonotonicClock = errors.New("monotonic clock moved backwards")
)

func NewSystemClock() Clock {
	return &systemClock{start: time.Now()}
}

func (clock *systemClock) WallNow() time.Time {
	return time.Now()
}

func (clock *systemClock) MonotonicNow() time.Duration {
	return time.Since(clock.start)
}

func NewUUID(random io.Reader) (UUID, error) {
	if random == nil {
		random = rand.Reader
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return UUID(encoded[:]), nil
}

func ParseUUID(value string) (UUID, error) {
	if len(value) != 36 ||
		value[8] != '-' ||
		value[13] != '-' ||
		value[18] != '-' ||
		value[23] != '-' ||
		strings.ToLower(value) != value {
		return "", ErrInvalidUUID
	}
	compact := strings.ReplaceAll(value, "-", "")
	raw, err := hex.DecodeString(compact)
	if err != nil || len(raw) != 16 {
		return "", ErrInvalidUUID
	}
	if raw[6]>>4 != 4 || raw[8]>>6 != 2 {
		return "", ErrInvalidUUID
	}
	return UUID(value), nil
}

func Validate(value Metadata) error {
	for _, id := range []UUID{value.EventID, value.NodeID, value.SessionID} {
		if _, err := ParseUUID(string(id)); err != nil {
			return err
		}
	}
	if value.Sequence == 0 || value.WallClock.IsZero() || value.MonotonicNanos < 0 {
		return errors.New("invalid event metadata")
	}
	return nil
}

func NewGenerator(
	nodeID UUID,
	initialSequence uint64,
	clock Clock,
	random io.Reader,
) (*Generator, error) {
	if _, err := ParseUUID(string(nodeID)); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = NewSystemClock()
	}
	if random == nil {
		random = rand.Reader
	}
	sessionID, err := NewUUID(random)
	if err != nil {
		return nil, err
	}
	start := clock.MonotonicNow()
	if start < 0 {
		return nil, ErrNonMonotonicClock
	}
	return &Generator{
		nodeID:        nodeID,
		sessionID:     sessionID,
		sequence:      initialSequence,
		startMono:     start,
		lastMonotonic: start,
		clock:         clock,
		random:        random,
	}, nil
}

func (generator *Generator) Next() (Metadata, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()

	if generator.sequence == math.MaxUint64 {
		return Metadata{}, ErrSequenceExhausted
	}
	monotonic := generator.clock.MonotonicNow()
	if monotonic < generator.lastMonotonic || monotonic < generator.startMono {
		return Metadata{}, ErrNonMonotonicClock
	}
	eventID, err := NewUUID(generator.random)
	if err != nil {
		return Metadata{}, err
	}

	generator.sequence++
	generator.lastMonotonic = monotonic
	return Metadata{
		EventID:        eventID,
		NodeID:         generator.nodeID,
		SessionID:      generator.sessionID,
		Sequence:       generator.sequence,
		WallClock:      generator.clock.WallNow().UTC(),
		MonotonicNanos: (monotonic - generator.startMono).Nanoseconds(),
	}, nil
}
