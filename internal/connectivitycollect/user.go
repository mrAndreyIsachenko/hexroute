package connectivitycollect

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

// The user mappers read only what the user daemon already observed about
// access and session presence. They never touch a PIN, a TOTP secret, a
// generated OTP or a Keychain reference — none of those appear in the
// observations they consume, and the payloads have no field to carry one.

// MapUserAccess describes whether user access is configured and established.
//
// The profile identity, its server, its organisation and its client address
// are all read by the observer and none of them travel: what leaves is whether
// a profile exists, whether it authenticated and whether it is carrying
// traffic.
func MapUserAccess(
	profile userobserve.ProfileObservation,
	service userobserve.ServiceObservation,
	err error,
) Observation {
	observation := Observation{Component: connectivity.ComponentUserAccess}
	payload := connectivity.UserAccessPayload{ProfileClass: connectivity.ProfileNone}
	if err != nil {
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{UserAccess: &payload}
		return observation
	}
	if !profile.Found {
		observation.Lifecycle = connectivity.LifecycleNotApplicable
		observation.Reason = connectivity.ReasonNotConfigured
		observation.Payload = connectivity.Payload{UserAccess: &payload}
		return observation
	}
	payload.ProfileClass = connectivity.ProfileConfigured
	switch {
	case profile.Connected():
		// Carrying traffic implies the session behind it was accepted.
		payload.Authenticated = true
		payload.Connected = true
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	case profile.Connecting || profile.State == userobserve.ProfileActive:
		payload.Authenticated = true
		observation.Lifecycle = connectivity.LifecycleDegraded
		observation.Reason = connectivity.ReasonProbeFailed
	case !service.Running:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonOwnerUnavailable
	default:
		observation.Lifecycle = connectivity.LifecycleFailed
		observation.Reason = connectivity.ReasonProbeFailed
	}
	observation.Payload = connectivity.Payload{UserAccess: &payload}
	return observation
}

// MapUserSession describes the console session that user access depends on.
//
// Hexroute does not observe how long a session has left to live, so this
// mapper reports presence and never claims expiring or expired. Saying
// "expiring" from a presence check would be inventing a measurement; the
// component simply carries less than its name allows until an owner can
// actually observe expiry.
func MapUserSession(session userobserve.SessionObservation, err error) Observation {
	observation := Observation{Component: connectivity.ComponentSessionExpiry}
	payload := connectivity.SessionExpiryPayload{ExpiryClass: connectivity.ExpiryNone}
	if err != nil {
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
		observation.Payload = connectivity.Payload{SessionExpiry: &payload}
		return observation
	}
	switch session.State {
	case userobserve.SessionActive:
		payload.ExpiryClass = connectivity.ExpiryValid
		payload.Sessions = 1
		observation.Lifecycle = connectivity.LifecycleReady
		observation.Reason = connectivity.ReasonProbeSucceeded
	case userobserve.SessionInactive:
		observation.Lifecycle = connectivity.LifecycleNotApplicable
		observation.Reason = connectivity.ReasonNotConfigured
	default:
		observation.Lifecycle = connectivity.LifecycleUnknown
		observation.Reason = connectivity.ReasonProbeFailed
	}
	observation.Payload = connectivity.Payload{SessionExpiry: &payload}
	return observation
}
