package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
)

type fakeNetworkObserver struct {
	power    observe.PowerObservation
	physical observe.PhysicalNetwork
	tuns     []observe.TUNInterface
	routes   map[netip.Addr]observe.RouteObservation
	err      error
}

func (observer *fakeNetworkObserver) Power(context.Context) (observe.PowerObservation, error) {
	return observer.power, observer.err
}

func (observer *fakeNetworkObserver) PhysicalNetwork(
	context.Context,
	string,
) (observe.PhysicalNetwork, error) {
	return observer.physical, observer.err
}

func (observer *fakeNetworkObserver) TUNInterfaces(context.Context) ([]observe.TUNInterface, error) {
	return observer.tuns, observer.err
}

func (observer *fakeNetworkObserver) Route(
	_ context.Context,
	address netip.Addr,
) (observe.RouteObservation, error) {
	route, exists := observer.routes[address]
	if !exists {
		return observe.RouteObservation{}, errors.New("not observed")
	}
	return route, nil
}

type fakeProcessObserver struct {
	observation observe.ProcessObservation
	err         error
}

func (observer fakeProcessObserver) SingBox(
	context.Context,
	int,
) (observe.ProcessObservation, error) {
	return observer.observation, observer.err
}

type fakeEndpointObserver struct {
	ready map[string]bool
}

func (observer fakeEndpointObserver) Endpoint(
	_ context.Context,
	endpoint observe.Endpoint,
) (observe.ReadinessObservation, error) {
	return observe.ReadinessObservation{Name: endpoint.Name, Ready: observer.ready[endpoint.Name]}, nil
}

func runtimeFixture(t *testing.T) RuntimeConfig {
	t.Helper()
	config, err := DecodeConfig(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error: %v", err)
	}
	return config
}

func healthyCycleFixtures(t *testing.T) (
	RuntimeConfig,
	*fakeNetworkObserver,
	fakeProcessObserver,
	fakeEndpointObserver,
) {
	t.Helper()
	config := runtimeFixture(t)
	physical := observe.PhysicalNetwork{
		Interface: "en7",
		Gateway:   netip.MustParseAddr("192.0.2.1"),
		Link:      observe.LinkStateUp,
	}
	tun := observe.TUNInterface{
		Name:      "utun8",
		Addresses: []netip.Addr{config.ManagedTUNAddress},
	}
	network := &fakeNetworkObserver{
		power: observe.PowerObservation{
			Source:   observe.PowerSourceAC,
			Lid:      observe.LidStateOpen,
			WakeKind: observe.WakeKindFull,
		},
		physical: physical,
		tuns:     []observe.TUNInterface{tun},
		routes: map[netip.Addr]observe.RouteObservation{
			config.UpstreamProbeAddress: {
				Destination: config.UpstreamProbeAddress,
				Interface:   "utun3",
			},
		},
	}
	for _, target := range config.Targets {
		route := observe.RouteObservation{Destination: target.Destination}
		switch target.Role {
		case routeplan.RoleIngress:
			if target.Preferred == "upstream_vpn" {
				route.Interface = "utun3"
			} else {
				route.Interface = physical.Interface
				route.Gateway = physical.Gateway
			}
		case routeplan.RoleCorporate, routeplan.RoleGitLabHTTPS, routeplan.RoleCodexFallback:
			route.Interface = tun.Name
		}
		network.routes[target.Destination] = route
	}
	processes := fakeProcessObserver{
		observation: observe.ProcessObservation{Running: true},
	}
	endpoints := fakeEndpointObserver{ready: map[string]bool{
		"outer-ready":    true,
		"normal-codex":   false,
		"twilight-codex": true,
	}}
	return config, network, processes, endpoints
}

func TestCycleObservesHealthyBaselineWithoutMutation(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	cycle, err := NewCycle(config, network, processes, endpoints)
	if err != nil {
		t.Fatalf("NewCycle() error: %v", err)
	}

	summary := cycle.Observe(context.Background())
	if summary.State != CycleHealthy ||
		!summary.SingBoxRunning ||
		!summary.OuterReady ||
		summary.Failures != 0 ||
		!summary.Plan.ObserveOnly ||
		len(summary.Plan.Operations) != 0 {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleSuspendsDuringDarkWakeWithoutNetworkProposals(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	network.power.WakeKind = observe.WakeKindDark
	network.routes = nil
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleSuspended || len(summary.Plan.Operations) != 0 {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleBlocksPlanWhenAnyRouteObservationFails(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	delete(network.routes, config.Targets[0].Destination)
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleDegraded ||
		summary.Failures == 0 ||
		len(summary.Plan.Operations) != 0 {
		t.Fatalf("Observe() = %+v", summary)
	}
}

func TestCycleReportsScopedProposalWithoutApplyingIt(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	target := config.Targets[0]
	network.routes[target.Destination] = observe.RouteObservation{
		Destination: target.Destination,
		Interface:   "utun8",
	}
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleDegraded ||
		len(summary.Plan.Operations) != 1 ||
		!summary.Plan.ObserveOnly {
		t.Fatalf("Observe() = %+v", summary)
	}
	if summary.Plan.Operations[0].Role != routeplan.RoleIngress {
		t.Fatalf("proposal = %+v", summary.Plan.Operations[0])
	}
}

func TestCycleReadinessFailureDoesNotRestartProcess(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	endpoints.ready["outer-ready"] = false
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleDegraded || summary.OuterReady || !summary.SingBoxRunning {
		t.Fatalf("Observe() = %+v", summary)
	}
}
