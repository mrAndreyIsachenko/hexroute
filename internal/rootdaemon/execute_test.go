package rootdaemon

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelexec"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

type fakeClaims struct {
	held bool
	err  error
}

func (claims fakeClaims) Held() (tunnelclaim.Claim, bool, error) {
	return tunnelclaim.Claim{}, claims.held, claims.err
}

// flippingClaims is a claim a release can take away, which is what a release
// does and what the record about it has to read.
type flippingClaims struct {
	held *bool
	err  error
}

func (claims flippingClaims) Held() (tunnelclaim.Claim, bool, error) {
	return tunnelclaim.Claim{}, *claims.held, claims.err
}

func openRate(t *testing.T) tunnelexec.Rate {
	t.Helper()
	return tunnelexec.Rate{
		Path: filepath.Join(t.TempDir(), "tunnel-rebuilds.json"),
		Now:  time.Now,
	}
}

// Every gate is read where the decision was made, and the record names the one
// that stopped the act.
func TestTheCycleRecordsWhichGateStoppedTheAct(t *testing.T) {
	allowed := &event.TunnelAuthorization{Allowed: true}
	refused := &event.TunnelAuthorization{Allowed: false, Reason: "selector_mismatch"}
	for _, item := range []struct {
		name          string
		claims        fakeClaims
		suspended     bool
		authorization *event.TunnelAuthorization
		want          tunnelexec.Block
	}{
		{"no claim", fakeClaims{}, false, allowed, tunnelexec.BlockedNotOwned},
		{"a claim that cannot be read", fakeClaims{err: tunnelclaim.ErrInvalidClaim}, false, allowed,
			tunnelexec.BlockedNotOwned},
		{"suspended", fakeClaims{held: true}, true, allowed, tunnelexec.BlockedSuspended},
		// Nothing asked and a refusal are different states, and the record says
		// which. A daemon two seconds old is the first of them.
		{"nothing could be asked", fakeClaims{held: true}, false, nil, tunnelexec.BlockedUnasked},
		{"unauthorized", fakeClaims{held: true}, false, refused, tunnelexec.BlockedUnauthorized},
	} {
		t.Run(item.name, func(t *testing.T) {
			performer := executor{claims: item.claims, rate: openRate(t)}
			execution, _ := performer.perform(
				context.Background(), true, item.suspended, item.authorization,
				tunnelexec.Current{PID: 7})
			if execution == nil {
				t.Fatal("a decision to act recorded nothing about the act")
			}
			if execution.Performed || execution.Blocked != string(item.want) {
				t.Fatalf("execution = %+v, want blocked by %q", execution, item.want)
			}
		})
	}
}

// A cycle that decided to do nothing has no act to account for.
func TestACycleThatDecidedNothingRecordsNoExecution(t *testing.T) {
	performer := executor{claims: fakeClaims{held: true}, rate: openRate(t)}
	if execution, _ := performer.perform(
		context.Background(), false, false, &event.TunnelAuthorization{Allowed: true}, tunnelexec.Current{PID: 7}); execution != nil {
		t.Fatalf("a decision to do nothing recorded %+v", execution)
	}
}

// Every gate open and nothing to perform with is a runtime installed without
// what a rebuild needs. It records that nothing happened rather than claiming an
// act, and it does not count against the rate — nothing was rebuilt.
func TestEveryGateOpenWithoutARebuilderPerformsNothing(t *testing.T) {
	rate := openRate(t)
	performer := executor{claims: fakeClaims{held: true}, rate: rate}
	execution, _ := performer.perform(
		context.Background(), true, false, &event.TunnelAuthorization{Allowed: true}, tunnelexec.Current{PID: 7})
	if execution == nil || execution.Performed {
		t.Fatalf("execution = %+v", execution)
	}
	performed, err := rate.Performed()
	if err != nil {
		t.Fatal(err)
	}
	if len(performed) != 0 {
		t.Fatalf("a rebuild that never happened was counted: %v", performed)
	}
}

