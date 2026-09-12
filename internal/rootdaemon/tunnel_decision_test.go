package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

type traversingPayload struct{ traversed bool }

func (payload traversingPayload) Payload(
	_ context.Context,
	endpoint observe.PayloadEndpoint,
) (observe.PayloadObservation, error) {
	return observe.PayloadObservation{
		Name: endpoint.Name, Traversed: payload.traversed, Status: 204,
	}, nil
}

func supervisedCycle(t *testing.T, traversed bool) *Cycle {
	t.Helper()
	config, network, processes, endpoints := healthyCycleFixtures(t)
	config.TunnelSupervision = &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{
			WakeThreshold: 90 * time.Second, PayloadFailures: 2,
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
		WithPayloadObserver(traversingPayload{traversed: traversed}),
		WithTunnelState(store))
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	return cycle
}

// A decision that agreed and a decision never reached look the same in an empty
// record. Deciding nothing is a decision and says so; not being asked is not.
func TestDecidingNothingIsDistinctFromNotBeingAsked(t *testing.T) {
	decided := supervisedCycle(t, true).Observe(context.Background())
	if decided.Tunnel.Action != tunnelplan.ActionNone {
		t.Fatalf("a supervised cycle with nothing wrong decided %q, want %q",
			decided.Tunnel.Action, tunnelplan.ActionNone)
	}

	config, network, processes, endpoints := healthyCycleFixtures(t)
	unsupervised, err := NewCycle(config, network, processes, endpoints)
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	notAsked := unsupervised.Observe(context.Background())
	if notAsked.Tunnel.Action != "" {
		t.Fatalf("an unsupervised cycle decided %q; it was never asked",
			notAsked.Tunnel.Action)
	}
}

// The record distinguishes them too, or the distinction the cycle keeps is lost
// the moment it is written down.
func TestOnlyADecisionIsRecorded(t *testing.T) {
	if decided(tunnelplan.Plan{}) {
		t.Fatal("a cycle that was never asked was taken as having decided")
	}
	if !decided(tunnelplan.Plan{Action: tunnelplan.ActionNone}) {
		t.Fatal("deciding nothing was taken as not having been asked")
	}
	if err := recordTunnelDecision(nil, tunnelplan.Plan{}); err != nil {
		t.Fatalf("an absent decision was an error: %v", err)
	}
	encoded, err := event.Encode(event.SchemaTunnelDecision, event.TunnelDecision{
		Action: string(tunnelplan.ActionNone),
	})
	if err != nil {
		t.Fatalf("deciding nothing could not be written down: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("deciding nothing encoded to nothing")
	}
}

// The vocabulary is closed. Free text here would carry whatever a future
// planner happened to name something, into a record read months later.
func TestTheRecordedVocabularyIsClosed(t *testing.T) {
	for _, decision := range []event.TunnelDecision{
		{Action: "something_else"},
		{Action: "rebuild_tunnel", Causes: []string{"a_new_cause"}},
		{Action: "none", Causes: []string{"process_gone"}},
		{Action: "rebuild_tunnel", Causes: []string{"wake_gap", "wake_gap"}},
	} {
		if _, err := event.Encode(event.SchemaTunnelDecision, decision); err == nil {
			t.Errorf("the archive accepted %+v", decision)
		}
	}
	for _, decision := range []event.TunnelDecision{
		{Action: "none"},
		{Action: "reapply_routes", Causes: []string{"routes_drifted"}},
		{Action: "rebuild_tunnel", Causes: []string{"process_gone", "wake_gap"}},
	} {
		if _, err := event.Encode(event.SchemaTunnelDecision, decision); err != nil {
			t.Errorf("the archive refused %+v: %v", decision, err)
		}
	}
}

// The payload path failing past its threshold is the sixth cause, and it is the
// one that tells a tunnel that works from one that answers.
func TestAPayloadThatDoesNotTraverseBecomesACause(t *testing.T) {
	cycle := supervisedCycle(t, false)
	var summary Summary
	for range 2 {
		summary = cycle.Observe(context.Background())
	}
	found := false
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CausePayloadFailed {
			found = true
		}
	}
	if !found {
		t.Fatalf("causes = %v, want %q among them",
			summary.Tunnel.Causes, tunnelplan.CausePayloadFailed)
	}
}

