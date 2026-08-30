package userdaemon

import (
	"context"
	"encoding/json"
	"testing"

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
	publisher, err := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock")
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
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock")
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
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock")
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
	publisher, _ := newFactPublisher("boot-0000000000000000", "/tmp/probe.sock")
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
	publisher, err := newFactPublisher("boot-0000000000000000", "")
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
