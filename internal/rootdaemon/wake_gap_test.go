package rootdaemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// A gap the steady clock did not see is still a wake gap.
//
// On 2026-09-14 the machine slept for 136, 997 and 394 seconds while idle, the
// clock that should have stopped kept running, and every cycle after recorded a
// sleep of zero. The runtime this rule reproduces rebuilt after every sleep it
// held the tunnel for, by the wall clock alone. So the gap is the wall clock's,
// and a slow cycle makes one too, as a slow tick does there.
func TestAGapTheSteadyClockDidNotSeeIsAWakeGap(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	cycle.Observe(context.Background())
	clocks.advance(3*time.Minute, 3*time.Minute)
	summary := cycle.Observe(context.Background())

	if !namesWakeGap(summary) {
		t.Fatalf("three minutes between cycles and no wake gap: %v", summary.Tunnel.Causes)
	}
	if summary.Tunnel.Grounds.TickGap != 3*time.Minute || summary.Tunnel.Grounds.Slept != 0 {
		t.Fatalf("grounds: tick gap %s, slept %s; want 3m0s and 0s",
			summary.Tunnel.Grounds.TickGap, summary.Tunnel.Grounds.Slept)
	}
}

// A sleep the steady clock did see is a wake gap, and is recorded as one.
func TestASleepingMachineIsAWakeGap(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	cycle.Observe(context.Background())
	clocks.advance(3*time.Minute, time.Second)
	summary := cycle.Observe(context.Background())

	if !namesWakeGap(summary) {
		t.Fatalf("the machine slept for three minutes and nothing said so: %v",
			summary.Tunnel.Causes)
	}
	if summary.Tunnel.Grounds.Slept != 3*time.Minute-time.Second {
		t.Fatalf("slept recorded %s, want 2m59s", summary.Tunnel.Grounds.Slept)
	}
}

// A second short of the threshold is not a gap, however much of it was sleep.
func TestAGapShortOfTheThresholdIsNotAWakeGap(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	cycle.Observe(context.Background())
	clocks.advance(179*time.Second, time.Second)
	summary := cycle.Observe(context.Background())

	if namesWakeGap(summary) {
		t.Fatalf("179 seconds named a wake at a threshold of 180: %v", summary.Tunnel.Causes)
	}
}

func namesWakeGap(summary Summary) bool {
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CauseWakeGap {
			return true
		}
	}
	return false
}

// The first cycle has nothing to measure against.
func TestAFirstCycleReportsNoSleep(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	summary := cycle.Observe(context.Background())
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CauseWakeGap {
			t.Fatalf("the first cycle invented a gap: %v", summary.Tunnel.Causes)
		}
	}
}

// clockedCycle is a supervised cycle whose two clocks the test drives.
func clockedCycle(t *testing.T, clocks *pairedClocks) *Cycle {
	t.Helper()
	config, network, processes, endpoints := healthyCycleFixtures(t)
	config.TunnelSupervision = &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{
			Interval: 60 * time.Second, WakeThreshold: 180 * time.Second, PayloadFailures: 2, LinkFailures: 2,
		},
		Payload: observe.PayloadEndpoint{
			Name: "payload", URL: "http://198.51.100.1/", Timeout: time.Second,
		},
	}
	store, err := newTunnelStateStore(filepath.Join(t.TempDir(), "tunnel.json"))
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	cycle, err := NewCycle(config, network, processes, endpoints,
		WithPayloadObserver(traversingPayload{traversed: true}),
		WithTunnelState(store),
		WithCycleClock(clocks.now),
		WithSteadyClock(clocks.steady))
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	return cycle
}

// pairedClocks is a wall clock and a clock that stops when the machine does.
type pairedClocks struct {
	at  time.Time
	ran time.Duration
}

func (clocks *pairedClocks) advance(wall, running time.Duration) {
	clocks.at = clocks.at.Add(wall)
	clocks.ran += running
}

func (clocks *pairedClocks) now() time.Time        { return clocks.at }
func (clocks *pairedClocks) steady() time.Duration { return clocks.ran }

// The wall clock must carry no monotonic reading.
//
// time.Time from time.Now carries both, and Sub prefers the monotonic one when
// both operands have it. A wall clock that kept its monotonic reading would make
// the divergence identically zero, and the wake gap would never hold however
// long the machine slept — the opposite of the defect this change corrects, and
// invisible to every test that drives a clock it made up.
func TestTheWallClockCarriesNoMonotonicReading(t *testing.T) {
	at := wallClock()
	if at != at.Round(0) {
		t.Fatal("the wall clock carries a monotonic reading, so the difference " +
			"between it and the steady clock can never show a sleep")
	}
	// And the steady clock must be the one that does carry it, or there is
	// nothing to diverge from.
	steady := steadyClock()
	first := steady()
	if second := steady(); second < first {
		t.Fatalf("the steady clock went backwards: %v then %v", first, second)
	}
}
