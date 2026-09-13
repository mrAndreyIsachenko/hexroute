package rootdaemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// A slow observer is not a sleeping machine.
//
// This is the regression for the third defect the tunnel decision's soak found,
// and the only one that fires on a machine that is behaving. On 2026-09-12 the
// first fold after a reinstall cost 32.4 seconds, the loop then waited its
// interval, and ninety-three seconds passed between two observations against a
// threshold of ninety. The cycle named a wake gap and the machine had been awake
// throughout. With authority that decision rebuilds a working tunnel every time
// this daemon is installed.
//
// The two clocks are what make the distinction testable without a real sleep:
// advancing both is a slow runtime, advancing only the wall clock is a sleep.
func TestASlowObserverIsNotASleepingMachine(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	// Two cycles, ninety-three seconds apart on both clocks: the runtime was
	// busy and the machine never stopped.
	first := cycle.Observe(context.Background())
	clocks.advance(93*time.Second, 93*time.Second)
	second := cycle.Observe(context.Background())

	for index, summary := range []Summary{first, second} {
		for _, cause := range summary.Tunnel.Causes {
			if cause == tunnelplan.CauseWakeGap {
				t.Fatalf("cycle %d named a wake gap on a machine that stayed "+
					"awake: %v", index, summary.Tunnel.Causes)
			}
		}
	}
}

// A machine that slept is named, however quick the observer was.
func TestASleepingMachineIsAWakeGap(t *testing.T) {
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle := clockedCycle(t, clocks)

	cycle.Observe(context.Background())
	// Two minutes on the wall, one second of running: the machine slept.
	clocks.advance(2*time.Minute, time.Second)
	summary := cycle.Observe(context.Background())

	named := false
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CauseWakeGap {
			named = true
		}
	}
	if !named {
		t.Fatalf("the machine slept for two minutes and nothing said so: %v",
			summary.Tunnel.Causes)
	}
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
			WakeThreshold: 90 * time.Second, PayloadFailures: 2, LinkFailures: 2,
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