// The decision has to happen on a cycle that stopped early, because the tunnel
// being in trouble is exactly when a cycle does. A decision reached only on
// complete cycles would be absent at the moments it exists for — which is what
// the first soak check found, before any cause had been induced.
func TestAnIncompleteCycleStillDecides(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	config.TunnelSupervision = &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{
			WakeThreshold: 90 * time.Second, PayloadFailures: 2,
		},
		Payload: observe.PayloadEndpoint{
			Name: "payload", URL: "http://198.51.100.1/", Timeout: time.Second,
		},
	}
	store, err := newTunnelStateStore(filepath.Join(t.TempDir(), "tunnel.json"))
	if err != nil {
		t.Fatalf("newTunnelStateStore: %v", err)
	}
	// A cycle that cannot read the host's power state stops at the first
	// observation it takes, which is the earliest of the eight exits.
	cycle, err := NewCycle(config, refusingNetwork{NetworkObserver: network},
		processes, endpoints,
		WithPayloadObserver(traversingPayload{traversed: true}),
		WithTunnelState(store))
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	summary := cycle.Observe(context.Background())
	if summary.Complete {
		t.Fatal("the cycle was supposed to stop early")
	}
	if summary.Tunnel.Action == "" {
		t.Fatal("a cycle that stopped early reached no decision at all")
	}
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CauseCarrierChanged {
			t.Fatal("a cycle that saw no routes decided the carrier had changed")
		}
	}
}

// refusingNetwork cannot answer the first observation a cycle takes.
type refusingNetwork struct{ NetworkObserver }

func (refusingNetwork) Power(context.Context) (observe.PowerObservation, error) {
	return observe.PowerObservation{}, errPowerUnreadable
}

var errPowerUnreadable = errors.New("power state is unreadable")

// The signature is keyed by the address asked about, not by the route that
// answered.
//
// A route observation carries both, and the route's own destination is the
// prefix that matched — often the half a tunnel claims, shared by many
// configured destinations, and empty for routes that have none. On the machine
// that produced a signature with seven identical keys and four invalid ones:
// one that can change without the carrier changing, and stay still when it
// does.
func TestTheSignatureIsKeyedByWhatWasAsked(t *testing.T) {
	cycle := supervisedCycle(t, true)
	summary := cycle.Observe(context.Background())
	if len(summary.Observed.Routes) < 2 {
		t.Fatalf("the fixture observed %d routes; this proves nothing below two",
			len(summary.Observed.Routes))
	}

	carried := map[string]struct{}{}
	for _, route := range summary.Observed.Routes {
		if !route.Requested.IsValid() {
			t.Fatalf("a route observation has no address it was asked about: %+v",
				route)
		}
		if _, duplicate := carried[route.Requested.String()]; duplicate {
			t.Fatalf("%s was asked about twice; the signature would count it twice",
				route.Requested)
		}
		carried[route.Requested.String()] = struct{}{}
	}

	// The signature the cycle actually built, not the observations it built it
	// from. Keyed by the route that answered rather than by the address asked
	// about, it had seven identical keys and four invalid ones on the machine.
	for destination := range carried {
		if !strings.Contains(string(summary.Carrier), destination+"=") {
			t.Fatalf("the signature %q does not key %s, which was asked about",
				summary.Carrier, destination)
		}
	}
	if strings.Contains(string(summary.Carrier), "invalid IP") {
		t.Fatalf("the signature carries an address that is not one: %q",
			summary.Carrier)
	}
	entries := strings.Fields(string(summary.Carrier))
	if len(entries) != len(carried) {
		t.Fatalf("the signature has %d entries for %d destinations asked about: %q",
			len(entries), len(carried), summary.Carrier)
	}
}

// The shape the machine actually produced, which the shared fixture does not:
// route observations whose own destination is the prefix that matched rather
// than the address asked about, and some with no destination at all.
func TestTheSignatureSurvivesRoutesThatSharedAPrefix(t *testing.T) {
	shared := netip.MustParseAddr("128.0.0.0")
	routes := []observe.RouteObservation{
		{
			Requested:   netip.MustParseAddr("203.0.113.20"),
			Destination: shared,
			Interface:   "utun4",
		},
		{
			Requested:   netip.MustParseAddr("198.51.100.20"),
			Destination: shared,
			Interface:   "utun4",
		},
		{
			Requested: netip.MustParseAddr("192.0.2.20"),
			Interface: "en0",
		},
		{
			Destination: shared,
			Interface:   "utun4",
		},
	}
	carried := carriedDestinations(routes)
	if len(carried) != 3 {
		t.Fatalf("carried %d destinations, want 3 — the one with no address "+
			"asked about is not a destination this runtime cares about",
			len(carried))
	}
	signature := string(tunnelplan.NewSignature(carried))
	for _, want := range []string{
		"203.0.113.20=utun4", "198.51.100.20=utun4", "192.0.2.20=en0",
	} {
		if !strings.Contains(signature, want) {
			t.Errorf("signature %q does not carry %q", signature, want)
		}
	}
	if strings.Contains(signature, "128.0.0.0") {
		t.Fatalf("the signature is keyed by the route that answered: %q",
			signature)
	}
	if strings.Contains(signature, "invalid IP") {
		t.Fatalf("the signature carries an address that is not one: %q",
			signature)
	}
}
