package userdaemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

func observedEvidence() Evidence {
	return Evidence{
		Reached: true,
		Session: userobserve.SessionObservation{State: userobserve.SessionActive},
		Profile: userobserve.ProfileObservation{
			Found: true, State: userobserve.ProfileActive, HasClientAddress: true,
		},
		Service: userobserve.ServiceObservation{Running: true},
	}
}

// The publisher sends facts for exactly the components the user domain owns,
// in its own domain, and nothing else.
func TestPublisherSendsOnlyItsOwnComponents(t *testing.T) {
	publisher, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", filepath.Join(t.TempDir(), "connectivity-stream.json"))
	if err != nil || publisher == nil {
		t.Fatalf("newFactPublisher: %v", err)
	}
	var sent *ipc.PublishConnectivityFactsRequest
	publisher.roundTrip = func(
		_ context.Context, _ string, request ipc.Request,
	) (ipc.Response, error) {
		sent = request.PublishConnectivityFacts
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID,
			PublishConnectivityFacts: &ipc.PublishConnectivityFactsResult{Accepted: 2},
		}, nil
	}
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if sent == nil {
		t.Fatal("nothing was published")
	}
	if sent.Domain != policy.DomainUser {
		t.Fatalf("domain %q, want user", sent.Domain)
	}
	if len(sent.Facts) != len(userComponents()) {
		t.Fatalf("%d facts, want %d", len(sent.Facts), len(userComponents()))
	}
	owned := map[connectivity.Component]bool{}
	for _, raw := range sent.Facts {
		fact, err := connectivity.Decode(raw)
		if err != nil {
			t.Fatalf("root could not decode what was sent: %v", err)
		}
		if fact.Domain != policy.DomainUser {
			t.Fatalf("fact for %s claims domain %q", fact.Component, fact.Domain)
		}
		owned[fact.Component] = true
	}
	for _, component := range userComponents() {
		if !owned[component] {
			t.Fatalf("%s was not published", component)
		}
	}
}

// The first publication of a boot restates both components in full, because a
// partial first answer would be mistaken for a whole one.
func TestFirstPublicationIsABaseline(t *testing.T) {
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", filepath.Join(t.TempDir(), "connectivity-stream.json"))
	var first []json.RawMessage
	publisher.roundTrip = func(
		_ context.Context, _ string, request ipc.Request,
	) (ipc.Response, error) {
		if first == nil {
			first = request.PublishConnectivityFacts.Facts
		}
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID,
			PublishConnectivityFacts: &ipc.PublishConnectivityFactsResult{Accepted: 2},
		}, nil
	}
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for _, raw := range first {
		fact, err := connectivity.Decode(raw)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !fact.Baseline {
			t.Fatalf("%s was not published as a baseline", fact.Component)
		}
	}
}

// Root being unreachable costs a publication, not the daemon's own work. The
// next cycle republishes; a fact held back and sent later would describe a
// moment that has passed.
func TestUnreachableRootDoesNotStopTheDaemon(t *testing.T) {
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", filepath.Join(t.TempDir(), "connectivity-stream.json"))
	publisher.roundTrip = func(
		_ context.Context, _ string, request ipc.Request,
	) (ipc.Response, error) {
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID,
			Error: ipc.ErrorPrecondition,
		}, nil
	}
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("a refused publication stopped the loop: %v", err)
	}
	if publisher.baseline {
		t.Fatal("a refused publication was recorded as delivered")
	}
}

// A cycle that observed nothing publishes nothing: root then lets the user
// components go stale on their own evidence, which is true.
func TestUnreachedCyclePublishesNothing(t *testing.T) {
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", filepath.Join(t.TempDir(), "connectivity-stream.json"))
	called := false
	publisher.roundTrip = func(
		context.Context, string, ipc.Request,
	) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}
	if err := publisher.Publish(context.Background(), Evidence{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if called {
		t.Fatal("an unreached cycle published anyway")
	}
}

// Without a root socket the daemon runs the path it ran before this existed.
func TestNoRootSocketDisablesPublication(t *testing.T) {
	publisher, err := newFactPublisher("boot-0000000000000000", "", filepath.Join(t.TempDir(), "connectivity-stream.json"))
	if err != nil {
		t.Fatalf("newFactPublisher: %v", err)
	}
	if publisher != nil {
		t.Fatal("a daemon without a root socket built a publisher")
	}
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("a disabled publisher returned an error: %v", err)
	}
}