// A rebuild that happened is counted, whether or not it worked: a runtime that
// counted only the ones that worked would rebuild without end exactly where
// rebuilding is not working.
func TestAFailedRebuildStillCountsTowardTheBound(t *testing.T) {
	rate := openRate(t)
	performer := executor{
		claims: fakeClaims{held: true},
		rate:   rate,
		rebuilder: &tunnelexec.Rebuilder{
			Tunnel:  stoppingTunnel{},
			Routes:  countingRoutes{},
			Payload: neverTraverses{},
			Bound:   time.Second, Poll: time.Millisecond,
			Now:   time.Now,
			Sleep: func(context.Context, time.Duration) error { return nil },
		},
	}
	execution, _ := performer.perform(
		context.Background(), true, false, &event.TunnelAuthorization{Allowed: true}, tunnelexec.Current{PID: 7})
	if execution == nil || !execution.Performed ||
		execution.Reason != string(tunnelexec.ReasonPayloadNotPass) {
		t.Fatalf("execution = %+v", execution)
	}
	performed, err := rate.Performed()
	if err != nil {
		t.Fatal(err)
	}
	if len(performed) != 1 {
		t.Fatalf("a failed rebuild was counted %d times", len(performed))
	}
}

type stoppingTunnel struct{}

func (stoppingTunnel) Stop(context.Context, int) error           { return nil }
func (stoppingTunnel) Start(context.Context) (int, error)        { return 99, nil }
func (stoppingTunnel) Interface(context.Context) (string, error) { return "utun16", nil }

type countingRoutes struct{}

func (countingRoutes) Restore(context.Context, netip.Addr, string) error { return nil }

type neverTraverses struct{}

func (neverTraverses) Traversed(context.Context) (bool, error) { return false, nil }

// What a rebuild replaces comes from what the cycle already observed: the
// process it saw, the interface it holds, and the configured destinations whose
// routes point at that interface — and nothing pointing anywhere else.
func TestTheCurrentTunnelIsWhatTheCycleObserved(t *testing.T) {
	summary := Summary{Observed: connectivityhost.Evidence{
		ManagedTUN: observe.TUNInterface{Name: "utun15"},
		Routes: []observe.RouteObservation{
			{Destination: netip.MustParseAddr("192.0.2.10"), Interface: "utun15"},
			{Destination: netip.MustParseAddr("192.0.2.11"), Interface: "en0"},
			{Destination: netip.MustParseAddr("192.0.2.12"), Interface: "utun15"},
			{Destination: netip.MustParseAddr("192.0.2.13"), Interface: "utun4"},
		},
	}}
	summary.Observed.Process.Process.PID = 4242
	current := currentTunnel(summary)
	if current.PID != 4242 || current.Interface != "utun15" {
		t.Fatalf("current = %+v", current)
	}
	if len(current.Routes) != 2 ||
		current.Routes[0] != netip.MustParseAddr("192.0.2.10") ||
		current.Routes[1] != netip.MustParseAddr("192.0.2.12") {
		t.Fatalf("routes = %v, want only the tunnel's", current.Routes)
	}
	// A cycle that saw no tunnel interface has nothing to put back.
	summary.Observed.ManagedTUN = observe.TUNInterface{}
	if routes := currentTunnel(summary).Routes; len(routes) != 0 {
		t.Fatalf("a cycle with no tunnel gave %v", routes)
	}
}

// The tunnel is given back through the same transaction the operator runs, and
// the record says whether it was actually given.
func TestTheRuntimeGivesTheTunnelBackThroughTheTransaction(t *testing.T) {
	released := 0
	held := true
	performer := executor{
		claims:           flippingClaims{held: &held},
		rate:             openRate(t),
		payload:          &payloadState{},
		payloadThreshold: 3,
		release: func(context.Context) error {
			released++
			held = false
			return nil
		},
	}
	// Nothing to give back while the runtime stands.
	if handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true, true,
	)); handback != nil {
		t.Fatalf("a standing runtime gave the tunnel back: %+v", handback)
	}
	if released != 0 {
		t.Fatal("the release ran with nothing to release for")
	}
	// A lapsed grant gives it back.
	handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true, false,
	))
	if handback == nil || handback.Reason != string(tunnelexec.HandbackGrantLapsed) || !handback.Given {
		t.Fatalf("handback = %+v", handback)
	}
	if released != 1 {
		t.Fatalf("the release ran %d times", released)
	}
}

