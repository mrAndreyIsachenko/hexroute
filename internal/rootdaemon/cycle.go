package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

type CycleState string

const (
	CycleHealthy   CycleState = "healthy"
	CycleDegraded  CycleState = "degraded"
	CycleSuspended CycleState = "suspended"
)

type NetworkObserver interface {
	PhysicalNetwork(context.Context, string) (observe.PhysicalNetwork, error)
	TUNInterfaces(context.Context) ([]observe.TUNInterface, error)
	Route(context.Context, netip.Addr) (observe.RouteObservation, error)
	Power(context.Context) (observe.PowerObservation, error)
}

// TunnelProcessObserver reports the sing-box running a given configuration.
//
// The tunnel is identified by what it runs, never by the executable's name: an
// ingress probe runs the same executable against a temporary configuration.
type TunnelProcessObserver interface {
	Tunnel(context.Context, string) (observe.ProcessObservation, error)
}

type EndpointObserver interface {
	Endpoint(context.Context, observe.Endpoint) (observe.ReadinessObservation, error)
}

type Summary struct {
	State CycleState
	// Complete says the cycle reached the end rather than stopping at an
	// observation it could not take. Eight of them can end it early, and the
	// decision below is reached either way — but not from what was never seen.
	Complete bool
	// ProcessObserved says this cycle looked at the tunnel process. A cycle
	// that never reached the observation has not seen the tunnel gone.
	ProcessObserved bool
	SingBoxRunning  bool
	OuterReady      bool
	Failures        uint32
	Plan            routeplan.Plan
	// Observed carries the raw observations this cycle already made, so a
	// reader can build facts from them without probing the host a second
	// time. The cycle gathers all of it either way; keeping it was the only
	// thing missing.
	Observed connectivityhost.Evidence
	// Tunnel is what this runtime would do if it owned the tunnel. It performs
	// none of it: another runtime owns it, and the point of deciding anyway is
	// that the decision can be compared against what that runtime did.
	Tunnel tunnelplan.Plan
	// Carrier is which interface carried each configured destination this
	// cycle. It is what a carrier change is a change in, so a reader comparing
	// two decisions can see what differed rather than only that something did.
	Carrier tunnelplan.Signature
	// Carried is what the carrier signature is built from: the interface carrying
	// the upstream probe and each ingress target, the three paths the runtime
	// this rule reproduces watches. A signature over every configured route
	// changed whenever a fallback route came or went, and whenever the
	// configuration gained a destination.
	Carried []tunnelplan.CarriedDestination
}

// PayloadObserver exercises a path and reports whether traffic traversed it.
//
// It is separate from EndpointObserver because it answers a different question.
// A completed connection proves something accepted a socket; this proves a
// request reached a server and a response came back, which is the only thing
// that tells a tunnel that works from one that answers.
type PayloadObserver interface {
	Payload(context.Context, observe.PayloadEndpoint) (observe.PayloadObservation, error)
}

type Cycle struct {
	config    RuntimeConfig
	network   NetworkObserver
	processes TunnelProcessObserver
	// ownerConfig answers which configuration the tunnel's current owner runs:
	// this runtime's when it holds the claim, the previous owner's otherwise.
	ownerConfig func() (string, error)
	readiness   EndpointObserver
	payload     PayloadObserver
	tunnel      *tunnelStateStore
	now         func() time.Time
	// steady advances only while the machine is running.
	//
	// On this platform Go's monotonic reading is mach_absolute_time, which the
	// kernel suspends across sleep, while the wall clock is not suspended. The
	// difference between the two is recorded as the sleep. It decides nothing:
	// the kernel did not suspend it across idle sleeps on 2026-09-14, and the
	// wake gap is the wall time between cycles, as the owning runtime measures.
	steady func() time.Duration
	// lastObserved and lastSteady are when the previous cycle ran, on each
	// clock. Neither is durable on purpose: across a restart there is no
	// previous cycle of this process to have slept through, and inventing a gap
	// from a file would rebuild the tunnel every time the runtime is installed.
	lastObserved time.Time
	lastSteady   time.Duration
	observed     bool
	// lastTunnelPID is the tunnel process the last cycle that saw one saw. Not
	// durable, for the reason the clocks are not: a process replaced while this
	// runtime was not running is not one it watched go, and remembering it
	// across an installation would decide a loss on every restart.
	lastTunnelPID int
	// lastPayloadOK is the last answer the payload probe gave. A suspended
	// cycle does not probe and carries it, so the ground says the last thing
	// known rather than a failure nobody measured.
	lastPayloadOK bool
}

