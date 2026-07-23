package metadata

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"
)

type fakeClock struct {
	wall      time.Time
	monotonic time.Duration
}

func (clock *fakeClock) WallNow() time.Time {
	return clock.wall
}

func (clock *fakeClock) MonotonicNow() time.Duration {
	return clock.monotonic
}

func TestUUIDIsRFC4122Version4(t *testing.T) {
	id, err := NewUUID(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatalf("NewUUID() error = %v", err)
	}
	if id != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("NewUUID() = %q", id)
	}
	if parsed, err := ParseUUID(string(id)); err != nil || parsed != id {
		t.Fatalf("ParseUUID(%q) = %q, %v", id, parsed, err)
	}
}

func TestGeneratorProducesStableNodeSessionAndOrderedMetadata(t *testing.T) {
	nodeID := UUID("11111111-1111-4111-8111-111111111111")
	randomBytes := make([]byte, 16*3)
	randomBytes[16] = 1
	randomBytes[32] = 2
	random := bytes.NewReader(randomBytes)
	clock := &fakeClock{
		wall:      time.Date(2026, time.July, 23, 18, 0, 0, 0, time.FixedZone("MSK", 3*60*60)),
		monotonic: 10 * time.Second,
	}
	generator, err := NewGenerator(nodeID, 40, clock, random)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	first, err := generator.Next()
	if err != nil {
		t.Fatalf("Next(first) error = %v", err)
	}
	clock.wall = clock.wall.Add(-time.Hour)
	clock.monotonic += 250 * time.Millisecond
	second, err := generator.Next()
	if err != nil {
		t.Fatalf("Next(second) error = %v", err)
	}

	if first.NodeID != nodeID || second.NodeID != nodeID ||
		first.SessionID != second.SessionID {
		t.Fatalf("identity changed: first=%+v second=%+v", first, second)
	}
	if first.EventID == second.EventID {
		t.Fatal("event UUID was reused")
	}
	if first.Sequence != 41 || second.Sequence != 42 {
		t.Fatalf("sequences = %d, %d; want 41, 42", first.Sequence, second.Sequence)
	}
	if first.MonotonicNanos != 0 || second.MonotonicNanos != int64(250*time.Millisecond) {
		t.Fatalf(
			"monotonic offsets = %d, %d",
			first.MonotonicNanos,
			second.MonotonicNanos,
		)
	}
	if !second.WallClock.Before(first.WallClock) {
		t.Fatal("test wall clock did not move backwards")
	}
}

func TestGeneratorRejectsClockRollbackAndSequenceOverflow(t *testing.T) {
	nodeID := UUID("11111111-1111-4111-8111-111111111111")
	clock := &fakeClock{
		wall:      time.Now(),
		monotonic: time.Second,
	}
	generator, err := NewGenerator(nodeID, 0, clock, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	clock.monotonic = 500 * time.Millisecond
	if _, err := generator.Next(); !errors.Is(err, ErrNonMonotonicClock) {
		t.Fatalf("Next(clock rollback) error = %v, want %v", err, ErrNonMonotonicClock)
	}

	overflow, err := NewGenerator(
		nodeID,
		math.MaxUint64,
		&fakeClock{wall: time.Now(), monotonic: time.Second},
		bytes.NewReader(make([]byte, 16)),
	)
	if err != nil {
		t.Fatalf("NewGenerator(overflow) error = %v", err)
	}
	if _, err := overflow.Next(); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("Next(sequence overflow) error = %v, want %v", err, ErrSequenceExhausted)
	}
}

func TestParseUUIDRejectsNonCanonicalValues(t *testing.T) {
	values := []string{
		"",
		"11111111-1111-1111-8111-111111111111",
		"11111111-1111-4111-1111-111111111111",
		"11111111-1111-4111-8111-11111111111Z",
	}
	for _, value := range values {
		if _, err := ParseUUID(value); !errors.Is(err, ErrInvalidUUID) {
			t.Fatalf("ParseUUID(%q) error = %v, want %v", value, err, ErrInvalidUUID)
		}
	}
}
