package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/routeplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
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
	State          CycleState
	SingBoxRunning bool
	OuterReady     bool
	Failures       uint32
	Plan           routeplan.Plan
}

type Cycle struct {
	config    RuntimeConfig
	network   NetworkObserver
	processes SingBoxObserver
	readiness EndpointObserver
}

func NewCycle(
	config RuntimeConfig,
	network NetworkObserver,
	processes SingBoxObserver,
	readiness EndpointObserver,
) (*Cycle, error) {
	if network == nil || processes == nil || readiness == nil {
		return nil, errors.New("all observation adapters are required")
	}
	return &Cycle{
		config:    config,
		network:   network,
		processes: processes,
		readiness: readiness,
	}, nil
}

func (cycle *Cycle) Observe(ctx context.Context) Summary {
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
	if err != nil || !physical.Ready() {
		summary.Failures++
		summary.State = CycleSuspended
		return summary
	}

	process, err := cycle.processes.SingBox(ctx, cycle.config.ExpectedSingBoxParent)
	if err != nil {
		summary.Failures++
	} else {
		summary.SingBoxRunning = process.Running
		if !process.Running {
			summary.Failures++
		}
	}

	tunInterfaces, err := cycle.network.TUNInterfaces(ctx)
	if err != nil {
		summary.Failures++
		return summary
	}
	managedTUN, err := observe.FindTUNByAddress(tunInterfaces, cycle.config.ManagedTUNAddress)
	if err != nil {
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
		if routeErr != nil {
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
	for _, configuredEndpoint := range cycle.config.Endpoints {
		observation, endpointErr := cycle.readiness.Endpoint(ctx, configuredEndpoint.Endpoint)
		if endpointErr != nil {
			summary.Failures++
			continue
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
	case routeplan.RoleIngress, routeplan.RoleGitLabSSH:
		return observation.Interface == physical.Interface && observation.Gateway == physical.Gateway
	case routeplan.RoleCorporate, routeplan.RoleGitLabHTTPS:
		return observation.Interface == tun.Name
	default:
		return false
	}
}
