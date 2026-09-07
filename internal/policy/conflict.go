package policy

import (
	"net/netip"
	"sort"
	"strings"
)

const MaxConflictCodes = 8

type ConflictCode string

const (
	ConflictEndpointSemantics ConflictCode = "endpoint_semantics_conflict"
	ConflictRouteSemantics    ConflictCode = "route_semantics_conflict"
	ConflictActionSemantics   ConflictCode = "action_semantics_conflict"
	ConflictCredentialOwner   ConflictCode = "credential_ownership_conflict"
	ConflictCrossDomain       ConflictCode = "cross_domain_ownership"
)

type ConflictReport struct {
	Codes     []ConflictCode `json:"codes"`
	Truncated bool           `json:"truncated"`
}

func (report ConflictReport) Empty() bool {
	return len(report.Codes) == 0
}

type ConflictError struct {
	Report ConflictReport
}

func (err *ConflictError) Error() string {
	return "effective policy contains conflicting selectors"
}

func FindConflicts(snapshot EffectiveSnapshot) ConflictReport {
	type ownedRule struct {
		domain Domain
		rule   Rule
	}
	rules := make([]ownedRule, 0, len(snapshot.Root.Rules)+len(snapshot.User.Rules))
	for _, rule := range snapshot.Root.Rules {
		rules = append(rules, ownedRule{domain: DomainRoot, rule: rule})
	}
	for _, rule := range snapshot.User.Rules {
		rules = append(rules, ownedRule{domain: DomainUser, rule: rule})
	}

	codes := make(map[ConflictCode]struct{})
	truncated := false
	for left := 0; left < len(rules); left++ {
		for right := left + 1; right < len(rules); right++ {
			code, conflict := classifyConflict(rules[left].domain, rules[left].rule, rules[right].domain, rules[right].rule)
			if !conflict {
				continue
			}
			if len(codes) >= MaxConflictCodes {
				if _, exists := codes[code]; !exists {
					truncated = true
				}
				continue
			}
			codes[code] = struct{}{}
		}
	}
	ordered := make([]ConflictCode, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	return ConflictReport{Codes: ordered, Truncated: truncated}
}

func classifyConflict(leftDomain Domain, left Rule, rightDomain Domain, right Rule) (ConflictCode, bool) {
	if !selectorsOverlap(left.Selector, right.Selector) {
		return "", false
	}
	if leftDomain != rightDomain {
		// What an overlap across the two domains means depends on what it
		// contradicts. A credential reference, a route and an endpoint are each
		// singular on this host — one key has one owner, one prefix one path,
		// one host and port one listener — so two domains claiming one denies a
		// fact, and no arrangement of policy makes both true.
		//
		// An action capability has no such owner. The compiled envelope assigns
		// capability and target per domain and may assign the same pair to
		// both, which is how a capability with halves on opposite sides of the
		// privilege boundary is expressed at all; ValidateAgainstEnvelope has
		// already agreed by the time conflicts are looked for. And there is no
		// ambiguity to reject: an evaluation reads one domain's payload, so two
		// domains' rules are never candidates for the same decision.
		if left.Selector.Kind == SelectorAction {
			return "", false
		}
		return ConflictCrossDomain, true
	}
	switch left.Selector.Kind {
	case SelectorEndpoint:
		leftEndpoint := left.Selector.Endpoint
		rightEndpoint := right.Selector.Endpoint
		if left.Effect != right.Effect || leftEndpoint.Protocol != rightEndpoint.Protocol ||
			leftEndpoint.TLS != rightEndpoint.TLS || leftEndpoint.Path != rightEndpoint.Path {
			return ConflictEndpointSemantics, true
		}
	case SelectorRoute:
		if left.Effect != right.Effect || left.Selector.Route.Path != right.Selector.Route.Path {
			return ConflictRouteSemantics, true
		}
	case SelectorAction:
		if left.Effect != right.Effect {
			return ConflictActionSemantics, true
		}
	case SelectorCredential:
		if left.Effect != right.Effect ||
			left.Selector.Credential.Owner != right.Selector.Credential.Owner {
			return ConflictCredentialOwner, true
		}
	}
	return "", false
}

func selectorsOverlap(left, right Selector) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case SelectorEndpoint:
		return hostsOverlap(left.Endpoint.Host, right.Endpoint.Host) &&
			portSetsOverlap(left.Endpoint.Ports, right.Endpoint.Ports)
	case SelectorRoute:
		leftPrefix, leftErr := netip.ParsePrefix(left.Route.Prefix)
		rightPrefix, rightErr := netip.ParsePrefix(right.Route.Prefix)
		return leftErr == nil && rightErr == nil && leftPrefix.Overlaps(rightPrefix)
	case SelectorAction:
		return left.Action.Capability == right.Action.Capability && left.Action.Target == right.Action.Target
	case SelectorCredential:
		return left.Credential.Reference == right.Credential.Reference
	default:
		return false
	}
}

func hostsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	leftAddress, leftIsAddress := netip.ParseAddr(left)
	rightAddress, rightIsAddress := netip.ParseAddr(right)
	if leftIsAddress == nil || rightIsAddress == nil {
		return leftIsAddress == nil && rightIsAddress == nil && leftAddress == rightAddress
	}
	leftBase, leftWildcard := strings.CutPrefix(left, "*.")
	rightBase, rightWildcard := strings.CutPrefix(right, "*.")
	switch {
	case leftWildcard && rightWildcard:
		return leftBase == rightBase || strings.HasSuffix(leftBase, "."+rightBase) ||
			strings.HasSuffix(rightBase, "."+leftBase)
	case leftWildcard:
		return strings.HasSuffix(right, "."+leftBase)
	case rightWildcard:
		return strings.HasSuffix(left, "."+rightBase)
	default:
		return false
	}
}

func portSetsOverlap(left, right []PortRange) bool {
	for _, leftRange := range left {
		for _, rightRange := range right {
			if leftRange.First <= rightRange.Last && rightRange.First <= leftRange.Last {
				return true
			}
		}
	}
	return false
}
