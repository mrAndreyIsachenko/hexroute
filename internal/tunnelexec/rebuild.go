// Package tunnelexec performs the rebuild the decision rule decides on.
//
// A rebuild is three things that must happen together: the tunnel process is
// replaced, the host routes that pointed at the old tunnel interface are put
// back on the new one, and traffic is proved to pass through it. Doing the first
// alone leaves a machine whose routes point at an interface that no longer
// exists — the interface is not the one it left, measured across the previous
// owner's own rebuilds, where one address was carried by seven different tunnel
// interfaces.
//
// Nothing here decides. It is handed what to rebuild and reports what happened,
// so that the rule, the gates that allow an act and the act itself stay separate
// and separately testable.
package tunnelexec

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// Tunnel is the process this runtime runs and the interface it brings up.
type Tunnel interface {
	// Stop ends the process this runtime started. It returns when the process
	// is gone or when it cannot be ended.
	Stop(ctx context.Context, pid int) error
	// Start runs the tunnel from the signed configuration version and reports
	// the process it started.
	Start(ctx context.Context) (int, error)
	// Interface is the tunnel interface, found by the address it carries rather
	// than by a name. It reports ErrNoInterface while none is up.
	Interface(ctx context.Context) (string, error)
}

// Routes puts a host route back on an interface.
type Routes interface {
	Restore(ctx context.Context, destination netip.Addr, iface string) error
}

// Payload answers whether traffic traversed the tunnel, rather than whether
// something accepted a socket.
// Link answers whether the path the tunnel runs over is there.
//
// It is here because a payload that does not pass while the outer path is down
// says nothing about the tunnel — the same reason a failure under those
// conditions does not count against ownership. Without it a rebuild after a
// wake blames the tunnel for a network that has not come back yet: measured
// 2026-09-27, a wake of eleven minutes was rebuilt inside two seconds, seven
// routes were restored, and the thirty seconds it then had for a proof ran out
// while the machine was still finding its network. Traffic passed three minutes
// later.
//
// A rebuilder without one behaves as before, which is what a runtime that
// cannot observe the outer path should do.
type Link interface {
	// Ready is whether the outer path is reachable. An error is not an answer
	// and is read as not ready, because a reading nobody could take is not
	// evidence that the path is there.
	Ready(ctx context.Context) (bool, error)
}

type Payload interface {
	Traversed(ctx context.Context) (bool, error)
}

var (
	// ErrNoInterface is a tunnel interface that is not up yet.
	ErrNoInterface = errors.New("no tunnel interface")
	// ErrInvalidRebuild is a rebuilder that cannot perform one.
	ErrInvalidRebuild = errors.New("invalid rebuild")
)

// Reason is why a rebuild did not finish, or empty when it did.
type Reason string

const (
	ReasonStopFailed      Reason = "stop_failed"
	ReasonStartFailed     Reason = "start_failed"
	ReasonInterfaceAbsent Reason = "interface_absent"
	ReasonRouteNotRestore Reason = "route_not_restored"
	ReasonPayloadNotPass  Reason = "payload_did_not_pass"
	// ReasonOuterPathAbsent is a proof that could not be taken because the path
	// the tunnel runs over was not there. It is not the tunnel's failure, and
	// naming the payload for it would send a reader looking at the tunnel for a
	// machine that had no network.
	ReasonOuterPathAbsent Reason = "outer_path_absent"
)

// Current is what the cycle saw before the rebuild: the process to replace, the
// interface it holds, and the destinations whose routes point at that interface.
type Current struct {
	PID       int
	Interface string
	Routes    []netip.Addr
}

// Outcome is what the rebuild did. A rebuild that did not finish still reports
// what it got through, because a tunnel stopped and not started is a different
// machine from one that was never touched.
type Outcome struct {
	Done      bool
	Reason    Reason
	StoppedAt int
	StartedAt int
	Interface string
	Restored  []netip.Addr
	Elapsed   time.Duration
}

// Rebuilder performs rebuilds.
type Rebuilder struct {
	Tunnel  Tunnel
	Routes  Routes
	Payload Payload
	// Link is the outer path, and may be absent: a rebuilder without one waits
	// out its bound exactly as it did before this existed.
	Link Link
	// Bound is how long the interface and the payload together may take. The
	// owning runtime reached its own startup probe 17 seconds after stopping,
	// measured 2026-09-23, and a cycle that rebuilt has already changed the
	// machine more than waiting does.
	Bound time.Duration
	Poll  time.Duration
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
}

func (rebuilder Rebuilder) valid() bool {
	return rebuilder.Tunnel != nil && rebuilder.Routes != nil && rebuilder.Payload != nil &&
		rebuilder.Bound > 0 && rebuilder.Poll > 0 && rebuilder.Now != nil && rebuilder.Sleep != nil
}