// A release that let go of nothing is recorded as one that did not: a runtime
// that decided to let go and still holds the claim is in a different state from
// one that let go.
func TestAReleaseThatFailedIsRecordedAsSuch(t *testing.T) {
	held := true
	performer := executor{
		claims: flippingClaims{held: &held}, rate: openRate(t),
		payload: &payloadState{}, payloadThreshold: 3,
		release: func(context.Context) error { return context.DeadlineExceeded },
	}
	handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true, false,
	))
	if handback == nil || handback.Given {
		t.Fatalf("handback = %+v", handback)
	}
}

// A tunnel that carries nothing gives it back, and one whose outer path is down
// does not: the tunnel is not what is broken.
func TestOnlyAPayloadWithTheOuterPathUpEndsOwnership(t *testing.T) {
	performer := executor{
		claims: fakeClaims{held: true}, rate: openRate(t),
		payload: &payloadState{}, payloadThreshold: 3,
		release: func(context.Context) error { return nil },
	}
	outerDown := tunnelplan.Grounds{Complete: true, LinkPresent: false, PayloadOK: false}
	for index := 0; index < 5; index++ {
		if handback := performer.handBack(context.Background(),
			performer.standing(outerDown, true, true)); handback != nil {
			t.Fatalf("an unreachable outer path gave the tunnel back: %+v", handback)
		}
	}
	dead := tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: false}
	for index := 0; index < 2; index++ {
		if handback := performer.handBack(context.Background(),
			performer.standing(dead, true, true)); handback != nil {
			t.Fatalf("failure %d of three gave the tunnel back: %+v", index+1, handback)
		}
	}
	handback := performer.handBack(context.Background(), performer.standing(dead, true, true))
	if handback == nil || handback.Reason != string(tunnelexec.HandbackDeadTunnel) {
		t.Fatalf("handback = %+v", handback)
	}
	// A cycle that did not finish teaches nothing about the tunnel and does not
	// reset what was counted.
	performer.payload.failures = 2
	if count := performer.payload.observe(dead, false); count != 2 {
		t.Fatalf("an unfinished cycle changed the count to %d", count)
	}
	// Traffic passing clears it.
	if count := performer.payload.observe(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true); count != 0 {
		t.Fatalf("traffic passing left the count at %d", count)
	}
	// And clears it whatever the outer path was doing. A failure with the outer
	// path down says nothing about the tunnel; a success says the tunnel
	// carried traffic, which is the thing being counted against.
	//
	// Measured 2026-09-26T16:34:32Z: this runtime gave the tunnel back naming a
	// dead tunnel while the cycle that decided it recorded two failures against
	// a threshold of three. The count had climbed across cycles whose payload
	// passed and whose outer path read as down, each of which cleared the
	// planner's own ground and not this one.
	performer.payload.failures = 2
	if count := performer.payload.observe(
		tunnelplan.Grounds{Complete: true, LinkPresent: false, PayloadOK: true}, true); count != 0 {
		t.Fatalf("traffic passing with the outer path down left the count at %d", count)
	}
	// A cycle that did not finish still clears nothing: it carries the last
	// answer rather than one of its own, and a dark wake does not probe.
	performer.payload.failures = 2
	if count := performer.payload.observe(
		tunnelplan.Grounds{Complete: false, LinkPresent: true, PayloadOK: true}, false); count != 2 {
		t.Fatalf("an unfinished cycle cleared the count to %d", count)
	}
}

// A runtime that holds no tunnel hands nothing back, however it stands.
func TestARuntimeHoldingNothingHandsNothingBack(t *testing.T) {
	released := 0
	performer := executor{
		claims: fakeClaims{}, rate: openRate(t),
		payload: &payloadState{}, payloadThreshold: 3,
		release: func(context.Context) error { released++; return nil },
	}
	if handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true}, true, false,
	)); handback != nil || released != 0 {
		t.Fatalf("handback = %+v, released %d", handback, released)
	}
}

// A runtime with no authority to ask has no standing. Asking nobody is not the
// same as being told yes, and a runtime that read it that way would hold the
// tunnel on a grant it could not see.
func TestNoAuthorityIsNoStanding(t *testing.T) {
	if authorityActive(nil) {
		t.Fatal("a runtime with no authority was taken as standing")
	}
	if !authorityActive(&answeringAuthority{}) {
		t.Fatal("an authority that allows mutation was taken as lapsed")
	}
}

