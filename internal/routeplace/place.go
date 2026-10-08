// Package routeplace decides where a configured route belongs.
//
// It is deliberately not the planner. internal/routeplan can change the host,
// so internal/connectivityreduce's boundary guard forbids anything holding a
// reconciliation proposal from importing it — and the read model holds
// proposals. Deciding where a route belongs is not the authority to put it
// there, so the decision lives here, below that line, and both the planner and
// the read model read it from one place.
//
// Before this split the scoped-routes fact compared every route against the
// managed tunnel while the configuration assigns routes to three links by role.
// Read 2026-10-08: 14 of 21 routes counted as conflicting, and all 14 were
// exactly where their role asks them to be.
package routeplace

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

type Role string

const (
	RoleIngress       Role = "ingress"
	RoleCorporate     Role = "corporate"
	RoleGitLabHTTPS   Role = "gitlab_https"
	RoleCodexFallback Role = "codex_fallback"
	// RoleInherited is a destination the previous owner of the tunnel routed
	// through it, whose purpose is not recorded anywhere.
	//
	// Five exist on this machine. They are in the supervisor's environment and
	// in no repository's history, no variable of its own names them, none
	// resolves from one, and four of the five are CDN anycast — one service
	// behind four edge addresses, which cannot be attributed from a host
	// because that address serves every customer the CDN has.
	//
	// The name says what is known and no more. Calling them corporate would
	// have been a guess written into every record that mentions them, and the
	// point of carrying them across a handover is that the handover changes who
	// owns the tunnel and nothing about what it carries.
	RoleInherited Role = "inherited"
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

type GitLabSSHObservation struct {
	BindInterface     string
	PhysicalInterface string
}

type PolicyStatus string

const (
	PolicyUnknown   PolicyStatus = "unknown"
	PolicyCompliant PolicyStatus = "compliant"
	PolicyDegraded  PolicyStatus = "degraded"
)

type Input struct {
	Targets   []Target
	Physical  Path
	Upstream  *Path
	TUN       Path
	Current   map[netip.Addr]ObservedRoute
	Codex     CodexState
	GitLabSSH *GitLabSSHObservation
}

var (
	ErrInvalidInput       = errors.New("invalid route planner input")
	ErrAmbiguousOwnership = errors.New("ambiguous route ownership")
	targetNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	interfaceNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
)

func desiredPath(target Target, input Input) (Path, bool, error) {
	switch target.Role {
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
	case RoleCorporate, RoleGitLabHTTPS, RoleInherited:
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
	case RoleCorporate, RoleGitLabHTTPS, RoleCodexFallback, RoleInherited:
		if target.Preferred != "" {
			return ErrInvalidInput
		}
		return nil
	default:
		return ErrInvalidInput
	}
}

func EvaluateGitLabSSH(observation GitLabSSHObservation) (PolicyStatus, error) {
	if !interfaceNamePattern.MatchString(observation.PhysicalInterface) ||
		strings.HasPrefix(observation.PhysicalInterface, "utun") {
		return PolicyUnknown, ErrInvalidInput
	}
	if observation.BindInterface == "" {
		return PolicyDegraded, nil
	}
	if !interfaceNamePattern.MatchString(observation.BindInterface) {
		return PolicyUnknown, ErrInvalidInput
	}
	if observation.BindInterface != observation.PhysicalInterface {
		return PolicyDegraded, nil
	}
	return PolicyCompliant, nil
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

// State is where one configured route stands against what the configuration
// asks of it.
//
// The planner turns a state into an operation and a reason; the read model
// counts states. Both read them from here, so the plan and the fact cannot
// disagree about what the host is doing. Deriving the counts from the planner's
// reasons instead would not work: a fallback route is reported as
// `fallback_required` whether it is absent or standing on the wrong link.
type State string

const (
	// StateInstalled is asked for and on the link its role asks for.
	StateInstalled State = "installed"
	// StateConflicting is asked for and somewhere else.
	StateConflicting State = "conflicting"
	// StateMissing is asked for and not there.
	StateMissing State = "missing"
	// StateUnwanted is asked for nowhere, present, and this runtime's to remove.
	StateUnwanted State = "unwanted"
	// StateUnasked is asked for nowhere and not ours. Outside the question.
	StateUnasked State = "unasked"
)

// Decision is one target, where it belongs, and where it stands.
type Decision struct {
	Target Target
	// Path is where the configuration asks for this route. It is meaningless
	// when Active is false, because then it asks for no route at all.
	Path   Path
	Active bool
	State  State
	// Present is whether the host has a route for this destination, and Current
	// is that route. A caller naming a reason distinguishes absent from
	// misplaced by this rather than by re-reading the host.
	Present bool
	Current ObservedRoute
}

// Decide answers, for every configured target, where it belongs and where it
// stands. It is the one statement of that rule.
func Decide(input Input) ([]Decision, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	decisions := make([]Decision, 0, len(input.Targets))
	seen := make(map[netip.Addr]struct{}, len(input.Targets))
	for _, target := range input.Targets {
		if err := validateTarget(target); err != nil {
			return nil, err
		}
		if _, exists := seen[target.Destination]; exists {
			return nil, ErrAmbiguousOwnership
		}
		seen[target.Destination] = struct{}{}
		path, active, err := desiredPath(target, input)
		if err != nil {
			return nil, err
		}
		current, present := input.Current[target.Destination]
		decisions = append(decisions, Decision{
			Target:  target,
			Path:    path,
			Active:  active,
			State:   classify(target, path, active, current, present),
			Present: present,
			Current: current,
		})
	}
	return decisions, nil
}

func classify(
	target Target,
	path Path,
	active bool,
	current ObservedRoute,
	present bool,
) State {
	if !active {
		if target.Role == RoleCodexFallback && present && current.Owned {
			return StateUnwanted
		}
		return StateUnasked
	}
	if present && routeMatches(current, path) {
		return StateInstalled
	}
	if present {
		return StateConflicting
	}
	return StateMissing
}

// Placement is how the host's routes stand against what the configuration asks
// of them.
type Placement struct {
	// Configured is every target the configuration declares, whether or not it
	// asks for a route under the present conditions. It is reported unchanged
	// so a reader comparing it across time is handed no redefinition.
	Configured uint16
	// Installed is asked for and on the link its role asks for.
	Installed uint16
	// Conflicting is asked for and somewhere else, or present and asked for
	// nowhere. Both are a route the host has and should not have as it is.
	Conflicting uint16
	// Missing is asked for and absent. It is counted apart from Conflicting
	// because the two are different states with different remedies, and a
	// quantity that cannot tell them apart answers neither question.
	Missing uint16
}

// Place counts the states. Ready is reachable from it: a configuration that
// asks for nothing of a role contributes nothing to any of the three judged
// quantities, where a condition requiring every declared route to be installed
// could never hold.
func Place(input Input) (Placement, error) {
	decisions, err := Decide(input)
	if err != nil {
		return Placement{}, err
	}
	placement := Placement{Configured: uint16(len(input.Targets))}
	for _, decision := range decisions {
		switch decision.State {
		case StateInstalled:
			placement.Installed++
		case StateConflicting, StateUnwanted:
			placement.Conflicting++
		case StateMissing:
			placement.Missing++
		case StateUnasked:
		}
	}
	return placement, nil
}

// validateInput refuses the links a decision would be made against before any
// target is judged, because a judgement against a link that cannot be one is
// not a smaller answer, it is a wrong one.
func validateInput(input Input) error {
	if input.Physical.Link == safety.LinkTwilightTUN ||
		(input.Physical.Interface != "" && input.Physical.Interface == input.TUN.Interface) {
		return safety.ErrIngressSelfRoute
	}
	if err := validatePath(input.Physical, false); err != nil {
		return err
	}
	if err := validatePath(input.TUN, true); err != nil {
		return err
	}
	if input.Upstream != nil {
		if input.Upstream.Interface == input.TUN.Interface {
			return safety.ErrIngressSelfRoute
		}
		if err := validatePath(*input.Upstream, false); err != nil ||
			input.Upstream.Link != safety.LinkUpstreamVPN {
			return ErrInvalidInput
		}
	}
	return nil
}
