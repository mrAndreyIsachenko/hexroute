package connectivitycollect

import (
	"crypto/rand"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type fixedClock struct{ tick control.Tick }

func (clock *fixedClock) Wall() time.Time {
	return time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
}

func (clock *fixedClock) Tick() control.Tick {
	clock.tick++
	return clock.tick
}

func newCollector(t *testing.T, source connectivity.SourceID, domain policy.Domain) *Collector {
	t.Helper()
	collector, err := New(Options{
		Source: source, Domain: domain, BootID: "boot-abcdef0123456789",
		Clock: &fixedClock{tick: 100}, Random: rand.Reader,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return collector
}

func TestEmittedFactsAreValidAndOwned(t *testing.T) {
	collector := newCollector(t, "root.network", policy.DomainRoot)
	network := observe.PhysicalNetwork{
		Interface: "en0", Gateway: netip.MustParseAddr("192.168.1.1"),
		Link: observe.LinkStateUp,
	}
	for _, observation := range []Observation{
		MapPhysicalNetwork(network, nil),
		MapDefaultPath(network, []observe.TUNInterface{{
			Name: "utun7", Addresses: []netip.Addr{netip.MustParseAddr("10.0.0.2")},
		}}, nil),
	} {
		fact, err := collector.Emit(observation)
		if err != nil {
			t.Fatalf("emit %s: %v", observation.Component, err)
		}
		if err := connectivity.Validate(fact); err != nil {
			t.Fatalf("%s: %v", observation.Component, err)
		}
		if err := safety.ValidateAuthoritativeConnectivityFact(fact, policy.DomainRoot); err != nil {
			t.Fatalf("%s: %v", observation.Component, err)
		}
	}
}

// A collector wired to a component it does not own must fail where the mistake
// is, not one layer later at the acceptor.
func TestCollectorRefusesAComponentItDoesNotOwn(t *testing.T) {
	collector := newCollector(t, "root.network", policy.DomainRoot)
	_, err := collector.Emit(MapScopedRoutes(2, nil, "utun7", nil))
	if !errors.Is(err, ErrNotOwned) {
		t.Fatalf("got %v, want %v", err, ErrNotOwned)
	}
}

func TestSequenceAdvancesPerSource(t *testing.T) {
	collector := newCollector(t, "root.relays", policy.DomainRoot)
	last := uint64(0)
	for round := 0; round < 4; round++ {
		fact, err := collector.Emit(MapRelays(
			[]observe.ReadinessObservation{{Name: "a", Ready: true}},
			0, connectivity.SelectedPrimary))
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		if fact.SourceSequence != last+1 {
			t.Fatalf("sequence %d, want %d", fact.SourceSequence, last+1)
		}
		last = fact.SourceSequence
	}
}

func TestPhysicalNetworkMapping(t *testing.T) {
	gateway := netip.MustParseAddr("192.168.1.1")
	tests := []struct {
		name      string
		network   observe.PhysicalNetwork
		err       error
		lifecycle connectivity.Lifecycle
		linkUp    bool
		carrier   bool
	}{
		{"link up with gateway", observe.PhysicalNetwork{
			Interface: "en0", Gateway: gateway, Link: observe.LinkStateUp},
			nil, connectivity.LifecycleReady, true, true},
		{"link up without gateway", observe.PhysicalNetwork{
			Interface: "en0", Link: observe.LinkStateUp},
			nil, connectivity.LifecycleDegraded, true, false},
		{"link down", observe.PhysicalNetwork{
			Interface: "en0", Link: observe.LinkStateDown},
			nil, connectivity.LifecycleFailed, false, false},
		{"no interface", observe.PhysicalNetwork{},
			nil, connectivity.LifecycleFailed, false, false},
		{"observation failed", observe.PhysicalNetwork{},
			errors.New("boom"), connectivity.LifecycleUnknown, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := MapPhysicalNetwork(test.network, test.err)
			if observation.Lifecycle != test.lifecycle {
				t.Fatalf("lifecycle %q, want %q", observation.Lifecycle, test.lifecycle)
			}
			payload := observation.Payload.PhysicalNetwork
			if payload.LinkUp != test.linkUp || payload.HasCarrier != test.carrier {
				t.Fatalf("link=%v carrier=%v, want %v/%v",
					payload.LinkUp, payload.HasCarrier, test.linkUp, test.carrier)
			}
		})
	}
}

func TestScopedRouteMapping(t *testing.T) {
	onTunnel := observe.RouteObservation{Interface: "utun7"}
	elsewhere := observe.RouteObservation{Interface: "en0"}
	tests := []struct {
		name        string
		configured  uint16
		routes      []observe.RouteObservation
		lifecycle   connectivity.Lifecycle
		installed   uint16
		conflicting uint16
	}{
		{"all installed", 2, []observe.RouteObservation{onTunnel, onTunnel},
			connectivity.LifecycleReady, 2, 0},
		{"partially installed", 2, []observe.RouteObservation{onTunnel},
			connectivity.LifecycleDegraded, 1, 0},
		{"landed elsewhere", 2, []observe.RouteObservation{elsewhere, elsewhere},
			connectivity.LifecycleFailed, 0, 2},
		{"none configured", 0, nil, connectivity.LifecycleNotApplicable, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := MapScopedRoutes(test.configured, test.routes, "utun7", nil)
			payload := observation.Payload.ScopedRoutes
			if observation.Lifecycle != test.lifecycle {
				t.Fatalf("lifecycle %q, want %q", observation.Lifecycle, test.lifecycle)
			}
			if payload.Installed != test.installed || payload.Conflicting != test.conflicting {
				t.Fatalf("installed=%d conflicting=%d, want %d/%d",
					payload.Installed, payload.Conflicting, test.installed, test.conflicting)
			}
		})
	}
}

func TestRelayMappingNeverSelectsAReserveThatIsNotThere(t *testing.T) {
	observation := MapRelays(
		[]observe.ReadinessObservation{{Ready: true}, {Ready: true}},
		0, connectivity.SelectedReserve)
	if observation.Payload.Relays.SelectedClass != connectivity.SelectedPrimary {
		t.Fatalf("selected %q, want primary", observation.Payload.Relays.SelectedClass)
	}
}

func TestUserAccessMapping(t *testing.T) {
	running := userobserve.ServiceObservation{Loaded: true, Running: true}
	tests := []struct {
		name      string
		profile   userobserve.ProfileObservation
		service   userobserve.ServiceObservation
		lifecycle connectivity.Lifecycle
		class     connectivity.ProfileClass
	}{
		{"no profile", userobserve.ProfileObservation{}, running,
			connectivity.LifecycleNotApplicable, connectivity.ProfileNone},
		{"connecting", userobserve.ProfileObservation{
			Found: true, State: userobserve.ProfileActive, Connecting: true}, running,
			connectivity.LifecycleDegraded, connectivity.ProfileConfigured},
		{"disconnected with service down", userobserve.ProfileObservation{
			Found: true, State: userobserve.ProfileDisconnected},
			userobserve.ServiceObservation{}, connectivity.LifecycleFailed,
			connectivity.ProfileConfigured},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := MapUserAccess(test.profile, test.service, nil)
			if observation.Lifecycle != test.lifecycle {
				t.Fatalf("lifecycle %q, want %q", observation.Lifecycle, test.lifecycle)
			}
			if observation.Payload.UserAccess.ProfileClass != test.class {
				t.Fatalf("class %q, want %q",
					observation.Payload.UserAccess.ProfileClass, test.class)
			}
		})
	}
}

