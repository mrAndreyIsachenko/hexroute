package rootdaemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

func carrierCycle(t *testing.T) (*Cycle, RuntimeConfig, *fakeNetworkObserver) {
	t.Helper()
	config, network, processes, endpoints := healthyCycleFixtures(t)
	config.TunnelSupervision = &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{
			Interval: 60 * time.Second, WakeThreshold: 180 * time.Second,
			PayloadFailures: 2, LinkFailures: 2,
		},
		Payload: observe.PayloadEndpoint{Name: "payload", URL: "http://198.51.100.1/", Timeout: time.Second},
	}
	store, err := newTunnelStateStore(filepath.Join(t.TempDir(), "tunnel.json"))
	if err != nil {
		t.Fatal(err)
	}
	cycle, err := NewCycle(config, network, processes, endpoints,
		WithPayloadObserver(traversingPayload{traversed: true}), WithTunnelState(store))
	if err != nil {
		t.Fatal(err)
	}
	return cycle, config, network
}

func named(summary Summary, cause tunnelplan.Cause) bool {
	for _, held := range summary.Tunnel.Causes {
		if held == cause {
			return true
		}
	}
	return false
}

// The carrier is the upstream probe and the ingress targets, and nothing else.
func TestTheCarrierIsTheProbeAndTheIngressTargets(t *testing.T) {
	cycle, config, _ := carrierCycle(t)
	summary := cycle.Observe(context.Background())
	ingress := 0
	for _, target := range config.Targets {
		if target.Role == routeplan.RoleIngress {
			ingress++
		}
	}
	if want := 1 + ingress; summary.Carrier.Entries() != want {
		t.Fatalf("the carrier covers %d entries, want the probe and %d ingress targets", summary.Carrier.Entries(), ingress)
	}
}

// A route that is neither the probe nor an ingress target moving is not a
// change of carrier.
//
// The previous signature covered every configured route and changed whenever a
// fallback route came or went; before this change it decided 75 carrier changes
// in a window where the runtime it is compared against recorded none.
func TestAnotherRouteMovingIsNotACarrierChange(t *testing.T) {
	cycle, config, network := carrierCycle(t)
	cycle.Observe(context.Background())

	moved := false
	for _, target := range config.Targets {
		if target.Role == routeplan.RoleIngress {
			continue
		}
		route := network.routes[target.Destination]
		route.Interface = "utun77"
		network.routes[target.Destination] = route
		moved = true
		break
	}
	if !moved {
		t.Fatal("the fixture has no route that is not ingress")
	}
	if summary := cycle.Observe(context.Background()); named(summary, tunnelplan.CauseCarrierChanged) {
		t.Fatalf("a non-ingress route moving was a carrier change: %v", summary.Tunnel.Causes)
	}
}

// The upstream probe moving is a change of carrier.
func TestTheProbeMovingIsACarrierChange(t *testing.T) {
	cycle, config, network := carrierCycle(t)
	cycle.Observe(context.Background())

	route := network.routes[config.UpstreamProbeAddress]
	route.Interface = "en0"
	network.routes[config.UpstreamProbeAddress] = route
	if summary := cycle.Observe(context.Background()); !named(summary, tunnelplan.CauseCarrierChanged) {
		t.Fatalf("the upstream probe moving was not a carrier change: %v", summary.Tunnel.Causes)
	}
}

// A configured wake threshold at or below one interval is refused, because it
// would name a wake gap on every cycle.
func TestAWakeThresholdWithinOneIntervalIsRefused(t *testing.T) {
	supervision := TunnelSupervisionConfig{
		WakeThresholdSeconds: 60, PayloadFailures: 2, LinkFailures: 2,
		Payload: PayloadProbeConfig{Name: "payload", URL: "http://198.51.100.1/", TimeoutSeconds: 1},
	}
	runtime, err := supervision.runtime()
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	runtime.Policy.Interval = 60 * time.Second
	if _, _, err := tunnelplan.Decide(runtime.Policy, tunnelplan.State{}, tunnelplan.Observed{Complete: true, ProcessRunning: true}); err == nil {
		t.Fatal("a threshold of one interval was accepted by the rule")
	}
}
