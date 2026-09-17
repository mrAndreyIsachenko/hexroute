package rootdaemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// swappableProcesses is a tunnel whose process the test replaces.
type swappableProcesses struct {
	current *observe.ProcessObservation
}

func (processes swappableProcesses) Tunnel(context.Context, string) (observe.ProcessObservation, error) {
	return *processes.current, nil
}

func swappableCycle(t *testing.T) (*Cycle, *observe.ProcessObservation, *pairedClocks) {
	t.Helper()
	config, network, _, endpoints := healthyCycleFixtures(t)
	config.TunnelSupervision = &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{
			Interval: 60 * time.Second, WakeThreshold: 180 * time.Second, PayloadFailures: 2, LinkFailures: 2,
		},
		Payload: observe.PayloadEndpoint{Name: "payload", URL: "http://198.51.100.1/", Timeout: time.Second},
	}
	store, err := newTunnelStateStore(filepath.Join(t.TempDir(), "tunnel.json"))
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	current := &observe.ProcessObservation{Running: true, Process: observe.Process{PID: 100}}
	clocks := &pairedClocks{at: time.Unix(1_700_000_000, 0)}
	cycle, err := NewCycle(config, network, swappableProcesses{current: current}, endpoints,
		WithPayloadObserver(traversingPayload{traversed: true}),
		WithTunnelState(store),
		WithCycleClock(clocks.now),
		WithSteadyClock(clocks.steady))
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	return cycle, current, clocks
}

func namesProcessGone(summary Summary) bool {
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CauseProcessGone {
			return true
		}
	}
	return false
}

// A process the owner replaced between two cycles is named, as the owner names
// the loss it restarted on. Measured 2026-09-17: stopped at 11:38:09Z, running
// again at 11:38:32Z, and both cycles around it saw a tunnel running.
func TestAProcessReplacedBetweenCyclesIsGone(t *testing.T) {
	cycle, current, clocks := swappableCycle(t)
	cycle.Observe(context.Background())
	clocks.advance(time.Minute, time.Minute)
	if summary := cycle.Observe(context.Background()); namesProcessGone(summary) {
		t.Fatalf("the same process twice named a loss: %v", summary.Tunnel.Causes)
	}
	current.Process.PID = 200
	clocks.advance(time.Minute, time.Minute)
	summary := cycle.Observe(context.Background())
	if !namesProcessGone(summary) || !summary.Tunnel.Grounds.ProcessReplaced {
		t.Fatalf("a replaced process named %v, grounds %+v", summary.Tunnel.Causes, summary.Tunnel.Grounds)
	}
}

// The first cycle of this runtime has nothing to compare, so an installation
// is not a loss.
func TestAFirstCycleSeesNoReplacement(t *testing.T) {
	cycle, _, _ := swappableCycle(t)
	if summary := cycle.Observe(context.Background()); namesProcessGone(summary) {
		t.Fatalf("the first cycle named a loss: %v", summary.Tunnel.Causes)
	}
}

// A cycle that saw no tunnel keeps the last one it saw, so the replacement is
// still noticed when it appears.
func TestAReplacementAcrossAnAbsenceIsStillSeen(t *testing.T) {
	cycle, current, clocks := swappableCycle(t)
	cycle.Observe(context.Background())
	current.Running, current.Process.PID = false, 0
	clocks.advance(time.Minute, time.Minute)
	if summary := cycle.Observe(context.Background()); !namesProcessGone(summary) {
		t.Fatalf("an absent process named no loss: %v", summary.Tunnel.Causes)
	}
	current.Running, current.Process.PID = true, 300
	clocks.advance(time.Minute, time.Minute)
	summary := cycle.Observe(context.Background())
	if !summary.Tunnel.Grounds.ProcessReplaced {
		t.Fatalf("the process that came back was not seen as another one: %+v", summary.Tunnel.Grounds)
	}
}
