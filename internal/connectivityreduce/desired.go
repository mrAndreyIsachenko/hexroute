package connectivityreduce

import (
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// EffectivePolicy is the already-compiled, already-revalidated policy this
// read model is allowed to consult.
//
// It is an input rather than something compiled here: the policy foundation
// owns compilation and revalidation, and reading it as a value keeps reduction
// pure and keeps a replay bound to the policy it actually ran under.
type EffectivePolicy struct {
	Descriptor PolicyDescriptor  `json:"descriptor"`
	Components []ComponentPolicy `json:"components"`
}

// ComponentPolicy is what the active policy asks of one component.
type ComponentPolicy struct {
	Component connectivity.Component `json:"component"`
	// Managed is false when policy does not ask for this component at all.
	Managed bool        `json:"managed"`
	Expect  Expectation `json:"expect"`
}

// Expectation is a bounded statement of what a managed component should be.
//
// Pins are optional and typed per component; an absent pin means policy does
// not constrain that aspect. Like a fact payload, an expectation has no field
// that can hold an endpoint, prefix, selector, path or credential.
type Expectation struct {
	// Lifecycle is the state policy requires. Only ready and not_applicable
	// are meaningful requirements: policy never asks for degradation.
	Lifecycle connectivity.Lifecycle `json:"lifecycle"`

	PathClass     *connectivity.PathClass     `json:"path_class,omitempty"`
	ResolverClass *connectivity.ResolverClass `json:"resolver_class,omitempty"`
	SelectedClass *connectivity.SelectedClass `json:"selected_class,omitempty"`
	ProfileClass  *connectivity.ProfileClass  `json:"profile_class,omitempty"`

	MinInstalledRoutes *uint16 `json:"min_installed_routes,omitempty"`
	MinReadyTransports *uint16 `json:"min_ready_transports,omitempty"`
	MinReachableRelays *uint16 `json:"min_reachable_relays,omitempty"`
}

// DesiredComponent is the normalized desire for one component.
type DesiredComponent struct {
	Component connectivity.Component `json:"component"`
	Domain    policy.Domain          `json:"domain"`
	Managed   bool                   `json:"managed"`
	Expect    Expectation            `json:"expect"`
}

// DesiredState is the full desire, always covering every component.
type DesiredState struct {
	Schema     string             `json:"schema"`
	Version    uint16             `json:"version"`
	Policy     PolicyDescriptor   `json:"policy"`
	Authorized bool               `json:"authorized"`
	Components []DesiredComponent `json:"components"`
}

const (
	DesiredSchema         = "hexroute.connectivity-desired.v1"
	DesiredSchemaVersion  = uint16(1)
	DiffSchema            = "hexroute.connectivity-diff.v1"
	DiffSchemaVersion     = uint16(1)
	ProposalSchema        = "hexroute.connectivity-proposal.v1"
	ProposalSchemaVersion = uint16(1)
)

// Digest returns the canonical digest of the desired state.
func (desired DesiredState) Digest() (string, error) {
	digest, _, err := policy.CanonicalSHA256(desired)
	if err != nil {
		return "", ErrInvalidSnapshot
	}
	return digest, nil
}

// Desire adapts effective policy into normalized desired state.
//
// It reads policy and writes nothing. When the policy cannot authorize a
// desire at all, the result is an unauthorized desired state with no
// expectations rather than an empty one that could be mistaken for "nothing
// is wanted".
func Desire(effective EffectivePolicy) (DesiredState, error) {
	desired := DesiredState{
		Schema:     DesiredSchema,
		Version:    DesiredSchemaVersion,
		Policy:     effective.Descriptor,
		Authorized: effective.Descriptor.Authorized(),
	}
	byComponent := make(map[connectivity.Component]ComponentPolicy, len(effective.Components))
	for _, entry := range effective.Components {
		if !entry.Component.Valid() {
			return DesiredState{}, fmt.Errorf("%w: component %q", ErrInvalidInput, entry.Component)
		}
		if _, duplicate := byComponent[entry.Component]; duplicate {
			return DesiredState{}, fmt.Errorf("%w: component %q declared twice",
				ErrInvalidInput, entry.Component)
		}
		if entry.Managed {
			if err := validateExpectation(entry.Component, entry.Expect); err != nil {
				return DesiredState{}, err
			}
		}
		byComponent[entry.Component] = entry
	}

	for _, component := range connectivity.Components() {
		declaration, ok := safety.ConnectivityAuthority(component)
		if !ok {
			return DesiredState{}, fmt.Errorf("%w: component %q has no owner",
				ErrInvalidInput, component)
		}
		entry, declared := byComponent[component]
		record := DesiredComponent{
			Component: component,
			Domain:    declaration.Domain,
			Managed:   declared && entry.Managed && desired.Authorized,
		}
		if record.Managed {
			record.Expect = entry.Expect
		} else {
			record.Expect = Expectation{Lifecycle: connectivity.LifecycleNotApplicable}
		}
		desired.Components = append(desired.Components, record)
	}
	return desired, nil
}

// validateExpectation refuses a pin that does not belong to its component, so
// a policy cannot express a resolver class for a route table.
func validateExpectation(component connectivity.Component, expect Expectation) error {
	switch expect.Lifecycle {
	case connectivity.LifecycleReady, connectivity.LifecycleNotApplicable:
	default:
		return fmt.Errorf("%w: %q cannot be required", ErrInvalidInput, expect.Lifecycle)
	}
	allowed := func(condition bool, name string) error {
		if condition {
			return nil
		}
		return fmt.Errorf("%w: %q cannot pin %s", ErrInvalidInput, component, name)
	}
	if expect.PathClass != nil {
		if err := allowed(component == connectivity.ComponentDefaultPath, "path class"); err != nil {
			return err
		}
	}
	if expect.ResolverClass != nil {
		if err := allowed(component == connectivity.ComponentDNS, "resolver class"); err != nil {
			return err
		}
	}
	if expect.SelectedClass != nil {
		if err := allowed(component == connectivity.ComponentRelays, "selected class"); err != nil {
			return err
		}
	}
	if expect.ProfileClass != nil {
		if err := allowed(component == connectivity.ComponentUserAccess, "profile class"); err != nil {
			return err
		}
	}
	if expect.MinInstalledRoutes != nil {
		if err := allowed(component == connectivity.ComponentScopedRoutes, "route count"); err != nil {
			return err
		}
	}
	if expect.MinReadyTransports != nil {
		if err := allowed(component == connectivity.ComponentTransports, "transport count"); err != nil {
			return err
		}
	}
	if expect.MinReachableRelays != nil {
		if err := allowed(component == connectivity.ComponentRelays, "relay count"); err != nil {
			return err
		}
	}
	return nil
}
