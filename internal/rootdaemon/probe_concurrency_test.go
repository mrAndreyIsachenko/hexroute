package rootdaemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

// gatheringObserver answers only once every probe has arrived.
//
// It is how concurrency is asserted without a stopwatch. A timed assertion
// measures the machine and a threshold tuned until it passes measures the
// threshold; this measures the property. Taken together the probes all arrive
// and the gate opens. Taken in turn the first waits for arrivals that cannot
// come, and the test fails on its own deadline saying so.
type gatheringObserver struct {
	expected int
	mu       sync.Mutex
	arrived  int
	gate     chan struct{}
	once     sync.Once
	ready    map[string]bool
}

func newGatheringObserver(expected int, ready map[string]bool) *gatheringObserver {
	return &gatheringObserver{
		expected: expected, gate: make(chan struct{}), ready: ready,
	}
}

func (observer *gatheringObserver) Endpoint(
	ctx context.Context,
	endpoint observe.Endpoint,
) (observe.ReadinessObservation, error) {
	observer.mu.Lock()
	observer.arrived++
	arrived := observer.arrived
	observer.mu.Unlock()
	if arrived == observer.expected {
		observer.once.Do(func() { close(observer.gate) })
	}
	select {
	case <-observer.gate:
	case <-time.After(2 * time.Second):
		return observe.ReadinessObservation{}, fmt.Errorf(
			"probe for %s waited alone: %d of %d probes had started",
			endpoint.Name, arrived, observer.expected)
	case <-ctx.Done():
		return observe.ReadinessObservation{}, ctx.Err()
	}
	return observe.ReadinessObservation{
		Name: endpoint.Name, Ready: observer.ready[endpoint.Name],
	}, nil
}

// The cycle holds the operator socket until it finishes, so what it spends is
// what a caller waits. On 2026-09-11, after the archive and the spool stopped
// listing their directories, three probes taken one after another at about 0.7
// seconds each were the whole of what remained.
func TestTheCycleWaitsForItsProbesTogether(t *testing.T) {
	config, network, processes, _ := healthyCycleFixtures(t)
	observer := newGatheringObserver(len(config.Endpoints), map[string]bool{
		"outer-ready":    true,
		"normal-codex":   false,
		"twilight-codex": true,
	})
	if len(config.Endpoints) < 2 {
		t.Fatalf("the fixture has %d endpoints; this proves nothing below two",
			len(config.Endpoints))
	}
	cycle, err := NewCycle(config, network, processes, observer)
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	summary := cycle.Observe(context.Background())
	if summary.Observed.ReadinessError != nil {
		t.Fatalf("the probes did not run together: %v",
			summary.Observed.ReadinessError)
	}
	if !summary.OuterReady {
		t.Fatal("the outer path was not read as ready")
	}
}

// refusingObserver fails a named endpoint and answers the rest, after a delay
// that makes the failing one finish last if order followed completion.
type refusingObserver struct {
	failing string
	slow    string
	ready   map[string]bool
}

func (observer refusingObserver) Endpoint(
	_ context.Context,
	endpoint observe.Endpoint,
) (observe.ReadinessObservation, error) {
	if endpoint.Name == observer.slow {
		time.Sleep(50 * time.Millisecond)
	}
	if endpoint.Name == observer.failing || endpoint.Name == observer.slow {
		return observe.ReadinessObservation{}, errors.New(endpoint.Name)
	}
	return observe.ReadinessObservation{
		Name: endpoint.Name, Ready: observer.ready[endpoint.Name],
	}, nil
}

// Which failure the cycle records must be the one configuration order leaves,
// not the one that happened to finish last. Concurrency is allowed to change
// when the cycle waits and nothing about what it concludes.
func TestTheRecordedFailureFollowsConfigurationOrder(t *testing.T) {
	config, network, processes, _ := healthyCycleFixtures(t)
	names := make([]string, 0, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		names = append(names, endpoint.Endpoint.Name)
	}
	if len(names) < 2 {
		t.Fatalf("the fixture has %d endpoints; this proves nothing below two",
			len(names))
	}
	// The first configured endpoint fails at once and the last fails slowly.
	// In sequence the last one's error is the one left standing, so that is
	// what concurrency must also leave.
	cycle, err := NewCycle(config, network, processes, refusingObserver{
		failing: names[0], slow: names[len(names)-1],
	})
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	summary := cycle.Observe(context.Background())
	if summary.Observed.ReadinessError == nil {
		t.Fatal("two probes failed and nothing was recorded")
	}
	if got := summary.Observed.ReadinessError.Error(); got != names[len(names)-1] {
		t.Fatalf("recorded %q, want %q — the last in configuration order",
			got, names[len(names)-1])
	}
}
