package ingressagent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

type stubObserver struct {
	observation Observation
	err         error
	calls       int
}

func (observer *stubObserver) Observe(context.Context) (Observation, error) {
	observer.calls++
	return observer.observation, observer.err
}

func provingAgent(t *testing.T) (*Agent, *stubFetcher, *stubApplier, *Store, func(time.Time)) {
	t.Helper()
	private := operatorKey(7)
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	fetcher := &stubFetcher{objects: map[string][]byte{
		currentKey(): version(t, private, "2026-09-06.1", at, []byte(`{"inbounds":[{"port":443}]}`)),
	}}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))
	clock := at.Add(time.Minute)
	agent.now = func() time.Time { return clock }
	set := func(now time.Time) { clock = now }
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second version, so that there is something retained to return to.
	fetcher.objects[currentKey()] = version(t, private, "2026-09-07.1",
		at.Add(time.Hour), []byte(`{"inbounds":[{"port":8443}]}`))
	set(at.Add(2 * time.Hour))
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	return agent, fetcher, applier, store, set
}

// Proof is the host saying, in a signature, that this exact version is
// serving. Nothing weaker counts, and the two weaker things that look like
// proof are exactly the ones this rules out.
func TestOnlyTheHeartbeatForThatGenerationProvesAVersion(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		observation Observation
		err         error
		outcome     Outcome
		reason      string
	}{
		{
			name:        "the applied generation, healthy",
			observation: Observation{Generation: "2026-09-07.1", Healthy: true},
			outcome:     OutcomeProven,
		},
		{
			name:        "applied cleanly and the host is still on the old version",
			observation: Observation{Generation: "2026-09-06.1", Healthy: true},
			outcome:     OutcomePending,
			reason:      "generation_not_running",
		},
		{
			name:        "reachable and answering, and the transport does not work",
			observation: Observation{Generation: "2026-09-07.1", Healthy: false},
			outcome:     OutcomePending,
			reason:      "transport_unhealthy",
		},
		{
			name:    "no heartbeat at all",
			err:     errors.New("connection refused"),
			outcome: OutcomePending,
			reason:  "heartbeat_unavailable",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _, _, _, _ := provingAgent(t)
			observer := &stubObserver{observation: testCase.observation, err: testCase.err}
			result, err := agent.ProveOrReturn(context.Background(), observer, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != testCase.outcome || result.Reason != testCase.reason {
				t.Fatalf("result = %+v", result)
			}
			if observer.calls != 1 {
				t.Fatalf("observations: %d", observer.calls)
			}
		})
	}
}

// When the window passes with no proof, the host goes back to the version it
// was running, and says why. Leaving it on something nobody has evidence for
// is the outcome this exists to prevent.
func TestAnUnprovenVersionIsReturnedFromWithTheReason(t *testing.T) {
	agent, fetcher, applier, store, setNow := provingAgent(t)
	callsBefore := fetcher.calls
	appliedAt, ok, err := store.AppliedAt()
	if err != nil || !ok {
		t.Fatal("no application time was recorded")
	}
	setNow(appliedAt.Add(2 * time.Hour))

	observer := &stubObserver{observation: Observation{
		Generation: "2026-09-07.1", Healthy: false,
	}}
	result, err := agent.ProveOrReturn(context.Background(), observer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeReturned || result.Reason != "transport_unhealthy" ||
		result.VersionLabel != "2026-09-06.1" {
		t.Fatalf("result = %+v", result)
	}
	if fetcher.calls != callsBefore {
		t.Fatal("returning reached the network")
	}
	if applier.generations[len(applier.generations)-1] != "2026-09-06.1" {
		t.Fatalf("generations: %v", applier.generations)
	}
	applied, _, ok, err := store.Applied()
	if err != nil || !ok || applied.Statement.VersionLabel != "2026-09-06.1" {
		t.Fatal("the return is not what the host now records as applied")
	}
	recorded, ok, err := store.LastResult()
	if err != nil || !ok || recorded.Reason != "transport_unhealthy" ||
		recorded.Outcome != OutcomeReturned {
		t.Fatalf("recorded = %+v", recorded)
	}
}

// The first version a host ever applies has nothing behind it. Refusing to
// pretend it proved is the whole of what can be done.
func TestAnUnprovenFirstVersionIsRecordedRatherThanReturnedFrom(t *testing.T) {
	private := operatorKey(7)
	at := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	fetcher := &stubFetcher{objects: map[string][]byte{
		currentKey(): version(t, private, "2026-09-06.1", at, []byte(`{"inbounds":[]}`)),
	}}
	applier := &stubApplier{}
	agent, store := newAgent(t, fetcher, applier, private.Public().(ed25519.PublicKey))
	now := at.Add(time.Minute)
	agent.now = func() time.Time { return now }
	if _, err := agent.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = at.Add(4 * time.Hour)

	observer := &stubObserver{err: errors.New("connection refused")}
	result, err := agent.ProveOrReturn(context.Background(), observer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeRefused || result.Reason != "heartbeat_unavailable" {
		t.Fatalf("result = %+v", result)
	}
	if len(applier.applied) != 1 {
		t.Fatalf("applications: %d", len(applier.applied))
	}
	if _, _, retained, err := store.Retained(); err != nil || retained {
		t.Fatal("something was retained that never existed")
	}
}

func TestProvingNeedsAnObserverAWindowAndAnAppliedVersion(t *testing.T) {
	agent, _, _, _, _ := provingAgent(t)
	if _, err := agent.ProveOrReturn(context.Background(), nil, time.Hour); !errors.Is(err, ErrAgent) {
		t.Fatal("proved without an observer")
	}
	if _, err := agent.ProveOrReturn(context.Background(), &stubObserver{}, 0); !errors.Is(err, ErrAgent) {
		t.Fatal("proved without a window")
	}

	empty, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bare, err := New(&stubFetcher{}, &stubApplier{}, empty,
		operatorKey(7).Public().(ed25519.PublicKey), agentTarget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.ProveOrReturn(context.Background(), &stubObserver{}, time.Hour); !errors.Is(err, ErrAgent) {
		t.Fatal("proved with nothing applied")
	}
}
