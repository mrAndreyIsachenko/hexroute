package routeplan

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

type Role string

const (
	RoleIngress       Role = "ingress"
	RoleCorporate     Role = "corporate"
	RoleGitLabHTTPS   Role = "gitlab_https"
	RoleGitLabSSH     Role = "gitlab_ssh"
	RoleCodexFallback Role = "codex_fallback"
)

type Target struct {
	Name        string
	Destination netip.Addr
	Role        Role
	Preferred   safety.LinkClass
}

type Path struct {
	Link      safety.LinkClass
	Interface string
	Gateway   netip.Addr
}

type ObservedRoute struct {
	Destination netip.Addr
	Interface   string
	Gateway     netip.Addr
	Owned       bool
}

type CodexState struct {
	NormalReady   bool
	TwilightReady bool
}

type Input struct {
	Targets  []Target
	Physical Path
	Upstream *Path
	TUN      Path
	Current  map[netip.Addr]ObservedRoute
	Codex    CodexState
}

type OperationKind string

const (
	OperationEnsureHostRoute      OperationKind = "ensure_host_route"
	OperationRemoveOwnedHostRoute OperationKind = "remove_owned_host_route"
)

type Reason string

const (
	ReasonMissingRoute     Reason = "missing_route"
	ReasonWrongPath        Reason = "wrong_path"
	ReasonFallbackRequired Reason = "fallback_required"
	ReasonFallbackRestored Reason = "fallback_restored"
)

type Operation struct {
	Kind        OperationKind
	Target      string
	Role        Role
	Destination netip.Addr
	Path        Path
	Reason      Reason
}

type Plan struct {
	ObserveOnly bool
	Operations  []Operation
}

var (
	ErrInvalidInput       = errors.New("invalid route planner input")
	ErrAmbiguousOwnership = errors.New("ambiguous route ownership")
	targetNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	interfaceNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
)

func Build(input Input) (Plan, error) {
	if input.Physical.Link == safety.LinkTwilightTUN ||
		(input.Physical.Interface != "" && input.Physical.Interface == input.TUN.Interface) {
		return Plan{}, safety.ErrIngressSelfRoute
	}
	if err := validatePath(input.Physical, false); err != nil {
		return Plan{}, err
	}
	if err := validatePath(input.TUN, true); err != nil {
		return Plan{}, err
	}
	if input.Upstream != nil {
		if input.Upstream.Interface == input.TUN.Interface {
			return Plan{}, safety.ErrIngressSelfRoute
		}
		if err := validatePath(*input.Upstream, false); err != nil ||
			input.Upstream.Link != safety.LinkUpstreamVPN {
			return Plan{}, ErrInvalidInput
		}
	}

	type desiredRoute struct {
		target Target
		path   Path
		active bool
	}
	desired := make([]desiredRoute, 0, len(input.Targets))
	byDestination := make(map[netip.Addr]desiredRoute)
	safetyPlan := safety.RoutePlan{}

	for _, target := range input.Targets {
		if err := validateTarget(target); err != nil {
			return Plan{}, err
		}
		path, active, err := desiredPath(target, input)
		if err != nil {
			return Plan{}, err
		}
		candidate := desiredRoute{target: target, path: path, active: active}
		if _, exists := byDestination[target.Destination]; exists {
			return Plan{}, fmt.Errorf(
				"%w: destination=%s",
				ErrAmbiguousOwnership,
				target.Destination,
			)
		}
		byDestination[target.Destination] = candidate
		desired = append(desired, candidate)

		if active {
			safetyRole := safety.RouteScoped
			if target.Role == RoleIngress {
				safetyRole = safety.RouteIngress
			}
			safetyPlan.Routes = append(safetyPlan.Routes, safety.Route{
				Role: safetyRole,
				Link: path.Link,
			})
		}
	}
	if err := safety.ValidateRoutePlan(safetyPlan); err != nil {
		return Plan{}, err
	}

	plan := Plan{ObserveOnly: true}
	for _, route := range desired {
		current, present := input.Current[route.target.Destination]
		if !route.active {
			if route.target.Role == RoleCodexFallback && present && current.Owned {
				plan.Operations = append(plan.Operations, Operation{
					Kind:        OperationRemoveOwnedHostRoute,
					Target:      route.target.Name,
					Role:        route.target.Role,
					Destination: route.target.Destination,
					Reason:      ReasonFallbackRestored,
				})
			}
			continue
		}
		if present && routeMatches(current, route.path) {
			continue
		}

		reason := ReasonMissingRoute
		if present {
			reason = ReasonWrongPath
		}
		if route.target.Role == RoleCodexFallback {
			reason = ReasonFallbackRequired
		}
		plan.Operations = append(plan.Operations, Operation{
			Kind:        OperationEnsureHostRoute,
			Target:      route.target.Name,
			Role:        route.target.Role,
			Destination: route.target.Destination,
			Path:        route.path,
			Reason:      reason,
		})
	}

	sort.Slice(plan.Operations, func(left, right int) bool {
		leftAddress := plan.Operations[left].Destination.String()
		rightAddress := plan.Operations[right].Destination.String()
		if leftAddress != rightAddress {
			return leftAddress < rightAddress
		}
		return plan.Operations[left].Target < plan.Operations[right].Target
	})
	return plan, nil
}

func desiredPath(target Target, input Input) (Path, bool, error) {
	switch target.Role {
	case RoleGitLabSSH:
		return input.Physical, true, nil
	case RoleIngress:
		switch target.Preferred {
		case "", safety.LinkPhysical:
			return input.Physical, true, nil
		case safety.LinkUpstreamVPN:
			if input.Upstream != nil {
				return *input.Upstream, true, nil
			}
			return input.Physical, true, nil
		case safety.LinkTwilightTUN:
			return Path{}, false, safety.ErrIngressSelfRoute
		default:
			return Path{}, false, ErrInvalidInput
		}
	case RoleCorporate, RoleGitLabHTTPS:
		return input.TUN, true, nil
	case RoleCodexFallback:
		if input.Codex.NormalReady {
			return Path{}, false, nil
		}
		if input.Codex.TwilightReady {
			return input.TUN, true, nil
		}
		return Path{}, false, nil
	default:
		return Path{}, false, ErrInvalidInput
	}
}

func validateTarget(target Target) error {
	if !targetNamePattern.MatchString(target.Name) ||
		!target.Destination.IsValid() ||
		!target.Destination.Is4() {
		return ErrInvalidInput
	}
	switch target.Role {
	case RoleIngress:
		return nil
	case RoleCorporate, RoleGitLabHTTPS, RoleGitLabSSH, RoleCodexFallback:
		if target.Preferred != "" {
			return ErrInvalidInput
		}
		return nil
	default:
		return ErrInvalidInput
	}
}

func validatePath(path Path, tun bool) error {
	if !interfaceNamePattern.MatchString(path.Interface) {
		return ErrInvalidInput
	}
	if tun {
		if path.Link != safety.LinkTwilightTUN {
			return ErrInvalidInput
		}
		return nil
	}
	if path.Link != safety.LinkPhysical && path.Link != safety.LinkUpstreamVPN {
		return ErrInvalidInput
	}
	if path.Link == safety.LinkPhysical && !path.Gateway.IsValid() {
		return ErrInvalidInput
	}
	return nil
}

func routeMatches(route ObservedRoute, path Path) bool {
	if route.Interface != path.Interface {
		return false
	}
	return !path.Gateway.IsValid() || route.Gateway == path.Gateway
}
