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
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
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
	if err := recordTunnelDecision(nil, tunnelplan.Plan{}, 0, nil, 7); err != nil {
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

// A payload that does not traverse is recorded, and decides nothing.
//
// The runtime this rule reproduces rebuilds on the payload path only after a
// failover this runtime does not perform, so a rule that rebuilt on it would
// disagree with that runtime every time the path failed.
func TestAPayloadThatDoesNotTraverseIsRecordedNotActedOn(t *testing.T) {
	cycle := supervisedCycle(t, false)
	var summary Summary
	for range 3 {
		summary = cycle.Observe(context.Background())
	}
	for _, cause := range summary.Tunnel.Causes {
		if cause == tunnelplan.CausePayloadFailed {
			t.Fatalf("a failed payload path was named: %v", summary.Tunnel.Causes)
		}
	}
	if summary.Tunnel.Action != tunnelplan.ActionNone {
		t.Fatalf("a failed payload path decided %q", summary.Tunnel.Action)
	}
	if summary.Tunnel.Grounds.PayloadOK || summary.Tunnel.Grounds.PayloadFailures < 3 {
		t.Fatalf("the failure is not in the grounds: %+v", summary.Tunnel.Grounds)
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
// destinations. On the machine a signature keyed that way had seven identical
// keys and four invalid ones. The keys now come from the configuration: the
// upstream probe and each ingress target, the three paths the runtime this rule
// reproduces watches.
func TestTheSignatureIsKeyedByWhatWasAsked(t *testing.T) {
	cycle, config, _ := carrierCycle(t)
	summary := cycle.Observe(context.Background())

	asked := map[string]bool{config.UpstreamProbeAddress.String(): true}
	for _, target := range config.Targets {
		if target.Role == routeplan.RoleIngress {
			asked[target.Destination.String()] = true
		}
	}
	entries := strings.Fields(string(summary.Carrier))
	if len(entries) != len(asked) {
		t.Fatalf("the signature has %d entries for %d paths asked about: %q", len(entries), len(asked), summary.Carrier)
	}
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry, "=")
		if !asked[key] {
			t.Fatalf("the signature keys %s, which is not a path asked about: %q", key, summary.Carrier)
		}
	}
	if strings.Contains(string(summary.Carrier), "invalid IP") {
		t.Fatalf("the signature carries an address that is not one: %q", summary.Carrier)
	}
}

// The shape the machine actually produced: route observations whose own
// destination is the prefix that matched rather than the address asked about.
// The signature keys by what was asked, so the shared prefix never appears in it.
func TestTheSignatureSurvivesRoutesThatSharedAPrefix(t *testing.T) {
	cycle, config, network := carrierCycle(t)
	shared := netip.MustParseAddr("128.0.0.0")
	for address, route := range network.routes {
		route.Destination = shared
		network.routes[address] = route
	}
	summary := cycle.Observe(context.Background())

	signature := string(summary.Carrier)
	if strings.Contains(signature, "128.0.0.0") {
		t.Fatalf("the signature is keyed by the route that answered: %q", signature)
	}
	if !strings.Contains(signature, config.UpstreamProbeAddress.String()+"=") {
		t.Fatalf("the signature lost the probe it asked about: %q", signature)
	}
}
