package policy

import (
	"errors"
	"sort"
)

const (
	SemanticDiffSchema = "hexroute.policy-diff.v1"
	MaxDiffEntries     = 2 * MaxRules
)

type DiffClassification string

const (
	DiffNewlyAllowed DiffClassification = "newly_allowed"
	DiffNewlyDenied  DiffClassification = "newly_denied"
	DiffChangedPlan  DiffClassification = "changed_plan"
)

type DiffEntry struct {
	Domain         Domain             `json:"domain"`
	SelectorKind   SelectorKind       `json:"selector_kind"`
	Classification DiffClassification `json:"classification"`
	BeforeSHA256   string             `json:"before_sha256,omitempty"`
	AfterSHA256    string             `json:"after_sha256,omitempty"`
	Expansion      bool               `json:"authorization_expansion"`
}

type SemanticDiff struct {
	Schema                  string      `json:"schema"`
	CurrentSemanticSHA256   string      `json:"current_semantic_sha256"`
	CandidateSemanticSHA256 string      `json:"candidate_semantic_sha256"`
	AuthorizationExpansion  bool        `json:"authorization_expansion"`
	Entries                 []DiffEntry `json:"entries"`
}

var ErrInvalidSemanticDiff = errors.New("invalid semantic policy diff")

func BuildSemanticDiff(current *EffectiveSnapshot, candidate EffectiveSnapshot) (SemanticDiff, error) {
	if candidate.Validate() != nil {
		return SemanticDiff{}, ErrInvalidEffectiveSnapshot
	}
	if report := FindConflicts(candidate); !report.Empty() {
		return SemanticDiff{}, &ConflictError{Report: report}
	}
	candidateDigest, err := EffectiveSemanticSHA256(candidate)
	if err != nil {
		return SemanticDiff{}, err
	}
	currentDigest := SHA256Hex(nil)
	if current != nil {
		if current.Validate() != nil {
			return SemanticDiff{}, ErrInvalidEffectiveSnapshot
		}
		if report := FindConflicts(*current); !report.Empty() {
			return SemanticDiff{}, &ConflictError{Report: report}
		}
		currentDigest, err = EffectiveSemanticSHA256(*current)
		if err != nil {
			return SemanticDiff{}, err
		}
	}

	entries := make([]DiffEntry, 0)
	for _, domain := range []Domain{DomainRoot, DomainUser} {
		var currentPayload *DomainPayload
		candidatePayload := candidate.Root
		if domain == DomainUser {
			candidatePayload = candidate.User
		}
		if current != nil {
			payload := current.Root
			if domain == DomainUser {
				payload = current.User
			}
			currentPayload = &payload
		}
		domainEntries, err := diffDomain(domain, currentPayload, candidatePayload)
		if err != nil {
			return SemanticDiff{}, err
		}
		entries = append(entries, domainEntries...)
	}
	if len(entries) > MaxDiffEntries {
		return SemanticDiff{}, ErrInvalidSemanticDiff
	}
	sort.Slice(entries, func(left, right int) bool {
		leftKey, _, _ := CanonicalSHA256(entries[left])
		rightKey, _, _ := CanonicalSHA256(entries[right])
		return leftKey < rightKey
	})
	expansion := false
	for _, entry := range entries {
		expansion = expansion || entry.Expansion
	}
	report := SemanticDiff{
		Schema: SemanticDiffSchema, CurrentSemanticSHA256: currentDigest,
		CandidateSemanticSHA256: candidateDigest,
		AuthorizationExpansion:  expansion, Entries: entries,
	}
	if report.Validate() != nil {
		return SemanticDiff{}, ErrInvalidSemanticDiff
	}
	return report, nil
}

func (report SemanticDiff) Validate() error {
	if report.Schema != SemanticDiffSchema ||
		!validSHA256(report.CurrentSemanticSHA256) ||
		!validSHA256(report.CandidateSemanticSHA256) ||
		len(report.Entries) > MaxDiffEntries {
		return ErrInvalidSemanticDiff
	}
	expansion := false
	for _, entry := range report.Entries {
		if !entry.Domain.Valid() || !entry.SelectorKind.Valid() ||
			!entry.Classification.Valid() ||
			(entry.BeforeSHA256 != "" && !validSHA256(entry.BeforeSHA256)) ||
			(entry.AfterSHA256 != "" && !validSHA256(entry.AfterSHA256)) ||
			(entry.BeforeSHA256 == "" && entry.AfterSHA256 == "") {
			return ErrInvalidSemanticDiff
		}
		expansion = expansion || entry.Expansion
	}
	if expansion != report.AuthorizationExpansion {
		return ErrInvalidSemanticDiff
	}
	return nil
}