// The threshold the executor holds is the configured one. A runtime built with
// none hands nothing back for a payload, whatever it fails.
func TestTheExecutorTakesItsThresholdFromTheConfiguration(t *testing.T) {
	config := RuntimeConfig{TunnelSupervision: &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{PayloadFailures: 3},
	}}
	built, err := newExecutor(config, "/etc/hexroute.json", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if built.payloadThreshold != 3 {
		t.Fatalf("threshold = %d, want the configured 3", built.payloadThreshold)
	}
	if payloadThreshold(RuntimeConfig{}) != 0 {
		t.Fatal("a runtime without supervision took a threshold from somewhere")
	}
}

// releaseExecution is the delivery configuration a release is refused without.
func releaseExecution() *RuntimeTunnelExecution {
	return &RuntimeTunnelExecution{
		HandoverBinary: "/opt/hexroute/bin/hexroute-handover",
		VersionPath:    "/etc/hexroute/tunnel-version.json",
		TargetKey:      "21c7e1a8-9d3f-4f13-9b38-f06d8583309a",
		Binary:         "/opt/homebrew/bin/sing-box",
		ContentPath:    "/var/hexroute/tunnel-config.json",
	}
}

// What the runtime asks the handover binary for is a giving-up, not an
// exchange. An exchange takes the tunnel back when nobody else raises one,
// which would undo the decision that started the release.
func TestTheRuntimeAsksForAGivingUp(t *testing.T) {
	arguments := releaseArguments("/etc/hexroute/root-observe.json", releaseExecution())
	var relinquish, release bool
	for _, argument := range arguments {
		switch argument {
		case "--relinquish":
			relinquish = true
		case "release":
			release = true
		}
	}
	if !relinquish || !release {
		t.Fatalf("the runtime asks for %v", arguments)
	}
}

// And it asks for everything the release is refused without.
//
// `release` builds its starter before it touches anything, so it refuses a
// request that cannot start a tunnel even when it is a giving-up that will not
// start one. Measured 2026-09-26: this runtime asked with two flags of six and
// every handback it reached reported `given=False`, so the valve for a lapsed
// grant, a reached bound and a dead tunnel never opened.
func TestTheRuntimeAsksWithEverythingTheReleaseRequires(t *testing.T) {
	execution := releaseExecution()
	arguments := releaseArguments("/etc/hexroute/root-observe.json", execution)
	given := map[string]string{}
	for index := 0; index+1 < len(arguments); index++ {
		if strings.HasPrefix(arguments[index], "--") &&
			!strings.HasPrefix(arguments[index+1], "--") {
			given[arguments[index]] = arguments[index+1]
		}
	}
	for flag, want := range map[string]string{
		"--config":         "/etc/hexroute/root-observe.json",
		"--tunnel-version": execution.VersionPath,
		"--target-key":     execution.TargetKey,
		"--sing-box":       execution.Binary,
		"--content":        execution.ContentPath,
	} {
		if given[flag] != want {
			t.Fatalf("%s = %q, want %q; asked with %v", flag, given[flag], want, arguments)
		}
	}
}

// Giving the tunnel up leaves word where the runtime that can tell the operator
// will find it. The two daemons' only connection runs the other way, so a
// handback nobody wrote down is a handback nobody hears about.
func TestAHandbackLeavesWordForTheOperator(t *testing.T) {
	notice := filepath.Join(t.TempDir(), "tunnel-handback.json")
	held := true
	performer := executor{
		claims: flippingClaims{held: &held}, rate: openRate(t),
		payload: &payloadState{}, payloadThreshold: 3,
		notice:  notice,
		release: func(context.Context) error { held = false; return nil },
	}
	if handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true, false,
	)); handback == nil {
		t.Fatal("a lapsed grant gave the tunnel back without a record")
	}
	left, found, err := tunnelexec.ReadNotice(notice)
	if err != nil || !found {
		t.Fatalf("ReadNotice: %v, found=%v", err, found)
	}
	if left.Reason != string(tunnelexec.HandbackGrantLapsed) || !left.Given {
		t.Fatalf("the word left reads as %+v", left)
	}
	// A runtime that still stands leaves nothing new: the last word is about
	// the state the machine is in.
	before := left
	if handback := performer.handBack(context.Background(), performer.standing(
		tunnelplan.Grounds{Complete: true, LinkPresent: true, PayloadOK: true}, true, true,
	)); handback != nil {
		t.Fatalf("a standing runtime gave the tunnel back: %+v", handback)
	}
	after, _, err := tunnelexec.ReadNotice(notice)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the word changed without a handback: %+v", after)
	}
}

