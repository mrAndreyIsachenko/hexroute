// Package tunnelplan decides what a tunnel owner would do.
//
// It decides and does not act. While another runtime owns the tunnel this is
// the whole of it: the decision is recorded beside what that runtime actually
// did, and the two are compared. The causes here are the six the production
// supervisor acts on, taken from sixty-one days of its own log rather than
// derived from its source — nineteen carrier changes, eleven wake gaps, two
// payload failures, and two that did not occur at all.
//
// Nothing in here reads a clock, a file or a process. A rule that will later
// decide whether a machine's network is rebuilt should be decidable in a test,
// and everything it needs is passed in.
package tunnelplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

type Cause string

const (
	// CauseProcessGone is the only one of the six that stands between this
	// machine and no network at all. It has not occurred in sixty-one days.
	CauseProcessGone Cause = "process_gone"
	// CauseWakeGap is the interval since the previous cycle exceeding what a
	// sleep would explain. Eleven in sixty-one days.
	CauseWakeGap Cause = "wake_gap"
	// CauseCarrierChanged is the set of interfaces carrying the configured
	// destinations differing from the previous cycle's. Nineteen in sixty-one
	// days, the most common of them all.
	CauseCarrierChanged Cause = "carrier_changed"
	// CauseLinkReturned is connectivity coming back after being absent. It has
	// not occurred in sixty-one days and is how a laptop returns from a dead
	// link.
	// It is observed and recorded and no longer named: the runtime this rule
	// reproduces does not rebuild on a returned link, by recorded decision.
	CauseLinkReturned Cause = "link_returned"
	// CausePayloadFailed is the tunnel being up and carrying nothing. Two in
	// sixty-one days, and the only cause that distinguishes a path that works
	// from one that answers.
	// It is observed and recorded and no longer named: the runtime this rule
	// reproduces rebuilds on the payload path only after a failover this runtime
	// does not perform.
	CausePayloadFailed Cause = "payload_failed"
	// CauseRoutesDrifted is the observed routes differing from the planned
	// ones. The supervisor reapplies them every tick.
	// It is observed and recorded and no longer named: routes stay with the
	// runtime that owns them.
	CauseRoutesDrifted Cause = "routes_drifted"
)

type Action string

const (
	ActionNone          Action = "none"
	ActionRebuildTunnel Action = "rebuild_tunnel"
	// ActionReapplyRoutes is no longer produced. Records written before this
	// change carry it, and the record's vocabulary still reads them.
	ActionReapplyRoutes Action = "reapply_routes"
)

var (
	ErrInvalidPolicy = errors.New("invalid tunnel supervision policy")
	ErrInvalidInput  = errors.New("invalid tunnel supervision input")
)

// Signature identifies which interface carries each configured destination.
//
// It is the supervisor's own comparison, expressed in what this runtime already
// observes, so that the two can disagree about the facts and not about the
// question. It is a string because it is compared for equality and carried
// across a restart, and both are simpler on a string than on a map.
type Signature string

// Entries is how many destinations the signature covers.
//
// It travels into a decision record where the signature itself must not: a
// count says whether the runtime is watching what it was configured to watch,
// and a signature of twenty entries where sixteen were expected is how a defect
// showed itself once already.
func (signature Signature) Entries() int {
	if signature == "" {
		return 0
	}
	return strings.Count(string(signature), " ") + 1
}

// Digest identifies a signature without disclosing it.
//
// A reader needs to know whether two cycles saw the same carrier. They do not
// need the addresses, and this repository does not let those out: the relay
// mapper beside this one records how many endpoints answered and says in its own
// comment that which endpoint is which never leaves it.
//
// Truncated because it is compared and never resolved. A whole digest of a small
// input space can be walked back by anyone who guesses the space; twelve hex
// characters distinguish the carriers a machine sees without being a handle on
// them.
func (signature Signature) Digest() string {
	if signature == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:])[:12]
}

type CarriedDestination struct {
	Destination string
	Interface   string
}

// NewSignature builds a signature that does not depend on the order the routes
// were observed in.
func NewSignature(carried []CarriedDestination) Signature {
	parts := make([]string, 0, len(carried))
	for _, one := range carried {
		parts = append(parts, one.Destination+"="+one.Interface)
	}
	sort.Strings(parts)
	return Signature(strings.Join(parts, " "))
}