func (classification DiffClassification) Valid() bool {
	return classification == DiffNewlyAllowed || classification == DiffNewlyDenied ||
		classification == DiffChangedPlan
}

func SemanticDiffSHA256(report SemanticDiff) (string, error) {
	if report.Validate() != nil {
		return "", ErrInvalidSemanticDiff
	}
	digest, _, err := CanonicalSHA256(report)
	return digest, err
}

func diffDomain(domain Domain, current *DomainPayload, candidate DomainPayload) ([]DiffEntry, error) {
	type indexedRule struct {
		rule Rule
		plan string
	}
	currentRules := make(map[string]indexedRule)
	candidateRules := make(map[string]indexedRule)
	index := func(destination map[string]indexedRule, rules []Rule) error {
		for _, rule := range rules {
			identity, err := selectorIdentityKey(rule.Selector)
			if err != nil {
				return err
			}
			plan, err := ruleSemanticKey(rule)
			if err != nil {
				return err
			}
			if _, duplicate := destination[identity]; duplicate {
				return ErrInvalidSemanticDiff
			}
			destination[identity] = indexedRule{rule: rule, plan: plan}
		}
		return nil
	}
	if current != nil {
		if err := index(currentRules, current.Rules); err != nil {
			return nil, err
		}
	}
	if err := index(candidateRules, candidate.Rules); err != nil {
		return nil, err
	}
	identities := make(map[string]struct{}, len(currentRules)+len(candidateRules))
	for identity := range currentRules {
		identities[identity] = struct{}{}
	}
	for identity := range candidateRules {
		identities[identity] = struct{}{}
	}
	entries := make([]DiffEntry, 0, len(identities))
	for identity := range identities {
		before, beforeExists := currentRules[identity]
		after, afterExists := candidateRules[identity]
		if beforeExists && afterExists && before.plan == after.plan {
			continue
		}
		entry := DiffEntry{Domain: domain}
		switch {
		case !beforeExists:
			entry.SelectorKind = after.rule.Selector.Kind
			entry.AfterSHA256 = after.plan
			if after.rule.Effect == EffectAllow {
				entry.Classification = DiffNewlyAllowed
				entry.Expansion = true
			} else {
				entry.Classification = DiffNewlyDenied
			}
		case !afterExists:
			entry.SelectorKind = before.rule.Selector.Kind
			entry.BeforeSHA256 = before.plan
			if before.rule.Effect == EffectDeny {
				entry.Classification = DiffNewlyAllowed
				entry.Expansion = true
			} else {
				entry.Classification = DiffNewlyDenied
			}
		default:
			entry.SelectorKind = after.rule.Selector.Kind
			entry.BeforeSHA256 = before.plan
			entry.AfterSHA256 = after.plan
			switch {
			case before.rule.Effect == EffectDeny && after.rule.Effect == EffectAllow:
				entry.Classification = DiffNewlyAllowed
				entry.Expansion = true
			case before.rule.Effect == EffectAllow && after.rule.Effect == EffectDeny:
				entry.Classification = DiffNewlyDenied
			default:
				entry.Classification = DiffChangedPlan
				entry.Expansion = after.rule.Effect == EffectAllow
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func selectorIdentityKey(selector Selector) (string, error) {
	var identity any
	switch selector.Kind {
	case SelectorEndpoint:
		normalized := normalizeSelector(selector)
		identity = struct {
			Kind  SelectorKind `json:"kind"`
			Host  string       `json:"host"`
			Ports []PortRange  `json:"ports"`
		}{selector.Kind, normalized.Endpoint.Host, normalized.Endpoint.Ports}
	case SelectorRoute:
		identity = struct {
			Kind   SelectorKind `json:"kind"`
			Prefix string       `json:"prefix"`
		}{selector.Kind, selector.Route.Prefix}
	case SelectorAction:
		identity = struct {
			Kind       SelectorKind `json:"kind"`
			Capability Capability   `json:"capability"`
			Target     string       `json:"target"`
		}{selector.Kind, selector.Action.Capability, selector.Action.Target}
	case SelectorCredential:
		identity = struct {
			Kind      SelectorKind `json:"kind"`
			Reference string       `json:"reference"`
		}{selector.Kind, selector.Credential.Reference}
	default:
		return "", ErrInvalidSelector
	}
	digest, _, err := CanonicalSHA256(identity)
	return digest, err
}
