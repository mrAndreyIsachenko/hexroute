// Package connectivity defines the typed facts that describe the observed
// state of one connectivity component.
//
// A fact is a complete statement about a component, never a patch and never an
// instruction. It carries no address, hostname, route prefix, selector,
// filesystem path, process detail or credential reference: every payload field
// is a bounded enumeration or a small count. That is stricter than the local
// model strictly needs, and it is deliberate — it makes the redacted cloud
// projection a subset of an already redacted model rather than a filter that
// has to be trusted.
package connectivity

import (
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	// FactSchema names the wire contract for a single connectivity fact.
	FactSchema = "hexroute.connectivity-fact.v1"
	// FactSchemaVersion is bumped only for an incompatible fact change.
	FactSchemaVersion uint16 = 1

	// MaxEncodedFactBytes bounds one encoded fact.
	MaxEncodedFactBytes = 4 * 1024
	// MaxIdentifierBytes bounds every opaque identifier carried by a fact.
	MaxIdentifierBytes = 64
	// MaxComponentCount bounds every count a payload may report. A host that
	// configures more than this many routes or transports is misconfigured,
	// and an unbounded count would let a source grow the encoded fact.
	MaxComponentCount uint16 = 4096
)

// Component names one separately inspectable part of host connectivity. The
// set is closed: a fact naming anything else is rejected rather than stored as
// an unknown component.
type Component string

const (
	ComponentPhysicalNetwork Component = "physical_network"
	ComponentDefaultPath     Component = "default_path"
	ComponentDNS             Component = "dns"
	ComponentScopedRoutes    Component = "scoped_routes"
	ComponentTransports      Component = "managed_transports"
	ComponentRelays          Component = "relay_ingress"
	ComponentUserAccess      Component = "user_access"
	ComponentSessionExpiry   Component = "session_expiry"
)

// Components returns every configured component in a stable order.
func Components() []Component {
	return []Component{
		ComponentPhysicalNetwork,
		ComponentDefaultPath,
		ComponentDNS,
		ComponentScopedRoutes,
		ComponentTransports,
		ComponentRelays,
		ComponentUserAccess,
		ComponentSessionExpiry,
	}
}

// TimeSensitive reports whether a component's health can be invalidated by the
// host having been asleep.
//
// Scoped routes are the exception: a route table does not stop being installed
// because the machine slept. What traverses it can, and every one of those is
// time-sensitive, so a wake still degrades the picture without pretending the
// route table itself became unknown.
func (component Component) TimeSensitive() bool {
	return component != ComponentScopedRoutes
}

// Valid reports whether the component is one this build knows about.
func (component Component) Valid() bool {
	switch component {
	case ComponentPhysicalNetwork,
		ComponentDefaultPath,
		ComponentDNS,
		ComponentScopedRoutes,
		ComponentTransports,
		ComponentRelays,
		ComponentUserAccess,
		ComponentSessionExpiry:
		return true
	default:
		return false
	}
}

// Lifecycle is what a source may assert about its component.
//
// It deliberately excludes stale and conflict: those are conclusions the
// aggregate draws from freshness deadlines and ownership, and a source that
// could declare itself stale or in conflict would be able to describe the
// aggregate's own bookkeeping.
type Lifecycle string

const (
	LifecycleUnknown       Lifecycle = "unknown"
	LifecycleReady         Lifecycle = "ready"
	LifecycleDegraded      Lifecycle = "degraded"
	LifecycleFailed        Lifecycle = "failed"
	LifecycleNotApplicable Lifecycle = "not_applicable"
)

// Valid reports whether the lifecycle state may appear in a fact.
func (lifecycle Lifecycle) Valid() bool {
	switch lifecycle {
	case LifecycleUnknown,
		LifecycleReady,
		LifecycleDegraded,
		LifecycleFailed,
		LifecycleNotApplicable:
		return true
	default:
		return false
	}
}

// Reason is the bounded explanation a source attaches to its lifecycle state.
type Reason string

const (
	ReasonNone              Reason = "none"
	ReasonBaseline          Reason = "baseline"
	ReasonProbeSucceeded    Reason = "probe_succeeded"
	ReasonProbeFailed       Reason = "probe_failed"
	ReasonLinkChanged       Reason = "link_changed"
	ReasonPolicyApplied     Reason = "policy_applied"
	ReasonOwnerUnavailable  Reason = "owner_unavailable"
	ReasonWakeRebaseline    Reason = "wake_rebaseline"
	ReasonBootRebaseline    Reason = "boot_rebaseline"
	ReasonNotConfigured     Reason = "not_configured"
	ReasonExpiryApproaching Reason = "expiry_approaching"
	ReasonExpired           Reason = "expired"
)

// Valid reports whether the reason is allowlisted.
func (reason Reason) Valid() bool {
	switch reason {
	case ReasonNone,
		ReasonBaseline,
		ReasonProbeSucceeded,
		ReasonProbeFailed,
		ReasonLinkChanged,
		ReasonPolicyApplied,
		ReasonOwnerUnavailable,
		ReasonWakeRebaseline,
		ReasonBootRebaseline,
		ReasonNotConfigured,
		ReasonExpiryApproaching,
		ReasonExpired:
		return true
	default:
		return false
	}
}

// SourceID identifies one collector. Ownership is resolved by this value, so
// it is an opaque bounded identifier and never a path or command.
type SourceID string

// Fact is one source's complete statement about one component.
type Fact struct {
	Schema         string        `json:"schema"`
	Version        uint16        `json:"version"`
	EventID        string        `json:"event_id"`
	Domain         policy.Domain `json:"domain"`
	Component      Component     `json:"component"`
	SourceID       SourceID      `json:"source_id"`
	BootID         string        `json:"boot_id"`
	SourceSequence uint64        `json:"source_sequence"`
	ObservedAt     time.Time     `json:"observed_at"`
	MonotonicTick  control.Tick  `json:"monotonic_tick"`
	// FreshnessDeadline is a monotonic tick in the same boot as MonotonicTick.
	// It is meaningless across a boot change, which is why BootID travels with
	// it and a boot change invalidates it rather than extending it.
	FreshnessDeadline control.Tick `json:"freshness_deadline"`
	Lifecycle         Lifecycle    `json:"lifecycle"`
	Reason            Reason       `json:"reason"`
	// Baseline marks a fact the source offers as a complete restatement of its
	// component. Only a baseline can clear a recorded sequence gap.
	Baseline bool    `json:"baseline"`
	Payload  Payload `json:"payload"`
}