// Rebuild replaces the tunnel and puts the routes back.
//
// The order is the one a machine can survive being interrupted in: the process
// is stopped first, because two tunnels on one address is worse than none, and
// the routes are restored only once the new interface exists, because a route to
// an interface that is not there cannot be created and a route to the old one
// would point at nothing.
func (rebuilder Rebuilder) Rebuild(ctx context.Context, current Current) (Outcome, error) {
	if !rebuilder.valid() {
		return Outcome{}, ErrInvalidRebuild
	}
	began := rebuilder.Now()
	outcome := Outcome{}
	if current.PID > 0 {
		if err := rebuilder.Tunnel.Stop(ctx, current.PID); err != nil {
			outcome.Reason = ReasonStopFailed
			outcome.Elapsed = rebuilder.Now().Sub(began)
			return outcome, nil
		}
		outcome.StoppedAt = current.PID
	}
	started, err := rebuilder.Tunnel.Start(ctx)
	if err != nil {
		outcome.Reason = ReasonStartFailed
		outcome.Elapsed = rebuilder.Now().Sub(began)
		return outcome, nil
	}
	outcome.StartedAt = started

	iface, err := rebuilder.awaitInterface(ctx, began)
	if err != nil {
		outcome.Reason = ReasonInterfaceAbsent
		outcome.Elapsed = rebuilder.Now().Sub(began)
		return outcome, nil
	}
	outcome.Interface = iface

	for _, destination := range current.Routes {
		if err := rebuilder.Routes.Restore(ctx, destination, iface); err != nil {
			outcome.Reason = ReasonRouteNotRestore
			outcome.Elapsed = rebuilder.Now().Sub(began)
			return outcome, nil
		}
		outcome.Restored = append(outcome.Restored, destination)
	}

	if passed, reason := rebuilder.awaitPayload(ctx, began); !passed {
		outcome.Reason = reason
		outcome.Elapsed = rebuilder.Now().Sub(began)
		return outcome, nil
	}
	outcome.Done = true
	outcome.Elapsed = rebuilder.Now().Sub(began)
	return outcome, nil
}

// awaitInterface waits for the tunnel to bring its interface up, inside what is
// left of the bound.
func (rebuilder Rebuilder) awaitInterface(ctx context.Context, began time.Time) (string, error) {
	for {
		iface, err := rebuilder.Tunnel.Interface(ctx)
		if err == nil && iface != "" {
			return iface, nil
		}
		if !rebuilder.within(began) {
			return "", ErrNoInterface
		}
		if err := rebuilder.Sleep(ctx, rebuilder.Poll); err != nil {
			return "", err
		}
	}
}

// awaitPayload waits for traffic to pass, inside what is left of the bound. A
// probe that errors is not an answer and is waited out like a probe that
// answered no.
//
// Time in which the outer path is not there is not spent from the bound. A
// payload cannot traverse a tunnel over a path that is absent, and counting
// that wait against the tunnel is blaming it for something it did not do —
// measured 2026-09-27, a rebuild after a wake of eleven minutes spent its whole
// bound while the machine was still finding its network, and traffic passed
// three minutes later.
//
// The wait is no longer than it was. The wall clock still ends it at the bound;
// what changes is what the record says when it does. A tunnel that spent the
// whole bound with a path and did not carry traffic is one thing, and a tunnel
// that never had a path to be judged over is another.
func (rebuilder Rebuilder) awaitPayload(
	ctx context.Context,
	began time.Time,
) (bool, Reason) {
	spent := time.Duration(0)
	ready := true
	for {
		if rebuilder.Link != nil {
			answered, err := rebuilder.Link.Ready(ctx)
			ready = err == nil && answered
		}
		traversed, err := rebuilder.Payload.Traversed(ctx)
		if err == nil && traversed {
			return true, ""
		}
		if ready {
			spent += rebuilder.Poll
		}
		if spent >= rebuilder.Bound {
			return false, ReasonPayloadNotPass
		}
		if !rebuilder.within(began) {
			// The wall clock ran out before the tunnel had its bound with a
			// path to be judged over. Naming the payload here would blame the
			// tunnel for a network that was not there.
			return false, ReasonOuterPathAbsent
		}
		if err := rebuilder.Sleep(ctx, rebuilder.Poll); err != nil {
			if ready {
				return false, ReasonPayloadNotPass
			}
			return false, ReasonOuterPathAbsent
		}
	}
}

func (rebuilder Rebuilder) within(began time.Time) bool {
	return rebuilder.Now().Sub(began) < rebuilder.Bound
}
