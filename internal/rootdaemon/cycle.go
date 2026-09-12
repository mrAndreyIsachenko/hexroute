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

type SingBoxObserver interface {
	SingBox(context.Context, int) (observe.ProcessObservation, error)
}

type EndpointObserver interface {
	Endpoint(context.Context, observe.Endpoint) (observe.ReadinessObservation, error)
}

type Summary struct {
	State CycleState
	// Complete says the cycle reached the end rather than stopping at an
	// observation it could not take. Eight of them can end it early, and the
	// decision below is reached either way — but not from what was never seen.
	Complete       bool
	SingBoxRunning bool
	OuterReady     bool
	Failures       uint32
	Plan           routeplan.Plan
	// Observed carries the raw observations this cycle already made, so a
	// reader can build facts from them without probing the host a second
	// time. The cycle gathers all of it either way; keeping it was the only
	// thing missing.
	Observed connectivityhost.Evidence
	// Tunnel is what this runtime would do if it owned the tunnel. It performs
	// none of it: another runtime owns it, and the point of deciding anyway is
	// that the decision can be compared against what that runtime did.
	Tunnel tunnelplan.Plan
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
	processes SingBoxObserver
	readiness EndpointObserver
	payload   PayloadObserver
	tunnel    *tunnelStateStore
	now       func() time.Time
	// lastObserved is when the previous cycle ran, for the wake gap. It is not
	// durable on purpose: across a restart there is no previous cycle of this
	// process to have slept through, and inventing a gap from a file would
	// rebuild the tunnel every time the runtime is installed.
	lastObserved time.Time
}

func NewCycle(
	config RuntimeConfig,
	network NetworkObserver,
	processes SingBoxObserver,
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
		now:       time.Now,
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
	if power.WakeKind == observe.WakeKindDark || power.Lid == observe.LidStateClosed {
		summary.State = CycleSuspended
		return summary
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

	process, err := cycle.processes.SingBox(ctx, cycle.config.ExpectedSingBoxParent)
	summary.Observed.Process, summary.Observed.ProcessError = process, err
	if err != nil {
		summary.Failures++
	} else {
		summary.SingBoxRunning = process.Running
		if !process.Running {
			summary.Failures++
		}
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

// decideTunnel reaches what a tunnel owner would do and records the memory the
// next cycle needs. It performs nothing.
func (cycle *Cycle) decideTunnel(
	ctx context.Context,
	summary *Summary,
) {
	if cycle.config.TunnelSupervision == nil {
		return
	}
	at := cycle.now()
	since := time.Duration(0)
	if !cycle.lastObserved.IsZero() && at.After(cycle.lastObserved) {
		since = at.Sub(cycle.lastObserved)
	}
	cycle.lastObserved = at

	carried := make([]tunnelplan.CarriedDestination, 0, len(summary.Observed.Routes))
	for _, route := range summary.Observed.Routes {
		carried = append(carried, tunnelplan.CarriedDestination{
			Destination: route.Destination.String(),
			Interface:   route.Interface,
		})
	}

	// A path that cannot be exercised is not a failed path. Without a probe the
	// cause simply does not hold, rather than holding on every cycle.
	payloadOK := true
	if cycle.payload != nil {
		observation, err := cycle.payload.Payload(
			ctx, cycle.config.TunnelSupervision.Payload)
		payloadOK = err == nil && observation.Traversed
	}

	previous := cycle.tunnel.Load()
	plan, next, err := tunnelplan.Decide(
		cycle.config.TunnelSupervision.Policy,
		previous,
		tunnelplan.Observed{
			ProcessRunning: summary.SingBoxRunning,
			SincePrevious:  since,
			Carrier:        tunnelplan.NewSignature(carried),
			Complete:       summary.Complete,
			LinkPresent:    summary.OuterReady,
			PayloadOK:      payloadOK,
			RoutesDrifted:  len(summary.Plan.Operations) > 0,
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
