package tunnelexec

// Gates are what a cycle must hold before it may perform what it decided.
//
// They are read in the cycle that decided, and answered there: an authorization
// carried to a later cycle is an answer about a machine that has moved on. They
// are separate from the rebuild so that "may this act happen" and "what does the
// act do" cannot be tested through one another.
type Gates struct {
	// Owns is whether this runtime holds the tunnel. Without the claim the
	// process belongs to another runtime, and stopping it would be this one
	// ending another's job without having said so on disk.
	Owns bool
	// Suspended is whether the machine was in a dark wake or had its lid closed
	// when the cycle ran. Such a wake lasts seconds — 21 of them in the night of
	// 2026-09-20 — and a rebuild does not fit in one; the gap keeps accruing, so
	// the first waking cycle rebuilds instead.
	Suspended bool
	// Asked is whether the grant was put the question at all.
	//
	// A runtime whose control state has no generation yet cannot ask: the
	// question carries the state it was reached under, and an evaluator refuses
	// a malformed one before it looks at policy. Its first cycle is such a
	// runtime, and that is the cycle after every restart.
	Asked bool
	// Authorized is the grant's answer to this act, in this cycle. It means
	// nothing unless the question was asked.
	Authorized bool
	// RateAllows is whether the rebuild is inside the bounds this runtime keeps
	// on itself.
	RateAllows bool
}

// Block is which gate stopped an act, or empty when none did.
type Block string

const (
	BlockedNotOwned  Block = "not_owned"
	BlockedSuspended Block = "suspended"
	// BlockedUnasked is a cycle that could not put the question, which is not
	// the same as a grant that refused.
	//
	// Measured 2026-09-26T09:04:05Z: a daemon two seconds old saw its tunnel
	// gone, had both the reason and the grant, and recorded `unauthorized`. It
	// refused itself while naming the grant, and the machine went about a
	// minute with no tunnel because the previous owner was standing down under
	// this runtime's claim. It is the malformed question of 2026-09-14 one
	// layer down: the decision record learned to say nothing could be asked and
	// the gate did not.
	BlockedUnasked      Block = "authorization_unasked"
	BlockedUnauthorized Block = "unauthorized"
	BlockedRate         Block = "rate_bound"
)

// Blocked names the gate that stopped the act, or is empty if it may proceed.
//
// The order is from the least to the most specific claim about this runtime: a
// runtime that does not own the tunnel is not refused for being suspended, and a
// suspended machine is not reported as unauthorized. A record that named the
// wrong gate would send a reader looking at the policy for a machine that was
// merely asleep.
//
// An unasked question sits before an unauthorized answer for the same reason.
// "Nothing could be asked" is about this runtime having no state to ask with;
// "the grant refused" is a claim about what the grant says, and a runtime that
// never asked is in no position to make it.
func (gates Gates) Blocked() Block {
	switch {
	case !gates.Owns:
		return BlockedNotOwned
	case gates.Suspended:
		return BlockedSuspended
	case !gates.Asked:
		return BlockedUnasked
	case !gates.Authorized:
		return BlockedUnauthorized
	case !gates.RateAllows:
		return BlockedRate
	default:
		return ""
	}
}

// Allowed is whether every gate is open.
func (gates Gates) Allowed() bool { return gates.Blocked() == "" }