// WithOwnerConfig sets how the cycle learns which configuration the tunnel's
// owner runs.
func WithOwnerConfig(ownerConfig func() (string, error)) CycleOption {
	return func(cycle *Cycle) {
		if ownerConfig != nil {
			cycle.ownerConfig = ownerConfig
		}
	}
}

func NewCycle(
	config RuntimeConfig,
	network NetworkObserver,
	processes TunnelProcessObserver,
	readiness EndpointObserver,
	options ...CycleOption,
) (*Cycle, error) {
	if network == nil || processes == nil || readiness == nil {
		return nil, errors.New("all observation adapters are required")
	}
	cycle := &Cycle{
		config:    config,
		network:   network,
		processes: processes,
		readiness: readiness,
		now:       wallClock,
		steady:    steadyClock(),
		// Without a claim reader the previous owner is taken to hold the tunnel,
		// which is what every machine is until a handover is made. The daemon
		// wires the real claim; a test that does not care reads no file.
		ownerConfig:   func() (string, error) { return tunnelclaim.PreviousOwnerConfig, nil },
		lastPayloadOK: true,
	}
	for _, option := range options {
		option(cycle)
	}
	return cycle, nil
}

// Observe watches the host and reaches what a tunnel owner would decide.
//
// The decision is reached on every cycle, including the ones that stopped
// early. Eight observations can end a cycle before it finishes, and the tunnel
// being in trouble is exactly when several of them do — so a decision that only
// happened on complete cycles would be absent at the moments it exists for.
// What an incomplete cycle did not see, it does not decide from.
func (cycle *Cycle) Observe(ctx context.Context) Summary {
	summary := cycle.observe(ctx)
	// It performs nothing: another runtime owns the tunnel, and the decision
	// exists to be compared against what that runtime did.
	cycle.decideTunnel(ctx, &summary)
	return summary
}

