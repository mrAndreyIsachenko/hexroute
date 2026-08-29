package safety

import (
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestEveryComponentHasExactlyOneAuthority(t *testing.T) {
	seen := make(map[connectivity.Component]connectivity.SourceID)
	for _, declaration := range ConnectivitySources() {
		if declaration.Role != RoleAuthoritative {
			continue
		}
		if existing, taken := seen[declaration.Component]; taken {
			t.Fatalf("component %s owned by both %s and %s",
				declaration.Component, existing, declaration.Source)
		}
		seen[declaration.Component] = declaration.Source
	}
	for _, component := range connectivity.Components() {
		if _, owned := seen[component]; !owned {
			t.Fatalf("component %s has no authoritative source", component)
		}
	}
}

// The fixtures name their own sources. If the compiled envelope moves a
// component to a different owner or domain, the fixtures become wrong, and
// every downstream reducer test would silently exercise a fiction.
func TestFixtureSourcesMatchCompiledOwnership(t *testing.T) {
	for _, component := range connectivity.Components() {
		source, domain := connectivity.FixtureSource(component)
		declaration, ok := ConnectivityAuthority(component)
		if !ok {
			t.Fatalf("component %s has no authority", component)
		}
		if declaration.Source != source || declaration.Domain != domain {
			t.Fatalf("component %s: fixture claims %s/%s, envelope says %s/%s",
				component, domain, source, declaration.Domain, declaration.Source)
		}
	}
}

func TestAuthoritativeFactsAreAccepted(t *testing.T) {
	for _, fact := range connectivity.FixtureBaselineSet() {
		if err := ValidateAuthoritativeConnectivityFact(fact, fact.Domain); err != nil {
			t.Fatalf("%s: %v", fact.Component, err)
		}
	}
}

func TestFactOutsideItsDomainIsRejected(t *testing.T) {
	// A root-authenticated sender publishing a user-domain fact.
	fact := connectivity.FixtureBaseline(connectivity.ComponentUserAccess, 1)
	if err := ValidateAuthoritativeConnectivityFact(fact, policy.DomainRoot); !errors.Is(err, ErrDomainMismatch) {
		t.Fatalf("got %v, want %v", err, ErrDomainMismatch)
	}

	// A sender that claims the domain it is authenticated for, but names a
	// source belonging to the other domain.
	crossed := connectivity.FixtureBaseline(connectivity.ComponentPhysicalNetwork, 1)
	crossed.Domain = policy.DomainUser
	if err := ValidateAuthoritativeConnectivityFact(crossed, policy.DomainUser); !errors.Is(err, ErrDomainMismatch) {
		t.Fatalf("got %v, want %v", err, ErrDomainMismatch)
	}
}

func TestSourceCannotClaimAnotherComponent(t *testing.T) {
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	fact.SourceID = "root.routes"
	if err := ValidateAuthoritativeConnectivityFact(fact, policy.DomainRoot); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("got %v, want %v", err, ErrUnknownSource)
	}
}

func TestUnknownSourceIsRejected(t *testing.T) {
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	fact.SourceID = "root.somethingelse"
	if err := ValidateAuthoritativeConnectivityFact(fact, policy.DomainRoot); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("got %v, want %v", err, ErrUnknownSource)
	}
}

// A corroborating source is allowed to speak and its statement is classified,
// but it may not be accepted as the component's state.
func TestCorroboratingSourceCannotAssertState(t *testing.T) {
	fact := connectivity.FixtureBaseline(connectivity.ComponentDNS, 1)
	fact.SourceID = "root.probe"

	role, err := ClassifyConnectivityFact(fact, policy.DomainRoot)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if role != RoleCorroborating {
		t.Fatalf("got role %q, want %q", role, RoleCorroborating)
	}
	if err := ValidateAuthoritativeConnectivityFact(fact, policy.DomainRoot); !errors.Is(err, ErrCorroboratingWrite) {
		t.Fatalf("got %v, want %v", err, ErrCorroboratingWrite)
	}
}

// Corroboration is only meaningful where an owner exists to be corroborated.
func TestEveryCorroboratingSourceShadowsAnAuthority(t *testing.T) {
	for _, declaration := range ConnectivitySources() {
		if declaration.Role != RoleCorroborating {
			continue
		}
		authority, ok := ConnectivityAuthority(declaration.Component)
		if !ok {
			t.Fatalf("corroborating source %s has no authority to corroborate", declaration.Source)
		}
		if authority.Source == declaration.Source {
			t.Fatalf("source %s corroborates itself for %s", declaration.Source, declaration.Component)
		}
	}
}
