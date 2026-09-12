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
	CauseLinkReturned Cause = "link_returned"
	// CausePayloadFailed is the tunnel being up and carrying nothing. Two in
	// sixty-one days, and the only cause that distinguishes a path that works
	// from one that answers.
	CausePayloadFailed Cause = "payload_failed"
	// CauseRoutesDrifted is the observed routes differing from the planned
	// ones. The supervisor reapplies them every tick.
	CauseRoutesDrifted Cause = "routes_drifted"
)

type Action string

const (
	ActionNone          Action = "none"
	ActionRebuildTunnel Action = "rebuild_tunnel"
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
	// WakeThreshold is the interval since the previous cycle beyond which the
	// gap is a sleep rather than a slow cycle.
	WakeThreshold time.Duration
	// PayloadFailures is how many consecutive cycles the payload path must
	// fail before it is a cause. One failure is a network; several are a path.
	PayloadFailures uint32
}

// Observed is what one cycle saw.
type Observed struct {
	ProcessRunning bool
	// SincePrevious is the interval between this cycle and the one before it.
	SincePrevious time.Duration
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
	Known           bool      `json:"known"`
	Carrier         Signature `json:"carrier"`
	LinkPresent     bool      `json:"link_present"`
	PayloadFailures uint32    `json:"payload_failures"`
}

type Plan struct {
	Action Action
	// Causes holds every cause that held, in a stable order. The runtime this
	// is compared against reports one reason, so recording only the first would
	// make an honest disagreement look like a wrong decision.
	Causes []Cause
}

// Decide reaches one decision and the state the next cycle needs.
//
// The causes are evaluated in a fixed order so that a reader comparing two
// records is comparing decisions rather than iteration order. Every cause that
// held is named; the action is the strongest of them, because rebuilding the
// tunnel reapplies its routes and reporting both would suggest two acts.
func Decide(policy Policy, previous State, observed Observed) (Plan, State, error) {
	if policy.WakeThreshold <= 0 || policy.PayloadFailures == 0 {
		return Plan{}, State{}, ErrInvalidPolicy
	}
	if observed.SincePrevious < 0 {
		return Plan{}, State{}, ErrInvalidInput
	}

	next := State{
		Known:           true,
		Carrier:         observed.Carrier,
		LinkPresent:     observed.LinkPresent,
		PayloadFailures: previous.PayloadFailures,
	}
	if observed.PayloadOK {
		next.PayloadFailures = 0
	} else {
		next.PayloadFailures++
	}

	causes := make([]Cause, 0, 6)
	if !observed.ProcessRunning {
		causes = append(causes, CauseProcessGone)
	}
	// The interval is measured against the previous cycle, so a first cycle
	// has nothing to measure and SincePrevious is zero for it.
	if observed.SincePrevious > policy.WakeThreshold {
		causes = append(causes, CauseWakeGap)
	}
	// Both of these compare against a previous cycle. Without one there is
	// nothing to differ from, and inventing a difference would rebuild the
	// tunnel on every restart.
	if previous.Known && observed.Carrier != previous.Carrier {
		causes = append(causes, CauseCarrierChanged)
	}
	if previous.Known && !previous.LinkPresent && observed.LinkPresent {
		causes = append(causes, CauseLinkReturned)
	}
	if next.PayloadFailures >= policy.PayloadFailures {
		causes = append(causes, CausePayloadFailed)
		// The count is spent on the decision it caused. Leaving it standing
		// would make every later cycle repeat the cause until the path
		// recovered, which says the path failed many times rather than once.
		next.PayloadFailures = 0
	}
	if observed.RoutesDrifted {
		causes = append(causes, CauseRoutesDrifted)
	}

	action := ActionNone
	for _, cause := range causes {
		if cause == CauseRoutesDrifted {
			if action == ActionNone {
				action = ActionReapplyRoutes
			}
			continue
		}
		action = ActionRebuildTunnel
	}
	return Plan{Action: action, Causes: causes}, next, nil
}