func (cycle *Cycle) observe(ctx context.Context) Summary {
	summary := Summary{State: CycleDegraded}

	power, err := cycle.network.Power(ctx)
	if err != nil {
		summary.Failures++
		return summary
	}
	// A dark wake or a closed lid suspends what this cycle proposes for the
	// network, not what it sees of the tunnel. The runtime this rule reproduces
	// checks its own process on every tick whatever the lid is doing, and a
	// cycle that stopped here saw no tunnel and named it gone: on the night of
	// 2026-09-18 a machine cycling through dark wakes on battery was reported
	// to have lost its tunnel eight times, and had not.
	suspended := power.WakeKind == observe.WakeKindDark || power.Lid == observe.LidStateClosed

	// The tunnel's own observations come first and cost a process listing: a
	// dark wake lasts seconds and the probes below can outlast it. The runtime
	// this rule reproduces decides on a tick of a sleep and a few quick checks,
	// and deciding later named wakes it did not.
	//
	// A claim that cannot be read is a failed observation, not an absent
	// tunnel. Reporting it as absent would decide process_gone from a file this
	// runtime could not read, which is the unreadable-claim fault the claim
	// itself refuses.
	var process observe.ProcessObservation
	configPath, processErr := cycle.ownerConfig()
	if processErr == nil {
		process, processErr = cycle.processes.Tunnel(ctx, configPath)
	}
	summary.Observed.Process, summary.Observed.ProcessError = process, processErr
	if processErr != nil {
		summary.Failures++
	} else {
		summary.ProcessObserved = true
		summary.SingBoxRunning = process.Running
		if !process.Running {
			summary.Failures++
		}
	}

	physical, err := cycle.network.PhysicalNetwork(ctx, cycle.config.PhysicalInterface)
	summary.Observed.Reached = true
	summary.Observed.Physical, summary.Observed.PhysicalError = physical, err
	summary.Observed.ConfiguredRoutes = uint16(len(cycle.config.Targets))
	if err != nil || !physical.Ready() {
		summary.Failures++
		summary.State = CycleSuspended
		return summary
	}

	tunInterfaces, err := cycle.network.TUNInterfaces(ctx)
	summary.Observed.TUNs, summary.Observed.TUNError = tunInterfaces, err
	if err != nil {
		summary.Failures++
		return summary
	}
	managedTUN, err := observe.FindTUNByAddress(tunInterfaces, cycle.config.ManagedTUNAddress)
	summary.Observed.ManagedTUN = managedTUN
	if err != nil {
		summary.Observed.TUNError = err
		summary.Failures++
		return summary
	}

	var upstream *routeplan.Path
	if cycle.config.UpstreamProbeAddress.IsValid() {
		observation, routeErr := cycle.network.Route(ctx, cycle.config.UpstreamProbeAddress)
		if routeErr != nil {
			summary.Failures++
			return summary
		}
		summary.Carried = append(summary.Carried, tunnelplan.CarriedDestination{
			Destination: cycle.config.UpstreamProbeAddress.String(),
			Interface:   observation.Interface,
		})
		if strings.HasPrefix(observation.Interface, "utun") &&
			observation.Interface != managedTUN.Name {
			upstream = &routeplan.Path{
				Link:      safety.LinkUpstreamVPN,
				Interface: observation.Interface,
			}
		}
	}

	current := make(map[netip.Addr]routeplan.ObservedRoute, len(cycle.config.Targets))
	for _, target := range cycle.config.Targets {
		observation, routeErr := cycle.network.Route(ctx, target.Destination)
		summary.Observed.Routes = append(summary.Observed.Routes, observation)
		// An interface that could not be read is still an entry, as the runtime
		// this rule reproduces writes "unknown" for one: a path that stops
		// answering is a change of carrier to both of them.
		if target.Role == routeplan.RoleIngress {
			summary.Carried = append(summary.Carried, tunnelplan.CarriedDestination{
				Destination: target.Destination.String(),
				Interface:   observation.Interface,
			})
		}
		if routeErr != nil {
			summary.Observed.RouteError = routeErr
			summary.Failures++
			return summary
		}
		current[target.Destination] = routeplan.ObservedRoute{
			Destination: observation.Destination,
			Interface:   observation.Interface,
			Gateway:     observation.Gateway,
			Owned:       routeMatchesTarget(observation, target, physical, managedTUN),
		}
	}

	// Everything above is a local lookup; everything below waits on a network.
	// A suspended cycle stops here, with the tunnel seen and the carrier known.
	if suspended {
		summary.State = CycleSuspended
		return summary
	}

	codex := routeplan.CodexState{}
	outerCount := uint32(0)
	outerReady := uint32(0)
	// The probes wait on a network and say nothing to each other, so they are
	// started together and waited for once. Taken in turn they cost their sum,
	// and this cycle holds the operator socket until it finishes: on
	// 2026-09-11 three of them at about 0.7 seconds each were the whole of what
	// a caller waited for after the archive and the spool stopped listing their
	// directories.
	//
	// Only the waiting changes. Every answer lands at its configured index and
	// the fold below walks them in that order, so the summary is the one
	// sequence produced — including which failure is the one recorded when more
	// than one fails.
	probes := make([]endpointProbe, len(cycle.config.Endpoints))
	var probing sync.WaitGroup
	for index, configuredEndpoint := range cycle.config.Endpoints {
		probing.Add(1)
		go func(index int, endpoint observe.Endpoint) {
			defer probing.Done()
			observation, endpointErr := cycle.readiness.Endpoint(ctx, endpoint)
			probes[index] = endpointProbe{
				observation: observation, err: endpointErr,
			}
		}(index, configuredEndpoint.Endpoint)
	}
	probing.Wait()

	for index, configuredEndpoint := range cycle.config.Endpoints {
		observation, endpointErr := probes[index].observation, probes[index].err
		if endpointErr != nil {
			summary.Observed.ReadinessError = endpointErr
			summary.Failures++
			continue
		}
		if configuredEndpoint.Purpose == PurposeOuterReady {
			summary.Observed.Readiness = append(summary.Observed.Readiness, observation)
		}
		switch configuredEndpoint.Purpose {
		case PurposeOuterReady:
			outerCount++
			if observation.Ready {
				outerReady++
			}
		case PurposeNormalCodex:
			codex.NormalReady = observation.Ready
		case PurposeTwilightCodex:
			codex.TwilightReady = observation.Ready
		}
	}
	summary.OuterReady = outerCount == 0 || outerReady > 0
	if !summary.OuterReady {
		summary.Failures++
	}

	plan, err := routeplan.Build(routeplan.Input{
		Targets: cycle.config.Targets,
		Physical: routeplan.Path{
			Link:      safety.LinkPhysical,
			Interface: physical.Interface,
			Gateway:   physical.Gateway,
		},
		Upstream: upstream,
		TUN: routeplan.Path{
			Link:      safety.LinkTwilightTUN,
			Interface: managedTUN.Name,
		},
		Current: current,
		Codex:   codex,
	})
	if err != nil {
		summary.Failures++
		return summary
	}
	summary.Plan = plan
	if summary.Failures == 0 && len(plan.Operations) == 0 {
		summary.State = CycleHealthy
	}
	summary.Complete = true
	return summary
}

