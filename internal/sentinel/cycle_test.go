package sentinel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/heartbeat"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type fakeHeartbeatReader struct {
	record heartbeat.Record
	err    error
}

func (reader fakeHeartbeatReader) Load(string) (heartbeat.Record, error) {
	return reader.record, reader.err
}

type fakeEndpointObserver struct {
	ready bool
	err   error
}

func (observer fakeEndpointObserver) Endpoint(
	context.Context,
	observe.Endpoint,
) (observe.ReadinessObservation, error) {
	return observe.ReadinessObservation{
		Name:  "legacy-twilight",
		Ready: observer.ready,
	}, observer.err
}

func sentinelRuntimeFixture(t *testing.T) RuntimeConfig {
	t.Helper()
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	return config
}

func TestCycleUsesIndependentHeartbeatAndDataPathAdapters(t *testing.T) {
	config := sentinelRuntimeFixture(t)
	tracker, _ := NewTracker(30)
	cycle, err := NewCycle(
		config,
		fakeHeartbeatReader{record: heartbeat.Record{
			Schema:        heartbeat.Schema,
			Sequence:      10,
			PID:           123,
			MonotonicTick: 20,
		}},
		fakeEndpointObserver{ready: true},
		tracker,
	)
	if err != nil {
		t.Fatalf("NewCycle() error: %v", err)
	}

	summary := cycle.Observe(context.Background(), 0)
	if summary.Failures != 0 ||
		!summary.HeartbeatFound ||
		!summary.DataPathReady ||
		summary.Decision.EvidenceReady ||
		summary.Decision.Action != ActionNone {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestMalformedHeartbeatCannotBecomeRestartEvidence(t *testing.T) {
	config := sentinelRuntimeFixture(t)
	tracker, _ := NewTracker(30)
	cycle, _ := NewCycle(
		config,
		fakeHeartbeatReader{err: heartbeat.ErrInvalidHeartbeat},
		fakeEndpointObserver{ready: false},
		tracker,
	)

	summary := cycle.Observe(context.Background(), 100)
	if summary.Failures != 1 ||
		summary.Decision.EvidenceReady ||
		summary.Decision.Action != ActionNone {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestProbeAdapterErrorCannotProduceRestartEvidence(t *testing.T) {
	config := sentinelRuntimeFixture(t)
	tracker, _ := NewTracker(30)
	cycle, _ := NewCycle(
		config,
		fakeHeartbeatReader{record: heartbeat.Record{
			Schema:        heartbeat.Schema,
			Sequence:      10,
			PID:           123,
			MonotonicTick: 20,
		}},
		fakeEndpointObserver{err: errors.New("invalid probe")},
		tracker,
	)

	first := cycle.Observe(context.Background(), 0)
	second := cycle.Observe(context.Background(), 30)
	if first.Failures != 1 ||
		first.Decision.EvidenceReady ||
		second.Failures != 1 ||
		second.Decision.EvidenceReady ||
		second.Decision.Action != ActionNone {
		t.Fatalf("Observe() first=%+v second=%+v", first, second)
	}
}