// A rebuild reports the process it started, so the cycle that watches for a loss
// can be told about a replacement this runtime made.
func TestAPerformedRebuildReportsTheProcessItStarted(t *testing.T) {
	performer := executor{
		claims: fakeClaims{held: true},
		rate:   openRate(t),
		rebuilder: &tunnelexec.Rebuilder{
			Tunnel:  stoppingTunnel{},
			Routes:  countingRoutes{},
			Payload: neverTraverses{},
			Bound:   time.Second, Poll: time.Millisecond,
			Now:   time.Now,
			Sleep: func(context.Context, time.Duration) error { return nil },
		},
	}
	execution, started := performer.perform(
		context.Background(), true, false,
		&event.TunnelAuthorization{Allowed: true}, tunnelexec.Current{PID: 7})
	if execution == nil || !execution.Performed {
		t.Fatalf("execution = %+v", execution)
	}
	if started != 99 {
		t.Fatalf("the rebuild reported starting %d, and the tunnel it started is 99", started)
	}
}

// And an act that started nothing reports nothing, so a cycle is not told to
// remember a process nobody raised.
func TestAnActThatStartedNothingReportsNothing(t *testing.T) {
	performer := executor{claims: fakeClaims{}, rate: openRate(t)}
	execution, started := performer.perform(
		context.Background(), true, false,
		&event.TunnelAuthorization{Allowed: true}, tunnelexec.Current{PID: 7})
	if execution == nil || execution.Performed || started != 0 {
		t.Fatalf("execution = %+v, started = %d", execution, started)
	}
}

// tellingCycler is a cycler that remembers what the loop told it about a
// replacement, and ends the loop once it has observed enough.
type tellingCycler struct {
	summary  Summary
	replaced []int
	observed int
	until    int
	stop     func()
}

func (cycler *tellingCycler) Observe(context.Context) Summary {
	cycler.observed++
	if cycler.observed >= cycler.until && cycler.stop != nil {
		cycler.stop()
	}
	return cycler.summary
}

func (cycler *tellingCycler) Replaced(pid int) {
	cycler.replaced = append(cycler.replaced, pid)
}

// The loop tells the cycle what it replaced.
//
// The memory a loss is decided from belongs to the cycle, so an act that
// replaced the process has to reach it. Without this the next cycle compares
// against the process this runtime itself ended: measured 2026-09-26, that
// decided a second rebuild ninety seconds after the first.
//
// It takes more than one cycle to see, because the first cycle of a runtime has
// no control state to ask the grant with and the act is stopped before it
// starts — which is task 9.4, measured on the same morning.
func TestTheLoopTellsTheCycleWhatItReplaced(t *testing.T) {
	root := t.TempDir()
	reader, err := connectivityhost.Open(
		filepath.Join(root, "host"), "boot", filepath.Join(root, "event-archive"))
	if err != nil {
		t.Fatalf("connectivityhost.Open: %v", err)
	}
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentDaemon)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := operator.NewController(
		ipc.RoleRoot, ipc.ModeObserveOnly,
		[]control.Component{control.ComponentTunnel},
		control.NewSnapshot(control.StateHealthy), control.ReasonNone, nil,
		func() control.Tick { return 7 })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cycler := &tellingCycler{
		until: 3, stop: cancel,
		summary: Summary{
			State:           CycleHealthy,
			ProcessObserved: true,
			Tunnel: tunnelplan.Plan{
				Action: tunnelplan.ActionRebuildTunnel,
				Causes: []tunnelplan.Cause{tunnelplan.CauseProcessGone},
				Grounds: tunnelplan.Grounds{
					Complete: true, ProcessObserved: true, ProcessRunning: false,
				},
			},
		},
	}
	performer := executor{
		claims: fakeClaims{held: true},
		rate:   openRate(t),
		rebuilder: &tunnelexec.Rebuilder{
			Tunnel:  stoppingTunnel{},
			Routes:  countingRoutes{},
			Payload: neverTraverses{},
			Bound:   time.Second, Poll: time.Millisecond,
			Now:   time.Now,
			Sleep: func(context.Context, time.Duration) error { return nil },
		},
	}
	authority := &answeringAuthority{
		decision: policy.ActionAuthorizationDecision{Allowed: true},
	}
	if _, err := observeLoop(
		ctx, time.Millisecond, false,
		func() control.Tick { return 7 }, func() time.Duration { return 0 },
		cycler, &fixedHeartbeat{}, controller, nil, nil, logger, reader, nil,
		&rootObservations{}, authority, performer); err != nil {
		t.Fatalf("observeLoop: %v", err)
	}
	var told bool
	for _, pid := range cycler.replaced {
		told = told || pid == 99
	}
	if !told {
		t.Fatalf("the loop told the cycle %v, and the rebuild started 99", cycler.replaced)
	}
}

