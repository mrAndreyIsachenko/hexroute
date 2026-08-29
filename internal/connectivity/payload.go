package connectivity

// Payload is the component-specific part of a fact.
//
// Exactly one member is set, and the set member must match the fact's
// component. Every field is an enumeration or a bounded count: there is no
// place in this type to put an address, a hostname, a route prefix, a
// selector, a filesystem path, a process argument or a credential.
type Payload struct {
	PhysicalNetwork *PhysicalNetworkPayload `json:"physical_network,omitempty"`
	DefaultPath     *DefaultPathPayload     `json:"default_path,omitempty"`
	DNS             *DNSPayload             `json:"dns,omitempty"`
	ScopedRoutes    *ScopedRoutesPayload    `json:"scoped_routes,omitempty"`
	Transports      *TransportsPayload      `json:"managed_transports,omitempty"`
	Relays          *RelaysPayload          `json:"relay_ingress,omitempty"`
	UserAccess      *UserAccessPayload      `json:"user_access,omitempty"`
	SessionExpiry   *SessionExpiryPayload   `json:"session_expiry,omitempty"`
}

// LinkClass describes the kind of physical link carrying the host, never which
// network it is attached to.
type LinkClass string

const (
	LinkNone     LinkClass = "none"
	LinkWired    LinkClass = "wired"
	LinkWireless LinkClass = "wireless"
	LinkCellular LinkClass = "cellular"
	LinkVirtual  LinkClass = "virtual"
)

func (class LinkClass) valid() bool {
	switch class {
	case LinkNone, LinkWired, LinkWireless, LinkCellular, LinkVirtual:
		return true
	default:
		return false
	}
}

type PhysicalNetworkPayload struct {
	LinkClass  LinkClass `json:"link_class"`
	LinkUp     bool      `json:"link_up"`
	HasCarrier bool      `json:"has_carrier"`
}

func (payload PhysicalNetworkPayload) validate() error {
	if !payload.LinkClass.valid() {
		return ErrInvalidPayload
	}
	// A link cannot carry traffic it does not have.
	if payload.HasCarrier && !payload.LinkUp {
		return ErrInvalidPayload
	}
	if payload.LinkClass == LinkNone && (payload.LinkUp || payload.HasCarrier) {
		return ErrInvalidPayload
	}
	return nil
}

// PathClass describes how the default path leaves the host.
type PathClass string

const (
	PathNone     PathClass = "none"
	PathDirect   PathClass = "direct"
	PathTunneled PathClass = "tunneled"
)

func (class PathClass) valid() bool {
	switch class {
	case PathNone, PathDirect, PathTunneled:
		return true
	default:
		return false
	}
}

type DefaultPathPayload struct {
	PathClass      PathClass `json:"path_class"`
	GatewayPresent bool      `json:"gateway_present"`
}

func (payload DefaultPathPayload) validate() error {
	if !payload.PathClass.valid() {
		return ErrInvalidPayload
	}
	if payload.PathClass == PathNone && payload.GatewayPresent {
		return ErrInvalidPayload
	}
	return nil
}

// ResolverClass describes the kind of resolver in use, not its address.
type ResolverClass string

const (
	ResolverNone      ResolverClass = "none"
	ResolverSystem    ResolverClass = "system"
	ResolverScoped    ResolverClass = "scoped"
	ResolverEncrypted ResolverClass = "encrypted"
)

func (class ResolverClass) valid() bool {
	switch class {
	case ResolverNone, ResolverSystem, ResolverScoped, ResolverEncrypted:
		return true
	default:
		return false
	}
}

type DNSPayload struct {
	ResolverClass  ResolverClass `json:"resolver_class"`
	Responding     bool          `json:"responding"`
	ScopedDomains  uint16        `json:"scoped_domains"`
	FailingDomains uint16        `json:"failing_domains"`
}

func (payload DNSPayload) validate() error {
	if !payload.ResolverClass.valid() {
		return ErrInvalidPayload
	}
	if payload.ScopedDomains > MaxComponentCount || payload.FailingDomains > MaxComponentCount {
		return ErrInvalidPayload
	}
	if payload.FailingDomains > payload.ScopedDomains {
		return ErrInvalidPayload
	}
	if payload.ResolverClass == ResolverNone && payload.Responding {
		return ErrInvalidPayload
	}
	return nil
}

type ScopedRoutesPayload struct {
	Configured  uint16 `json:"configured"`
	Installed   uint16 `json:"installed"`
	Conflicting uint16 `json:"conflicting"`
}

func (payload ScopedRoutesPayload) validate() error {
	if payload.Configured > MaxComponentCount ||
		payload.Installed > MaxComponentCount ||
		payload.Conflicting > MaxComponentCount {
		return ErrInvalidPayload
	}
	if payload.Installed > payload.Configured || payload.Conflicting > payload.Configured {
		return ErrInvalidPayload
	}
	return nil
}

type TransportsPayload struct {
	Configured uint16 `json:"configured"`
	Ready      uint16 `json:"ready"`
	Degraded   uint16 `json:"degraded"`
}

func (payload TransportsPayload) validate() error {
	if payload.Configured > MaxComponentCount ||
		payload.Ready > MaxComponentCount ||
		payload.Degraded > MaxComponentCount {
		return ErrInvalidPayload
	}
	if payload.Ready+payload.Degraded > payload.Configured {
		return ErrInvalidPayload
	}
	return nil
}

