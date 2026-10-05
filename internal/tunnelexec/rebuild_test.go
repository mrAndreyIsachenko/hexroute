package tunnelexec

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

type fakeTunnel struct {
	stopped      []int
	stopErr      error
	started      int
	startErr     error
	ifaceAfter   int // how many calls before the interface is up
	ifaceCalls   int
	ifaceName    string
	interfaceErr error
}

func (tunnel *fakeTunnel) Stop(_ context.Context, pid int) error {
	if tunnel.stopErr != nil {
		return tunnel.stopErr
	}
	tunnel.stopped = append(tunnel.stopped, pid)
	return nil
}

func (tunnel *fakeTunnel) Start(context.Context) (int, error) {
	if tunnel.startErr != nil {
		return 0, tunnel.startErr
	}
	tunnel.started++
	return 4242, nil
}

func (tunnel *fakeTunnel) Interface(context.Context) (string, error) {
	tunnel.ifaceCalls++
	if tunnel.interfaceErr != nil {
		return "", tunnel.interfaceErr
	}
	if tunnel.ifaceCalls <= tunnel.ifaceAfter {
		return "", ErrNoInterface
	}
	return tunnel.ifaceName, nil
}

type fakeRoutes struct {
	restored []string
	failOn   netip.Addr
}

func (routes *fakeRoutes) Restore(_ context.Context, destination netip.Addr, iface string) error {
	if routes.failOn.IsValid() && destination == routes.failOn {
		return errors.New("the route could not be created")
	}
	routes.restored = append(routes.restored, destination.String()+"@"+iface)
	return nil
}

type fakePayload struct {
	passAfter int
	calls     int
	err       error
}

func (payload *fakePayload) Traversed(context.Context) (bool, error) {
	payload.calls++
	if payload.err != nil {
		return false, payload.err
	}
	return payload.calls > payload.passAfter, nil
}

// clock advances only when the rebuild sleeps, so a test spends no real time and
// the bound is exercised exactly.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func (c *clock) sleep(_ context.Context, duration time.Duration) error {
	c.at = c.at.Add(duration)
	return nil
}

