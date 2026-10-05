package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
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
	// The real observer records what it was asked about, and the signature is
	// keyed by that rather than by the route that answered. A fixture that left
	// it empty let a test pass over a signature the machine could not build.
	route.Requested = address
	return route, nil
}

type fakeProcessObserver struct {
	observation observe.ProcessObservation
	err         error
	asked       *string
}

func (observer fakeProcessObserver) Tunnel(
	_ context.Context,
	configPath string,
) (observe.ProcessObservation, error) {
	if observer.asked != nil {
		*observer.asked = configPath
	}
	return observer.observation, observer.err
}

type fakeEndpointObserver struct {
	ready map[string]bool
	// unreadable names the endpoints whose probe cannot run at all, which is a
	// different failure from one that runs and answers not ready.
	unreadable map[string]bool
}

func (observer fakeEndpointObserver) Endpoint(
	_ context.Context,
	endpoint observe.Endpoint,
) (observe.ReadinessObservation, error) {
	if observer.unreadable[endpoint.Name] {
		return observe.ReadinessObservation{}, errors.New("the probe could not run")
	}
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
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleSuspended || len(summary.Plan.Operations) != 0 {
		t.Fatalf("Observe() = %+v", summary)
	}
	// It stops before the probes, not before the tunnel: the runtime this rule
	// reproduces checks its process on every tick whatever the lid is doing.
	if !summary.ProcessObserved || !summary.SingBoxRunning {
		t.Fatalf("a dark wake did not look at the tunnel: %+v", summary)
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
	// The proposal is recorded and not applied. It does not grade the health:
	// a runtime that may not act on what it sees is working when it sees it.
	if summary.State != CycleHealthy ||
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

// A proposal this runtime may not apply stands for as long as the condition
// holds, so a health that counted it reported the same value forever. Measured
// 2026-10-05: seven unbroken hours of degraded with no failure of any kind.
func TestCycleIsSoundWhileHoldingAProposalItMayNotApply(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	target := config.Targets[0]
	network.routes[target.Destination] = observe.RouteObservation{
		Destination: target.Destination,
		Interface:   "utun8",
	}
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.Failures != 0 {
		t.Fatalf("the fixture failed an observation: %+v", summary)
	}
	if len(summary.Plan.Operations) == 0 {
		t.Fatalf("the fixture proposed nothing, so it proves nothing: %+v", summary)
	}
	if summary.State != CycleHealthy {
		t.Fatalf("a standing proposal graded the health: %+v", summary.State)
	}
}

// The other half of the same rule: work outstanding does not excuse a failure.
func TestCycleIsUnsoundOnFailureWhetherWorkStandsOrNot(t *testing.T) {
	for _, standing := range []bool{false, true} {
		config, network, processes, endpoints := healthyCycleFixtures(t)
		// A missing route observation is the failure; a second target moved to
		// a foreign interface is the standing work beside it.
		delete(network.routes, config.Targets[0].Destination)
		if standing {
			target := config.Targets[1]
			network.routes[target.Destination] = observe.RouteObservation{
				Destination: target.Destination,
				Interface:   "utun8",
			}
		}
		cycle, _ := NewCycle(config, network, processes, endpoints)

		summary := cycle.Observe(context.Background())
		if summary.Failures == 0 {
			t.Fatalf("standing=%v: the fixture did not fail: %+v", standing, summary)
		}
		if summary.State != CycleDegraded {
			t.Fatalf("standing=%v: a failure did not read as unsound: %+v", standing, summary.State)
		}
	}
}

// The cause kept is the first failure the fold met, and a later failure does
// not replace it. The probes run together and are folded in configuration
// order, so the fold decides, not which answer arrived first.
func TestCycleKeepsTheCauseOfItsFirstFailure(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	endpoints.unreadable = map[string]bool{"normal-codex": true, "twilight-codex": true}
	endpoints.ready["outer-ready"] = false
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	// Two probes that could not run, then an outer path that answered and was
	// not ready: three failures, two causes, one kept.
	if summary.Failures != 3 {
		t.Fatalf("the fixture did not fail three times: %+v", summary.Failures)
	}
	if summary.Cause != control.ReasonEndpointUnreadable {
		t.Fatalf("the cause kept was not the first met: %q", summary.Cause)
	}
	if got := rootOperatorReason(summary); got != control.ReasonEndpointUnreadable {
		t.Fatalf("the published reason = %q", got)
	}
}

// The record that sent a reader after a probe that had not run.
func TestCycleNamesAFailureThatWasNotAProbe(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	delete(network.routes, config.Targets[0].Destination)
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleDegraded || summary.Failures == 0 {
		t.Fatalf("the fixture was not degraded: %+v", summary)
	}
	if summary.Cause != control.ReasonRouteUnreadable {
		t.Fatalf("the cause = %q", summary.Cause)
	}
	if got := rootOperatorReason(summary); got == control.ReasonProbeFailed {
		t.Fatalf("a route it could not read was published as a failed probe")
	}
}

// And the other half: the word keeps its meaning where it is true.
func TestCycleReportsAFailedProbeAsAFailedProbe(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	endpoints.ready["outer-ready"] = false
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.Failures != 1 || summary.Cause != control.ReasonProbeFailed {
		t.Fatalf("a probe that ran and failed = %+v / %q", summary.Failures, summary.Cause)
	}
	if got := rootOperatorReason(summary); got != control.ReasonProbeFailed {
		t.Fatalf("the published reason = %q", got)
	}
}

// A physical network it could not read was published as an intentional sleep.
func TestCycleDoesNotCallAnUnreadableNetworkASleep(t *testing.T) {
	config, network, processes, endpoints := healthyCycleFixtures(t)
	network.physical = observe.PhysicalNetwork{}
	cycle, _ := NewCycle(config, network, processes, endpoints)

	summary := cycle.Observe(context.Background())
	if summary.State != CycleSuspended || summary.Failures == 0 {
		t.Fatalf("the fixture was not the suspended-with-failure case: %+v", summary)
	}
	if got := rootOperatorReason(summary); got != control.ReasonPhysicalNetworkUnready {
		t.Fatalf("the published reason = %q", got)
	}
}
