package connectivitycollect

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplace"
)

// The mappers below are pure functions from an observation the root daemon
// already performs to a bounded payload. None of them runs a command, opens a
// socket or reads a file: whatever the daemon saw is all they get.
//
// Addresses, interface names and gateways are read to decide a class or a
// count and are never carried into the payload, which has nowhere to put them.

// MapPhysicalNetwork describes the link the host is attached to.
func MapPhysicalNetwork(network observe.PhysicalNetwork, err error) Observation {
	observation := Observation{Component: connectivity.ComponentPhysicalNetwork}
	if err != nil {
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{
			PhysicalNetwork: &connectivity.PhysicalNetworkPayload{LinkClass: connectivity.LinkNone},
		}
		return observation
	}
	payload := connectivity.PhysicalNetworkPayload{LinkClass: connectivity.LinkNone}
	switch {
	case network.Interface == "":
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonLinkChanged
	case network.Link == observe.LinkStateUp:
		// The observer does not distinguish wired from wireless, so the class
		// stays the honest one rather than a guess dressed as detail.
		payload.LinkClass = connectivity.LinkWired
		payload.LinkUp = true
		payload.HasCarrier = network.Gateway.IsValid()
		if payload.HasCarrier {
			observation.Lifecycle = connectivity.LifecycleReady
			observation.Reason = connectivity.ReasonProbeSucceeded
		} else {
			observation.Lifecycle = connectivity.LifecycleDegraded
			observation.Reason = connectivity.ReasonLinkChanged
		}
	case network.Link == observe.LinkStateDown:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonLinkChanged
	default:
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
	}
	observation.Payload = connectivity.Payload{PhysicalNetwork: &payload}
	return observation
}

// MapDefaultPath describes how traffic leaves the host: through a tunnel the
// daemon knows about, directly, or not at all.
func MapDefaultPath(
	network observe.PhysicalNetwork,
	tunnels []observe.TUNInterface,
	err error,
) Observation {
	observation := Observation{Component: connectivity.ComponentDefaultPath}
	payload := connectivity.DefaultPathPayload{PathClass: connectivity.PathNone}
	if err != nil {
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{DefaultPath: &payload}
		return observation
	}
	tunnelled := false
	for _, tunnel := range tunnels {
		if len(tunnel.Addresses) > 0 {
			tunnelled = true
			break
		}
	}
	switch {
	case tunnelled:
		payload.PathClass = connectivity.PathTunneled
		payload.GatewayPresent = network.Gateway.IsValid()
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	case network.Gateway.IsValid():
		payload.PathClass = connectivity.PathDirect
		payload.GatewayPresent = true
		observation.Lifecycle = connectivity.LifecycleDegraded
		observation.Reason = connectivity.ReasonLinkChanged
	default:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonLinkChanged
	}
	observation.Payload = connectivity.Payload{DefaultPath: &payload}
	return observation
}

// MapScopedRoutes counts how many of the configured scoped routes are present
// on the interface policy expects, and how many landed somewhere else.
// MapScopedRoutes describes the scoped routes against what the configuration
// asks of them.
//
// It is given a judgement the read model reached by calling the one rule that
// decides where a route belongs, rather than comparing every route against a
// single interface. Doing the latter counted a route correctly on another link
// as a conflict, and made ready unreachable for any configuration whose roles
// span more than one link: read 2026-10-08, 14 of 21 conflicting and all 14
// where their role asks them to be.
func MapScopedRoutes(placement routeplace.Placement, err error) Observation {
	observation := Observation{Component: connectivity.ComponentScopedRoutes}
	payload := connectivity.ScopedRoutesPayload{Configured: placement.Configured}
	if err != nil {
		// Nothing was judged. A count of zero here would read as "nothing
		// diverges", which is the opposite of "nothing was measured".
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{ScopedRoutes: &payload}
		return observation
	}
	if placement.Configured == 0 {
		observation.Lifecycle = connectivity.LifecycleNotApplicable
		observation.Reason = connectivity.ReasonNotConfigured
		observation.Payload = connectivity.Payload{ScopedRoutes: &payload}
		return observation
	}
	payload.Installed = placement.Installed
	payload.Conflicting = placement.Conflicting
	payload.Missing = placement.Missing
	switch {
	// Ready does not require every declared route to be present: the
	// configuration asks for none of the fallback routes while normal Codex is
	// reachable, and a condition requiring them could never hold.
	case placement.Conflicting == 0 && placement.Missing == 0:
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	case placement.Installed == 0:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonProbeFailed
	default:
		observation.Lifecycle = connectivity.LifecycleDegraded
		observation.Reason = connectivity.ReasonProbeFailed
	}
	observation.Payload = connectivity.Payload{ScopedRoutes: &payload}
	return observation
}

// MapTransports describes the managed transport processes.
func MapTransports(configured uint16, process observe.ProcessObservation, err error) Observation {
	observation := Observation{Component: connectivity.ComponentTransports}
	payload := connectivity.TransportsPayload{Configured: configured}
	if err != nil {
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{Transports: &payload}
		return observation
	}
	if configured == 0 {
		observation.Lifecycle = connectivity.LifecycleNotApplicable
		observation.Reason = connectivity.ReasonNotConfigured
		observation.Payload = connectivity.Payload{Transports: &payload}
		return observation
	}
	if process.Running {
		payload.Ready = 1
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	} else {
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonProbeFailed
	}
	observation.Payload = connectivity.Payload{Transports: &payload}
	return observation
}

// MapRelays counts reachable ingress endpoints. Which endpoint is which never
// leaves the mapper: only how many there are, how many answered, how many are
// held in reserve, and which class is carrying traffic.
func MapRelays(
	readiness []observe.ReadinessObservation,
	reserve uint16,
	selected connectivity.SelectedClass,
) Observation {
	observation := Observation{Component: connectivity.ComponentRelays}
	payload := connectivity.RelaysPayload{
		Configured: uint16(len(readiness)), Reserve: reserve, SelectedClass: selected,
	}
	for _, endpoint := range readiness {
		if endpoint.Ready {
			payload.Reachable++
		}
	}
	if payload.Reserve > payload.Configured {
		payload.Reserve = payload.Configured
	}
	if payload.Configured == 0 {
		payload.SelectedClass = connectivity.SelectedNone
		observation.Lifecycle = connectivity.LifecycleNotApplicable
		observation.Reason = connectivity.ReasonNotConfigured
		observation.Payload = connectivity.Payload{Relays: &payload}
		return observation
	}
	if payload.SelectedClass == connectivity.SelectedReserve && payload.Reserve == 0 {
		payload.SelectedClass = connectivity.SelectedPrimary
	}
	switch {
	case payload.Reachable == 0:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonProbeFailed
	case payload.Reachable < payload.Configured:
		observation.Lifecycle = connectivity.LifecycleDegraded
		observation.Reason = connectivity.ReasonProbeFailed
	default:
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	}
	observation.Payload = connectivity.Payload{Relays: &payload}
	return observation
}