func rebuilderFor(tunnel Tunnel, routes Routes, payload Payload) (Rebuilder, *clock) {
	c := &clock{at: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	return Rebuilder{
		Tunnel: tunnel, Routes: routes, Payload: payload,
		Bound: 30 * time.Second, Poll: time.Second,
		Now: c.now, Sleep: c.sleep,
	}, c
}

func addresses(values ...string) []netip.Addr {
	var parsed []netip.Addr
	for _, value := range values {
		parsed = append(parsed, netip.MustParseAddr(value))
	}
	return parsed
}

// A rebuild stops the process, starts one, and puts the routes back on the
// interface the new tunnel came up on — which is not the one it left.
func TestARebuildPutsTheRoutesOnTheNewInterface(t *testing.T) {
	tunnel := &fakeTunnel{ifaceAfter: 2, ifaceName: "utun16"}
	routes := &fakeRoutes{}
	rebuilder, _ := rebuilderFor(tunnel, routes, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{
		PID: 77, Interface: "utun15", Routes: addresses("192.0.2.10", "192.0.2.11"),
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !outcome.Done || outcome.Reason != "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(tunnel.stopped) != 1 || tunnel.stopped[0] != 77 || tunnel.started != 1 {
		t.Fatalf("stopped %v, started %d", tunnel.stopped, tunnel.started)
	}
	if outcome.Interface != "utun16" {
		t.Fatalf("interface = %q", outcome.Interface)
	}
	want := []string{"192.0.2.10@utun16", "192.0.2.11@utun16"}
	if len(routes.restored) != len(want) {
		t.Fatalf("restored %v, want %v", routes.restored, want)
	}
	for index, entry := range want {
		if routes.restored[index] != entry {
			t.Fatalf("restored %v, want %v", routes.restored, want)
		}
	}
	if len(outcome.Restored) != 2 {
		t.Fatalf("outcome restored %v", outcome.Restored)
	}
}

// Only what was handed in is restored. The runtime does not consult a plan of
// its own, and nothing that pointed elsewhere is touched.
func TestARebuildRestoresNothingItWasNotGiven(t *testing.T) {
	routes := &fakeRoutes{}
	rebuilder, _ := rebuilderFor(&fakeTunnel{ifaceName: "utun16"}, routes, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !outcome.Done {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(routes.restored) != 0 {
		t.Fatalf("a rebuild with no routes to put back created %v", routes.restored)
	}
}

// A route that cannot be created ends the rebuild as failed, and the tunnel is
// left running: a machine with a tunnel and one route missing is better than a
// machine with neither.
func TestARouteThatCannotBeRestoredFailsTheRebuild(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun16"}
	routes := &fakeRoutes{failOn: netip.MustParseAddr("192.0.2.11")}
	rebuilder, _ := rebuilderFor(tunnel, routes, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{
		PID: 77, Interface: "utun15", Routes: addresses("192.0.2.10", "192.0.2.11", "192.0.2.12"),
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonRouteNotRestore {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.StartedAt == 0 {
		t.Fatal("the rebuild reported no started process, so nothing says the tunnel is running")
	}
	if len(routes.restored) != 1 {
		t.Fatalf("restored %v, want to have stopped at the failure", routes.restored)
	}
}

// A rebuild is done when traffic passes, not when a process exists.
func TestARebuildIsDoneOnlyWhenTrafficPasses(t *testing.T) {
	// The payload answers on the third ask, inside the bound.
	rebuilder, _ := rebuilderFor(&fakeTunnel{ifaceName: "utun16"}, &fakeRoutes{}, &fakePayload{passAfter: 2})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !outcome.Done {
		t.Fatalf("outcome = %+v", outcome)
	}

	// A payload that never passes fails the rebuild inside the bound rather
	// than waiting for a cycle that never comes.
	rebuilder, clock := rebuilderFor(&fakeTunnel{ifaceName: "utun16"}, &fakeRoutes{},
		&fakePayload{passAfter: 1_000_000})
	began := clock.at
	outcome, err = rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonPayloadNotPass {
		t.Fatalf("outcome = %+v", outcome)
	}
	if waited := clock.at.Sub(began); waited > rebuilder.Bound+rebuilder.Poll {
		t.Fatalf("the rebuild waited %s past its bound of %s", waited, rebuilder.Bound)
	}
	// A probe that errors is not an answer: it is waited out, not taken as a
	// pass.
	rebuilder, _ = rebuilderFor(&fakeTunnel{ifaceName: "utun16"}, &fakeRoutes{},
		&fakePayload{err: errors.New("the probe could not run")})
	outcome, err = rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonPayloadNotPass {
		t.Fatalf("a probe that errored was read as %+v", outcome)
	}
}

// An interface that never comes up fails the rebuild, and nothing is restored on
// a name that is not there.
func TestAnInterfaceThatNeverComesUpFailsTheRebuild(t *testing.T) {
	routes := &fakeRoutes{}
	rebuilder, _ := rebuilderFor(&fakeTunnel{ifaceAfter: 1_000_000}, routes, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{
		PID: 77, Interface: "utun15", Routes: addresses("192.0.2.10"),
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonInterfaceAbsent {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(routes.restored) != 0 {
		t.Fatalf("routes were created without an interface: %v", routes.restored)
	}
}

// A process that cannot be stopped is not replaced: starting a second tunnel on
// one address is the thing the handover was built to prevent.
func TestAProcessThatCannotBeStoppedIsNotReplaced(t *testing.T) {
	tunnel := &fakeTunnel{stopErr: errors.New("the process would not end"), ifaceName: "utun16"}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonStopFailed {
		t.Fatalf("outcome = %+v", outcome)
	}
	if tunnel.started != 0 {
		t.Fatal("a tunnel was started beside one that would not stop")
	}
}

// A start that fails is reported as one, with nothing running.
func TestAStartThatFailsIsReported(t *testing.T) {
	tunnel := &fakeTunnel{startErr: errors.New("the version did not verify")}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, &fakePayload{})
	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 77, Interface: "utun15"})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonStartFailed || outcome.StoppedAt != 77 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// A rebuilder missing any of its parts performs nothing rather than half of one.
func TestAnIncompleteRebuilderPerformsNothing(t *testing.T) {
	if _, err := (Rebuilder{}).Rebuild(context.Background(), Current{PID: 1}); !errors.Is(err, ErrInvalidRebuild) {
		t.Fatalf("an empty rebuilder gave %v", err)
	}
	tunnel := &fakeTunnel{ifaceName: "utun16"}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, &fakePayload{})
	rebuilder.Bound = 0
	if _, err := rebuilder.Rebuild(context.Background(), Current{PID: 1}); !errors.Is(err, ErrInvalidRebuild) {
		t.Fatalf("a rebuilder with no bound gave %v", err)
	}
	if len(tunnel.stopped) != 0 {
		t.Fatal("an invalid rebuilder stopped a process")
	}
}

// fakeLink is the outer path under the test's control.
type fakeLink struct {
	readyAfter int
	calls      int
	err        error
}

func (link *fakeLink) Ready(context.Context) (bool, error) {
	link.calls++
	if link.err != nil {
		return false, link.err
	}
	return link.calls > link.readyAfter, nil
}

// A rebuild does not blame the tunnel for a network that is not there.
//
// Measured 2026-09-27: a wake of eleven minutes was rebuilt in two seconds and
// seven routes were restored, and the thirty seconds the rebuild then had for
// its proof ran out while the machine was still finding its network. Traffic
// passed three minutes later. The record said the payload did not pass, which
// sends a reader to the tunnel for something the tunnel did not do.
func TestAnAbsentOuterPathIsNotThePayloadsFailure(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun9"}
	payload := &fakePayload{passAfter: 1000}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, payload)
	rebuilder.Link = &fakeLink{readyAfter: 1000}

	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 7})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonOuterPathAbsent {
		t.Fatalf("outcome = %+v, want the outer path named", outcome)
	}
	// The tunnel was still replaced and its routes put back: what the rebuild
	// could do it did, and only the proof was out of reach.
	if outcome.StartedAt == 0 {
		t.Fatalf("no tunnel was started: %+v", outcome)
	}
}

// With a path that is there, a payload that does not pass is still the payload's
// failure, on the same bound as before.
func TestWithAnOuterPathThePayloadIsStillJudged(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun9"}
	payload := &fakePayload{passAfter: 1000}
	rebuilder, clock := rebuilderFor(tunnel, &fakeRoutes{}, payload)
	rebuilder.Link = &fakeLink{}
	began := clock.at

	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 7})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Done || outcome.Reason != ReasonPayloadNotPass {
		t.Fatalf("outcome = %+v, want the payload named", outcome)
	}
	// Every poll but the last is slept through, so the wall clock lands one
	// poll short of the bound the payload was judged over.
	if spent := clock.at.Sub(began); spent < rebuilder.Bound-rebuilder.Poll {
		t.Fatalf("the payload was given %s of its %s", spent, rebuilder.Bound)
	}
}

// A path that comes back inside the bound leaves the payload judged on its
// merits, and the waiting without a path costs the tunnel nothing.
//
// The wait itself is no longer than it was: the wall clock ends it at the bound
// either way. What the absent path changes is what the record says.
func TestAPathThatComesBackLeavesThePayloadJudged(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun9"}
	payload := &fakePayload{passAfter: 6}
	rebuilder, clock := rebuilderFor(tunnel, &fakeRoutes{}, payload)
	rebuilder.Bound = 10 * time.Second
	rebuilder.Link = &fakeLink{readyAfter: 5}
	began := clock.at

	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 7})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !outcome.Done || outcome.Reason != "" {
		t.Fatalf("outcome = %+v, want a rebuild done on traffic", outcome)
	}
	// Five polls were spent without a path and one with: the bound is nowhere
	// near reached, and none of the waiting counted against the tunnel.
	if spent := clock.at.Sub(began); spent > rebuilder.Bound {
		t.Fatalf("the rebuild took %s, past its bound of %s", spent, rebuilder.Bound)
	}
}

// A reading nobody could take is not evidence that the path is there.
func TestALinkThatCannotBeReadIsNotAPath(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun9"}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, &fakePayload{passAfter: 1000})
	rebuilder.Link = &fakeLink{err: errors.New("the probe could not be taken")}

	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 7})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Reason != ReasonOuterPathAbsent {
		t.Fatalf("outcome = %+v, want the outer path named", outcome)
	}
}

// A rebuilder with no way to read the outer path waits exactly as it did before
// this existed, and names the payload.
func TestWithoutALinkNothingChanges(t *testing.T) {
	tunnel := &fakeTunnel{ifaceName: "utun9"}
	rebuilder, _ := rebuilderFor(tunnel, &fakeRoutes{}, &fakePayload{passAfter: 1000})

	outcome, err := rebuilder.Rebuild(context.Background(), Current{PID: 7})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if outcome.Reason != ReasonPayloadNotPass {
		t.Fatalf("outcome = %+v, want the payload named", outcome)
	}
}
