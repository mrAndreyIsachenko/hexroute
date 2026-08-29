package safety

import (
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

var (
	ErrUnknownSource      = errors.New("connectivity source is not allowlisted")
	ErrOwnershipConflict  = errors.New("connectivity source does not own the component")
	ErrDomainMismatch     = errors.New("connectivity fact crosses its source domain")
	ErrCorroboratingWrite = errors.New("corroborating source cannot assert authoritative state")
)

// SourceRole separates the one source that defines a component's state from
// sources that merely agree or disagree with it.
type SourceRole string

const (
	// RoleAuthoritative is the single owner of a component's state.
	RoleAuthoritative SourceRole = "authoritative"
	// RoleCorroborating is evidence. It is retained and shown, and it can
	// contradict the owner, but it can never replace the owner's state.
	RoleCorroborating SourceRole = "corroborating"
)

// SourceDeclaration binds one collector to one component and role.
type SourceDeclaration struct {
	Source    connectivity.SourceID
	Domain    policy.Domain
	Component connectivity.Component
	Role      SourceRole
}

// connectivitySources is the compiled ownership envelope.
//
// Exactly one authoritative source exists per component, and the split follows
// the privilege boundary: root owns what only root can see, and the user
// domain owns access and session state so that root never needs a credential
// to describe them.
var connectivitySources = []SourceDeclaration{
	{"root.network", policy.DomainRoot, connectivity.ComponentPhysicalNetwork, RoleAuthoritative},
	{"root.network", policy.DomainRoot, connectivity.ComponentDefaultPath, RoleAuthoritative},
	{"root.dns", policy.DomainRoot, connectivity.ComponentDNS, RoleAuthoritative},
	{"root.routes", policy.DomainRoot, connectivity.ComponentScopedRoutes, RoleAuthoritative},
	{"root.transports", policy.DomainRoot, connectivity.ComponentTransports, RoleAuthoritative},
	{"root.relays", policy.DomainRoot, connectivity.ComponentRelays, RoleAuthoritative},
	{"user.access", policy.DomainUser, connectivity.ComponentUserAccess, RoleAuthoritative},
	{"user.session", policy.DomainUser, connectivity.ComponentSessionExpiry, RoleAuthoritative},

	// Probes corroborate what the owners assert. They run on a different
	// schedule and can disagree; that disagreement is evidence, not an update.
	{"root.probe", policy.DomainRoot, connectivity.ComponentDefaultPath, RoleCorroborating},
	{"root.probe", policy.DomainRoot, connectivity.ComponentDNS, RoleCorroborating},
	{"root.probe", policy.DomainRoot, connectivity.ComponentRelays, RoleCorroborating},
	{"user.probe", policy.DomainUser, connectivity.ComponentUserAccess, RoleCorroborating},
}

type sourceKey struct {
	source    connectivity.SourceID
	component connectivity.Component
}

var (
	connectivityByKey       map[sourceKey]SourceDeclaration
	connectivityAuthorities map[connectivity.Component]SourceDeclaration
)

func init() {
	connectivityByKey = make(map[sourceKey]SourceDeclaration, len(connectivitySources))
	connectivityAuthorities = make(map[connectivity.Component]SourceDeclaration)
	for _, declaration := range connectivitySources {
		key := sourceKey{source: declaration.Source, component: declaration.Component}
		if _, duplicate := connectivityByKey[key]; duplicate {
			panic("connectivity source declared twice for one component")
		}
		connectivityByKey[key] = declaration
		if declaration.Role != RoleAuthoritative {
			continue
		}
		if existing, taken := connectivityAuthorities[declaration.Component]; taken {
			panic(fmt.Sprintf(
				"component %q has two authoritative sources: %q and %q",
				declaration.Component, existing.Source, declaration.Source))
		}
		connectivityAuthorities[declaration.Component] = declaration
	}
	for _, component := range connectivity.Components() {
		if _, owned := connectivityAuthorities[component]; !owned {
			panic(fmt.Sprintf("component %q has no authoritative source", component))
		}
	}
}

// ConnectivityAuthority returns the single owner of a component.
func ConnectivityAuthority(component connectivity.Component) (SourceDeclaration, bool) {
	declaration, ok := connectivityAuthorities[component]
	return declaration, ok
}

// ConnectivitySources returns the compiled declarations in declaration order.
func ConnectivitySources() []SourceDeclaration {
	out := make([]SourceDeclaration, len(connectivitySources))
	copy(out, connectivitySources)
	return out
}

// ClassifyConnectivityFact resolves the role a fact carries.
//
// authenticatedDomain is the domain the transport proved, not the domain the
// fact claims. A fact whose claimed domain differs from the proven one is a
// domain mismatch even when the source would otherwise own the component.
func ClassifyConnectivityFact(
	fact connectivity.Fact,
	authenticatedDomain policy.Domain,
) (SourceRole, error) {
	if fact.Domain != authenticatedDomain {
		return "", fmt.Errorf("%w: claimed=%q authenticated=%q",
			ErrDomainMismatch, fact.Domain, authenticatedDomain)
	}
	declaration, known := connectivityByKey[sourceKey{
		source:    fact.SourceID,
		component: fact.Component,
	}]
	if !known {
		return "", fmt.Errorf("%w: source=%q component=%q",
			ErrUnknownSource, fact.SourceID, fact.Component)
	}
	if declaration.Domain != authenticatedDomain {
		return "", fmt.Errorf("%w: source=%q declared=%q authenticated=%q",
			ErrDomainMismatch, fact.SourceID, declaration.Domain, authenticatedDomain)
	}
	return declaration.Role, nil
}

// ValidateAuthoritativeConnectivityFact accepts a fact only as the owner's own
// statement about its component.
func ValidateAuthoritativeConnectivityFact(
	fact connectivity.Fact,
	authenticatedDomain policy.Domain,
) error {
	role, err := ClassifyConnectivityFact(fact, authenticatedDomain)
	if err != nil {
		return err
	}
	if role != RoleAuthoritative {
		return fmt.Errorf("%w: source=%q component=%q",
			ErrCorroboratingWrite, fact.SourceID, fact.Component)
	}
	return nil
}