// A source sequence numbers the source, and only the source can continue it.
// A restart that began again at zero would publish facts entirely behind the
// accepted watermark: root refuses every one of them, this daemon reports
// success, and the two components it speaks for keep whatever they last said
// for as long as the host stays up.
//
// That is not hypothetical. It is what the live host did — user_access and
// session_expiry sat on facts from before a sleep while the agent published
// happily into nothing.
func TestARestartedPublisherContinuesItsOwnStream(t *testing.T) {
	stream := filepath.Join(t.TempDir(), "connectivity-stream.json")
	first, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", stream)
	if err != nil || first == nil {
		t.Fatalf("publisher: %v", err)
	}
	accepted := func(context.Context, string, ipc.Request) (ipc.Response, error) {
		return ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorNone}, nil
	}
	first.roundTrip = accepted
	for cycle := 0; cycle < 3; cycle++ {
		if err := first.Publish(context.Background(), observedEvidence()); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	reached := uint64(0)
	for _, collector := range first.sources {
		if collector.Sequence() > reached {
			reached = collector.Sequence()
		}
	}
	if reached == 0 {
		t.Fatal("nothing was published, so this test proves nothing")
	}

	restarted, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock", stream)
	if err != nil || restarted == nil {
		t.Fatalf("restart: %v", err)
	}
	for _, collector := range restarted.sources {
		if collector.Sequence() != reached {
			t.Fatalf("a restarted source resumed at %d, want %d: every fact it "+
				"publishes is behind the accepted watermark",
				collector.Sequence(), reached)
		}
	}
}

// The two components this domain speaks for are time-sensitive, so a wake
// invalidates what it last said about them. Root raises that requirement for
// every component; only the owner can answer it, and until this existed the
// owner never did — which is why a sleep could never be recorded as survived.
func TestAPublisherRestatesInFullAfterASleep(t *testing.T) {
	publisher, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock",
		filepath.Join(t.TempDir(), "connectivity-stream.json"))
	if err != nil || publisher == nil {
		t.Fatalf("publisher: %v", err)
	}
	continuous, awake := time.Hour, time.Hour
	publisher.clocks = func() (time.Duration, time.Duration, error) {
		return continuous, awake, nil
	}
	var sent []connectivity.Fact
	publisher.roundTrip = func(_ context.Context, _ string, request ipc.Request) (ipc.Response, error) {
		sent = sent[:0]
		for _, raw := range request.PublishConnectivityFacts.Facts {
			var fact connectivity.Fact
			if err := json.Unmarshal(raw, &fact); err != nil {
				t.Fatalf("decode: %v", err)
			}
			sent = append(sent, fact)
		}
		return ipc.Response{Version: ipc.ProtocolVersion, Error: ipc.ErrorNone}, nil
	}

	// The opening publication restates everything, as any first one does.
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// An ordinary cycle does not.
	continuous += time.Minute
	awake += time.Minute
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for _, fact := range sent {
		if fact.Baseline {
			t.Fatal("an ordinary cycle restated the whole picture")
		}
	}

	// The host sleeps: the clock that counts through it advances and the one
	// that stops for it does not.
	continuous += 2 * time.Hour
	awake += time.Second
	if err := publisher.Publish(context.Background(), observedEvidence()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(sent) == 0 {
		t.Fatal("the cycle after the wake published nothing")
	}
	for _, fact := range sent {
		if !fact.Baseline {
			t.Fatalf("%s was not restated after the wake, so root keeps owing "+
				"a rebaseline nobody can answer", fact.Component)
		}
	}
}