type Policy struct {
	// Interval is how long the runtime waits between cycles. Every cycle is at
	// least an interval after the last, so a threshold at or below it would name
	// a wake gap on every cycle.
	Interval time.Duration
	// WakeThreshold is the wall time since the previous cycle at which the gap
	// is a wake, compared inclusively, as the runtime this rule reproduces
	// compares the wall time between two of its ticks.
	WakeThreshold time.Duration
	// PayloadFailures is how many consecutive cycles the payload path must
	// fail before it is a cause. One failure is a network; several are a path.
	PayloadFailures uint32
	// LinkFailures is how many consecutive cycles the outer path must be
	// unreachable before the link counts as gone.
	//
	// The same reasoning as PayloadFailures, and it was missing. Measured on
	// this machine over seven days: the outer probe failed on 25 of 3,117
	// cycles and never twice in a row, so without a threshold the link was
	// declared gone 25 times and returned 25 times. Six of those returns
	// landed in one half hour and each would have rebuilt a tunnel that was
	// working. The runtime that owns the tunnel declares the outer path down
	// on the second consecutive failure and up on the first success; this is
	// that shape.
	LinkFailures uint32
}

// Observed is what one cycle saw.
type Observed struct {
	// Complete says whether the cycle got far enough to see everything here.
	//
	// A cycle that stopped early has no routes and no endpoint readings, and an
	// empty carrier signature is not a changed one. Deciding a carrier change
	// out of an incomplete cycle would rebuild the tunnel every time an
	// observation failed — which is exactly when the tunnel is already in
	// trouble and the rebuild would be decided for the wrong reason.
	Complete bool
	// ProcessObserved says the cycle looked at the tunnel process at all. A
	// cycle that could not look has not seen a loss: reading "not running" out
	// of an observation never taken named a loss on every cycle a machine spent
	// in dark wake, measured on the night of 2026-09-18.
	ProcessObserved bool
	ProcessRunning  bool
	// ProcessReplaced says the tunnel process the previous cycle of this
	// runtime saw is no longer the one running: it went, and the owner started
	// another before this cycle looked. The owner watches its own child and
	// restarts it within seconds, faster than a cycle, so a rule asking only
	// whether a tunnel runs sees one both times. Measured 2026-09-17: stopped at
	// 11:38:09Z, noticed by the owner at 11:38:28Z, running again at 11:38:32Z.
	ProcessReplaced bool
	// Slept is how long the steady clock says the machine was asleep between
	// this cycle and the one before it. It is a ground and decides nothing: the
	// steady clock does not stop for every sleep. Across idle sleeps of 136, 997
	// and 394 seconds on 2026-09-14 it measured none.
	Slept time.Duration
	// TickGap is the wall time since this process's previous cycle, and zero on
	// its first. It is what the wake cause compares, as the runtime this rule
	// reproduces compares the wall time between two of its ticks, and a slow
	// cycle makes a gap as a sleep does in both.
	TickGap       time.Duration
	Carrier       Signature
	LinkPresent   bool
	PayloadOK     bool
	RoutesDrifted bool
}

// State is what a cycle leaves for the next one.
//
// Three of the six causes are comparisons against the previous cycle, so this
// is the memory they need. Known says whether there was a previous cycle at
// all: a runtime that has just started has no signature to differ from, and
// manufacturing a carrier change out of that would rebuild the tunnel on every
// restart.
type State struct {
	Known   bool      `json:"known"`
	Carrier Signature `json:"carrier"`
	// LinkPresent is what the rule believes about the outer path, which is not
	// the same as what the last cycle saw: a single failed probe does not
	// change the belief.
	LinkPresent     bool   `json:"link_present"`
	PayloadFailures uint32 `json:"payload_failures"`
	// LinkFailures counts consecutive cycles that could not reach the outer
	// path. It is reset by any cycle that could.
	LinkFailures uint32 `json:"link_failures"`
}

type Plan struct {
	Action Action
	// Causes holds every cause that held, in a stable order. The runtime this
	// is compared against reports one reason, so recording only the first would
	// make an honest disagreement look like a wrong decision.
	Causes []Cause
	// Grounds is what the causes were reached from.
	//
	// A decision naming a cause and nothing behind it reads as convincingly
	// when it is wrong as when it is right. On 2026-09-12 this rule recorded six
	// rebuilds for a returned link in half an hour and the link had not
	// returned; working that out took a second store laid beside the decisions
	// and matched on time, and the comparison these decisions exist for is
	// against a runtime that writes no such store.
	Grounds Grounds
}

// Grounds is the observation behind each cause, and the cycle's own honesty
// about what it managed to see.
//
// Three of the six causes are comparisons an incomplete cycle does not make, so
// without Complete a reader cannot tell a cause that did not hold from one that
// was never asked.
type Grounds struct {
	Complete        bool
	ProcessObserved bool
	ProcessRunning  bool
	ProcessReplaced bool
	Slept           time.Duration
	// TickGap is what the wake cause was compared on: the wall time since the
	// previous cycle.
	TickGap         time.Duration
	Carrier         Signature
	CarrierEntries  int
	LinkPresent     bool
	LinkFailures    uint32
	PayloadOK       bool
	PayloadFailures uint32
	RoutesDrifted   bool
}