func routeMatchesTarget(
	observation observe.RouteObservation,
	target routeplan.Target,
	physical observe.PhysicalNetwork,
	tun observe.TUNInterface,
) bool {
	if observation.Destination != target.Destination {
		return false
	}
	switch target.Role {
	case routeplan.RoleCodexFallback:
		return observation.Interface == tun.Name
	case routeplan.RoleIngress:
		return observation.Interface == physical.Interface && observation.Gateway == physical.Gateway
	case routeplan.RoleCorporate, routeplan.RoleGitLabHTTPS:
		return observation.Interface == tun.Name
	default:
		return false
	}
}

// endpointProbe is one probe's answer, held at its configured index until every
// probe has answered. It exists so that concurrency changes when the cycle
// waits and not what it concludes.
type endpointProbe struct {
	observation observe.ReadinessObservation
	err         error
}

// CycleOption supplies what the tunnel owner's decision needs beyond the
// observations. They are options because a cycle without them still observes:
// the decision is the new thing here, and a runtime that could not make it
// should still watch the host.
type CycleOption func(*Cycle)

func WithPayloadObserver(payload PayloadObserver) CycleOption {
	return func(cycle *Cycle) { cycle.payload = payload }
}

func WithTunnelState(store *tunnelStateStore) CycleOption {
	return func(cycle *Cycle) { cycle.tunnel = store }
}

func WithCycleClock(now func() time.Time) CycleOption {
	return func(cycle *Cycle) {
		if now != nil {
			cycle.now = now
		}
	}
}

// WithSteadyClock replaces the clock that stops when the machine does.
//
// A test cannot attach a monotonic reading to a time it made up, so a test that
// needs a sleeping machine needs this clock separately: it advances the wall
// clock and leaves this one still. Advancing both is a slow observer.
func WithSteadyClock(steady func() time.Duration) CycleOption {
	return func(cycle *Cycle) {
		if steady != nil {
			cycle.steady = steady
		}
	}
}