// Hexroute cannot observe how long a session has left. The mapper must not
// claim a measurement it never made.
func TestSessionMappingNeverClaimsExpiry(t *testing.T) {
	for _, state := range []userobserve.SessionState{
		userobserve.SessionActive, userobserve.SessionInactive, userobserve.SessionUnknown,
	} {
		observation := MapUserSession(userobserve.SessionObservation{State: state}, nil)
		class := observation.Payload.SessionExpiry.ExpiryClass
		if class == connectivity.ExpiryExpiring || class == connectivity.ExpiryExpired {
			t.Fatalf("session state %q produced %q from a presence check", state, class)
		}
	}
}

// Collection observes. It must not be able to change anything, and the
// cheapest durable proof is that it cannot reach the code that would.
func TestCollectionCannotMutate(t *testing.T) {
	forbidden := []string{
		"os/exec", "net/http",
		"github.com/mrAndreyIsachenko/hexroute/internal/command",
		"github.com/mrAndreyIsachenko/hexroute/internal/actionlease",
		"github.com/mrAndreyIsachenko/hexroute/internal/actionplan",
		"github.com/mrAndreyIsachenko/hexroute/internal/routeplan",
		"github.com/mrAndreyIsachenko/hexroute/internal/credentials",
	}
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, parsed := range packages {
		for name, file := range parsed.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				for _, banned := range forbidden {
					if path == banned {
						t.Fatalf("%s imports %q; collection must only observe", name, path)
					}
				}
			}
		}
	}
}

// DNS has no owner in this build. The model is designed to say so rather than
// omit the component, and this test records the gap so it is closed
// deliberately rather than noticed by accident.
func TestDNSHasNoCollectorYet(t *testing.T) {
	declaration, owned := safety.ConnectivityAuthority(connectivity.ComponentDNS)
	if !owned {
		t.Fatal("dns lost its declared owner")
	}
	if declaration.Source != "root.dns" {
		t.Fatalf("dns owner is %q", declaration.Source)
	}
	// No mapper produces a DNS observation, so nothing can emit one.
	collector := newCollector(t, "root.dns", policy.DomainRoot)
	_, err := collector.Emit(Observation{
		Component: connectivity.ComponentDNS,
		Lifecycle: connectivity.LifecycleReady,
		Reason:    connectivity.ReasonProbeSucceeded,
		Payload: connectivity.Payload{DNS: &connectivity.DNSPayload{
			ResolverClass: connectivity.ResolverSystem, Responding: true}},
	})
	if err != nil {
		t.Fatalf("the owner cannot emit for its own component: %v", err)
	}
	// The gap is that hexroute has no DNS observer to build that payload from.
	// If one is added, this test should be replaced by a mapper test.
}