// What the record says about giving the tunnel back is read from the claim, not
// from the release command's exit status.
//
// A giving-up does not take the tunnel back, so after releasing the claim it
// waits for the previous owner's tunnel to carry traffic and reports a failure
// when it does not. Measured 2026-09-26T14:57:37Z: the valve fired for a dead
// tunnel, the claim was released, the incumbent raised its own within the
// minute, and the record said the tunnel had not been given back.
func TestGivingBackIsReadFromTheClaim(t *testing.T) {
	for _, item := range []struct {
		name     string
		err      error
		releases bool
		claimErr error
		want     bool
	}{
		{"released and could not prove it afterwards",
			errors.New("the previous owner's tunnel did not carry traffic twice"), true, nil, true},
		{"released and proved it", nil, true, nil, true},
		{"reported success and released nothing", nil, false, nil, false},
		{"released nothing and said so", errors.New("refused"), false, nil, false},
		// An unreadable claim is not an answer either way, and a runtime that
		// cannot read it has not shown it gave anything back.
		{"the claim cannot be read", nil, true, tunnelclaim.ErrInvalidClaim, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			held := true
			performer := executor{
				claims:  flippingClaims{held: &held, err: item.claimErr},
				rate:    openRate(t),
				payload: &payloadState{}, payloadThreshold: 3,
				release: func(context.Context) error {
					if item.releases {
						held = false
					}
					return item.err
				},
			}
			handback := performer.handBack(context.Background(), tunnelexec.Standing{
				Owns: true, RateAllows: true, GrantActive: false,
			})
			if handback == nil {
				t.Fatal("a lapsed grant gave the tunnel back without a record")
			}
			if handback.Given != item.want {
				t.Fatalf("given = %v, want %v", handback.Given, item.want)
			}
		})
	}
}

// The threshold that ends ownership is its own number where one is configured.
//
// It answers its own question. The one beside it was chosen when a failing
// payload was a cause to rebuild, which costs about two and a half seconds; this
// decides whether to stop holding the tunnel at all. Measured 2026-09-27 over
// eight hours: 62 of 478 cycles with the outer path up did not traverse and the
// longest run was two, which is what the shared number was set to.
func TestTheThresholdThatEndsOwnershipIsItsOwn(t *testing.T) {
	supervision := func(shared, own uint32) RuntimeConfig {
		execution := &RuntimeTunnelExecution{HandbackPayloadFailures: own}
		return RuntimeConfig{TunnelSupervision: &RuntimeTunnelSupervision{
			Policy:    tunnelplan.Policy{PayloadFailures: shared},
			Execution: execution,
		}}
	}
	if threshold := payloadThreshold(supervision(2, 3)); threshold != 3 {
		t.Fatalf("threshold = %d, want the executor's own 3", threshold)
	}
	// Absent, the number beside it stands, so a runtime installed before this
	// existed behaves exactly as it did.
	if threshold := payloadThreshold(supervision(2, 0)); threshold != 2 {
		t.Fatalf("threshold = %d, want the shared 2", threshold)
	}
	// And a runtime that performs nothing has no threshold to reach.
	if threshold := payloadThreshold(RuntimeConfig{TunnelSupervision: &RuntimeTunnelSupervision{
		Policy: tunnelplan.Policy{PayloadFailures: 2},
	}}); threshold != 2 {
		t.Fatalf("threshold = %d, want the shared 2 where nothing is performed", threshold)
	}
	if threshold := payloadThreshold(RuntimeConfig{}); threshold != 0 {
		t.Fatalf("threshold = %d, want none at all", threshold)
	}
}