// wallClock is the clock a person reads, and carries no monotonic reading.
//
// Stripping it is the whole point. A time.Time from time.Now carries both a wall
// value and a monotonic one, and Sub prefers the monotonic one when both
// operands have it — so subtracting two of them would silently give the same
// quantity the steady clock gives: the recorded sleep would always be zero, and
// the gap between cycles would leave out every sleep that clock stops for.
//
// This cannot be caught by a test in one process: a test cannot make a real
// clock pair diverge without really sleeping. So the property is asserted of
// this function rather than of the arithmetic that uses it.
func wallClock() time.Time { return time.Now().Round(0) }

// steadyClock counts from now, on the reading that stops across sleep.
func steadyClock() func() time.Duration {
	start := time.Now()
	return func() time.Duration { return time.Since(start) }
}

// decideTunnel reaches what a tunnel owner would do and records the memory the
// next cycle needs. It performs nothing.
func (cycle *Cycle) decideTunnel(
	ctx context.Context,
	summary *Summary,
) {
	if cycle.config.TunnelSupervision == nil {
		return
	}
	// Round(0) again, because an injected clock is not obliged to have been
	// stripped and a monotonic reading here would silently zero the divergence.
	at := cycle.now().Round(0)
	steady := cycle.steady()
	slept, tickGap := time.Duration(0), time.Duration(0)
	if cycle.observed {
		wall := at.Sub(cycle.lastObserved)
		ran := steady - cycle.lastSteady
		// A clock that went backwards says nothing about sleep or about a gap.
		if slept = wall - ran; slept < 0 {
			slept = 0
		}
		if tickGap = wall; tickGap < 0 {
			tickGap = 0
		}
	}
	cycle.lastObserved, cycle.lastSteady, cycle.observed = at, steady, true

	// Only a cycle that saw a tunnel running learns which one it was. A cycle
	// that saw none, or could not look, keeps what the last one saw, so a loss
	// spanning it is still seen when a replacement appears.
	replaced := false
	if summary.Observed.ProcessError == nil && summary.Observed.Process.Running {
		pid := summary.Observed.Process.Process.PID
		replaced = cycle.lastTunnelPID != 0 && pid != 0 && pid != cycle.lastTunnelPID
		cycle.lastTunnelPID = pid
	}

	summary.Carrier = tunnelplan.NewSignature(summary.Carried)

	// A path that cannot be exercised is not a failed path. Without a probe the
	// cause simply does not hold, rather than holding on every cycle.
	//
	// A suspended cycle does not probe: the probe waits on a network that is not
	// there, and its timeout would put the decision after the dark wake it was
	// to be decided in. The last answer stands until one is taken, which costs
	// the rule nothing — the payload is a ground here and decides nothing.
	payloadOK := cycle.lastPayloadOK
	if cycle.payload != nil && summary.State != CycleSuspended {
		observation, err := cycle.payload.Payload(
			ctx, cycle.config.TunnelSupervision.Payload)
		payloadOK = err == nil && observation.Traversed
		cycle.lastPayloadOK = payloadOK
	}

	previous := cycle.tunnel.Load()
	plan, next, err := tunnelplan.Decide(
		cycle.config.TunnelSupervision.Policy,
		previous,
		tunnelplan.Observed{
			ProcessObserved: summary.ProcessObserved,
			ProcessRunning:  summary.SingBoxRunning,
			ProcessReplaced: replaced,
			Slept:           slept,
			TickGap:         tickGap,
			Carrier:         summary.Carrier,
			Complete:        summary.Complete,
			LinkPresent:     summary.OuterReady,
			PayloadOK:       payloadOK,
			RoutesDrifted:   len(summary.Plan.Operations) > 0,
		})
	if err != nil {
		return
	}
	summary.Tunnel = plan
	// The memory failing to save is not a reason to stop observing. The next
	// cycle then has no previous one, which costs the three causes that compare
	// against it and none of the three that do not.
	_ = cycle.tunnel.Save(next)
}
