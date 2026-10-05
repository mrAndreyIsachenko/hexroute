package tunnelexec

// Standing is what a cycle knows about this runtime's right and ability to go
// on holding the tunnel.
//
// It is read on every cycle, including the ones that decide nothing: a grant
// that lapsed or a tunnel that carries nothing are not noticed by deciding to
// rebuild, and a runtime that only looked when it wanted to act could hold a
// tunnel it had no right to for as long as it had no reason to touch it.
type Standing struct {
	// Owns is whether this runtime holds the tunnel. Everything below is about
	// a tunnel it holds; a runtime that holds none gives none back.
	Owns bool
	// RateAllows is whether the bound this runtime keeps on itself still lets a
	// rebuild through. Reaching it is the loop the bound exists for.
	RateAllows bool
	// GrantActive is whether the generation granting `tunnel_ownership` is
	// still in force. A runtime that kept acting after it lapsed would make the
	// expiry decorative.
	GrantActive bool
	// PayloadFailures is how many consecutive complete cycles the payload path
	// has failed while the outer path was reachable. A payload that fails with
	// the outer path down says nothing about the tunnel.
	PayloadFailures uint32
	// PayloadThreshold is how many of those end ownership. The runtime this one
	// reproduces judges its own payload only with the outer path up, and acts
	// on the third failure at a sixty-second interval.
	PayloadThreshold uint32
}

// Handback is why this runtime is giving the tunnel up, or empty while it is
// not.
type Handback string

const (
	HandbackRate        Handback = "rate_bound"
	HandbackGrantLapsed Handback = "grant_lapsed"
	HandbackDeadTunnel  Handback = "tunnel_carries_nothing"
)

// Handback answers whether this runtime should give the tunnel back, and why.
//
// The order is the order of certainty. A lapsed grant is a fact about this
// runtime's right and is true whatever the network is doing; the rate bound is a
// fact about its own behaviour; a tunnel that carries nothing is a reading of a
// path that may yet come back. A record naming the last where the first holds
// would say the network failed when what failed was the authority to touch it.
func (standing Standing) Handback() Handback {
	if !standing.Owns {
		return ""
	}
	switch {
	case !standing.GrantActive:
		return HandbackGrantLapsed
	case !standing.RateAllows:
		return HandbackRate
	case standing.PayloadThreshold > 0 && standing.PayloadFailures >= standing.PayloadThreshold:
		return HandbackDeadTunnel
	default:
		return ""
	}
}