// Decide reaches one decision and the state the next cycle needs.
//
// The causes are evaluated in a fixed order so that a reader comparing two
// records is comparing decisions rather than iteration order. Every cause that
// held is named; the action is the strongest of them, because rebuilding the
// tunnel reapplies its routes and reporting both would suggest two acts.
func Decide(policy Policy, previous State, observed Observed) (Plan, State, error) {
	// A threshold at or below one interval would name a wake gap on every cycle,
	// because every cycle is at least an interval after the one before it.
	if policy.WakeThreshold <= 0 || policy.PayloadFailures == 0 ||
		policy.LinkFailures == 0 || policy.Interval <= 0 ||
		policy.WakeThreshold <= policy.Interval {
		return Plan{}, State{}, ErrInvalidPolicy
	}
	if observed.Slept < 0 || observed.TickGap < 0 {
		return Plan{}, State{}, ErrInvalidInput
	}

	next := State{
		Known:           true,
		Carrier:         observed.Carrier,
		LinkPresent:     previous.LinkPresent,
		PayloadFailures: previous.PayloadFailures,
		LinkFailures:    previous.LinkFailures,
	}
	if !observed.Complete {
		// What it could not see, it does not overwrite. Carrying an empty
		// signature forward would make the next complete cycle read a carrier
		// change out of this one's blindness. A cycle that never reached the
		// probes did not fail them either, so the count stands.
		next.Carrier = previous.Carrier
		if !previous.Known {
			next.Known = false
		}
	} else if observed.LinkPresent {
		next.LinkPresent = true
		next.LinkFailures = 0
	} else {
		// A start knows nothing, and this rule exists to notice a change. A
		// start that assumed the link absent would call the first successful
		// probe a return and rebuild the tunnel for it.
		alreadyAbsent := previous.Known && !previous.LinkPresent
		next.LinkFailures = previous.LinkFailures + 1
		next.LinkPresent = !alreadyAbsent &&
			next.LinkFailures < policy.LinkFailures
	}
	if observed.PayloadOK {
		next.PayloadFailures = 0
	} else {
		next.PayloadFailures++
	}

	payloadFailuresBehind := next.PayloadFailures

	// Three causes act, in the definitions of the runtime this rule reproduces.
	// A returned link, a failed payload path and drifted routes are still
	// observed above and still recorded below; they no longer decide anything.
	// Measured before this change: the rule that acted on them decided 125
	// rebuilds in a window where that runtime made 2.
	causes := make([]Cause, 0, 3)
	// Gone now, or gone and replaced since the last cycle: either is the loss
	// that runtime restarts on, because it watches its own child rather than
	// asking whether some tunnel runs.
	if observed.ProcessObserved && (!observed.ProcessRunning || observed.ProcessReplaced) {
		causes = append(causes, CauseProcessGone)
	}
	// The tick gap, inclusive, on the wall clock, as that runtime compares it.
	// It was the interval plus the sleep the steady clock measured, and that
	// clock does not stop for every sleep this machine takes: across the idle
	// sleeps of 2026-09-14 it measured none, and the rule decided no wake.
	if observed.TickGap >= policy.WakeThreshold {
		causes = append(causes, CauseWakeGap)
	}
	// A carrier change compares against a previous cycle. Without one there is
	// nothing to differ from, and inventing a difference would rebuild the tunnel
	// on every restart.
	if observed.Complete && previous.Known && observed.Carrier != previous.Carrier {
		causes = append(causes, CauseCarrierChanged)
	}

	action := ActionNone
	if len(causes) > 0 {
		action = ActionRebuildTunnel
	}
	// Built from what the decision above used, rather than gathered again. A
	// second gathering could drift from the first, and a record that says
	// something the decision did not is worse than one that says nothing.
	grounds := Grounds{
		Complete:        observed.Complete,
		ProcessObserved: observed.ProcessObserved,
		ProcessRunning:  observed.ProcessRunning,
		ProcessReplaced: observed.ProcessReplaced,
		Slept:           observed.Slept,
		TickGap:         observed.TickGap,
		LinkPresent:     next.LinkPresent,
		LinkFailures:    next.LinkFailures,
		PayloadOK:       observed.PayloadOK,
		// How many consecutive cycles the payload path has failed. It is a
		// ground, not a cause, and the count stands until the path answers.
		PayloadFailures: payloadFailuresBehind,
		RoutesDrifted:   observed.RoutesDrifted,
	}
	if observed.Complete {
		// A cycle that stopped early saw no carrier, and an empty signature is
		// not an observation of one.
		grounds.Carrier = observed.Carrier
		grounds.CarrierEntries = observed.Carrier.Entries()
	}
	return Plan{Action: action, Causes: causes, Grounds: grounds}, next, nil
}
