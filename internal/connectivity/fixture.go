package connectivity

import (
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// This file holds synthetic fixtures used by tests and by the offline replay
// harness. Every value here is invented. The repository is public, so a
// fixture may never carry a live endpoint, route prefix, selector, profile,
// session identity or credential — and because payloads cannot express those
// at all, a fixture cannot acquire one by accident either.

// FixtureBootID is the invented boot identity every fixture shares.
const FixtureBootID = "boot-0000000000000000"

// fixtureEventID derives a stable synthetic UUID from an index so fixtures are
// byte-stable across runs. A real collector mints a random one.
func fixtureEventID(index int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", index)
}

// FixtureObservedAt is the invented wall-clock instant every fixture shares.
var FixtureObservedAt = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// FixturePayloads returns one representative payload per component, in the
// stable order of Components.
func FixturePayloads() map[Component]Payload {
	return map[Component]Payload{
		ComponentPhysicalNetwork: {PhysicalNetwork: &PhysicalNetworkPayload{
			LinkClass: LinkWired, LinkUp: true, HasCarrier: true,
		}},
		ComponentDefaultPath: {DefaultPath: &DefaultPathPayload{
			PathClass: PathTunneled, GatewayPresent: true,
		}},
		ComponentDNS: {DNS: &DNSPayload{
			ResolverClass: ResolverScoped, Responding: true,
			ScopedDomains: 12, FailingDomains: 0,
		}},
		ComponentScopedRoutes: {ScopedRoutes: &ScopedRoutesPayload{
			Configured: 7, Installed: 7, Conflicting: 0,
		}},
		ComponentTransports: {Transports: &TransportsPayload{
			Configured: 3, Ready: 3, Degraded: 0,
		}},
		ComponentRelays: {Relays: &RelaysPayload{
			Configured: 3, Reachable: 3, Reserve: 1, SelectedClass: SelectedPrimary,
		}},
		ComponentUserAccess: {UserAccess: &UserAccessPayload{
			ProfileClass: ProfileConfigured, Connected: true, Authenticated: true,
		}},
		ComponentSessionExpiry: {SessionExpiry: &SessionExpiryPayload{
			ExpiryClass: ExpiryValid, Sessions: 1,
		}},
	}
}

// FixtureSource returns the synthetic source and domain used for a component's
// fixtures. It mirrors the compiled ownership split without importing it: the
// safety package owns the real table, and a fixture that disagrees with it is
// exactly the kind of drift the ownership tests are there to catch.
func FixtureSource(component Component) (SourceID, policy.Domain) {
	switch component {
	case ComponentUserAccess:
		return "user.access", policy.DomainUser
	case ComponentSessionExpiry:
		return "user.session", policy.DomainUser
	case ComponentPhysicalNetwork, ComponentDefaultPath:
		return "root.network", policy.DomainRoot
	case ComponentDNS:
		return "root.dns", policy.DomainRoot
	case ComponentScopedRoutes:
		return "root.routes", policy.DomainRoot
	case ComponentTransports:
		return "root.transports", policy.DomainRoot
	case ComponentRelays:
		return "root.relays", policy.DomainRoot
	default:
		return "", ""
	}
}

// FixtureBaseline builds one valid complete baseline fact for a component.
func FixtureBaseline(component Component, sequence uint64) Fact {
	source, domain := FixtureSource(component)
	payloads := FixturePayloads()
	tick := control.Tick(1000 + sequence)
	return Fact{
		Schema:            FactSchema,
		Version:           FactSchemaVersion,
		EventID:           fixtureEventID(int(sequence)),
		Domain:            domain,
		Component:         component,
		SourceID:          source,
		BootID:            FixtureBootID,
		SourceSequence:    sequence,
		ObservedAt:        FixtureObservedAt.Add(time.Duration(sequence) * time.Second),
		MonotonicTick:     tick,
		FreshnessDeadline: tick + 300,
		Lifecycle:         LifecycleReady,
		Reason:            ReasonBaseline,
		Baseline:          true,
		Payload:           payloads[component],
	}
}

// FixtureBaselineSet returns one baseline fact per component, sequenced from 1.
func FixtureBaselineSet() []Fact {
	components := Components()
	facts := make([]Fact, 0, len(components))
	for index, component := range components {
		facts = append(facts, FixtureBaseline(component, uint64(index+1)))
	}
	return facts
}