// SelectedClass says which class of ingress is carrying traffic, never which
// ingress it is.
type SelectedClass string

const (
	SelectedNone    SelectedClass = "none"
	SelectedPrimary SelectedClass = "primary"
	SelectedReserve SelectedClass = "reserve"
)

func (class SelectedClass) valid() bool {
	switch class {
	case SelectedNone, SelectedPrimary, SelectedReserve:
		return true
	default:
		return false
	}
}

type RelaysPayload struct {
	Configured    uint16        `json:"configured"`
	Reachable     uint16        `json:"reachable"`
	Reserve       uint16        `json:"reserve"`
	SelectedClass SelectedClass `json:"selected_class"`
}

func (payload RelaysPayload) validate() error {
	if !payload.SelectedClass.valid() {
		return ErrInvalidPayload
	}
	if payload.Configured > MaxComponentCount ||
		payload.Reachable > MaxComponentCount ||
		payload.Reserve > MaxComponentCount {
		return ErrInvalidPayload
	}
	if payload.Reachable > payload.Configured || payload.Reserve > payload.Configured {
		return ErrInvalidPayload
	}
	if payload.SelectedClass != SelectedNone && payload.Configured == 0 {
		return ErrInvalidPayload
	}
	if payload.SelectedClass == SelectedReserve && payload.Reserve == 0 {
		return ErrInvalidPayload
	}
	return nil
}

// ProfileClass says whether user access is configured at all. It carries no
// profile name, server, organisation or user identity.
type ProfileClass string

const (
	ProfileNone       ProfileClass = "none"
	ProfileConfigured ProfileClass = "configured"
)

func (class ProfileClass) valid() bool {
	switch class {
	case ProfileNone, ProfileConfigured:
		return true
	default:
		return false
	}
}

type UserAccessPayload struct {
	ProfileClass  ProfileClass `json:"profile_class"`
	Connected     bool         `json:"connected"`
	Authenticated bool         `json:"authenticated"`
}

func (payload UserAccessPayload) validate() error {
	if !payload.ProfileClass.valid() {
		return ErrInvalidPayload
	}
	if payload.ProfileClass == ProfileNone && (payload.Connected || payload.Authenticated) {
		return ErrInvalidPayload
	}
	if payload.Connected && !payload.Authenticated {
		return ErrInvalidPayload
	}
	return nil
}

// ExpiryClass buckets how close a session is to expiring. The exact expiry
// instant is a session identifier in disguise, so only the bucket travels.
type ExpiryClass string

const (
	ExpiryNone     ExpiryClass = "none"
	ExpiryValid    ExpiryClass = "valid"
	ExpiryExpiring ExpiryClass = "expiring"
	ExpiryExpired  ExpiryClass = "expired"
)

func (class ExpiryClass) valid() bool {
	switch class {
	case ExpiryNone, ExpiryValid, ExpiryExpiring, ExpiryExpired:
		return true
	default:
		return false
	}
}

type SessionExpiryPayload struct {
	ExpiryClass ExpiryClass `json:"expiry_class"`
	Sessions    uint16      `json:"sessions"`
}

func (payload SessionExpiryPayload) validate() error {
	if !payload.ExpiryClass.valid() {
		return ErrInvalidPayload
	}
	if payload.Sessions > MaxComponentCount {
		return ErrInvalidPayload
	}
	if payload.ExpiryClass == ExpiryNone && payload.Sessions > 0 {
		return ErrInvalidPayload
	}
	if payload.ExpiryClass != ExpiryNone && payload.Sessions == 0 {
		return ErrInvalidPayload
	}
	return nil
}

// component reports which component the payload describes, and whether exactly
// one member is set.
func (payload Payload) component() (Component, bool) {
	var found Component
	count := 0
	if payload.PhysicalNetwork != nil {
		found, count = ComponentPhysicalNetwork, count+1
	}
	if payload.DefaultPath != nil {
		found, count = ComponentDefaultPath, count+1
	}
	if payload.DNS != nil {
		found, count = ComponentDNS, count+1
	}
	if payload.ScopedRoutes != nil {
		found, count = ComponentScopedRoutes, count+1
	}
	if payload.Transports != nil {
		found, count = ComponentTransports, count+1
	}
	if payload.Relays != nil {
		found, count = ComponentRelays, count+1
	}
	if payload.UserAccess != nil {
		found, count = ComponentUserAccess, count+1
	}
	if payload.SessionExpiry != nil {
		found, count = ComponentSessionExpiry, count+1
	}
	return found, count == 1
}

func (payload Payload) validate() error {
	switch {
	case payload.PhysicalNetwork != nil:
		return payload.PhysicalNetwork.validate()
	case payload.DefaultPath != nil:
		return payload.DefaultPath.validate()
	case payload.DNS != nil:
		return payload.DNS.validate()
	case payload.ScopedRoutes != nil:
		return payload.ScopedRoutes.validate()
	case payload.Transports != nil:
		return payload.Transports.validate()
	case payload.Relays != nil:
		return payload.Relays.validate()
	case payload.UserAccess != nil:
		return payload.UserAccess.validate()
	case payload.SessionExpiry != nil:
		return payload.SessionExpiry.validate()
	default:
		return ErrInvalidPayload
	}
}
