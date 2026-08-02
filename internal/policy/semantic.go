package policy

import (
	"errors"
	"sort"
)

var (
	ErrSemanticNoOp              = errors.New("effective policy is a semantic no-op")
	ErrInvalidGenerationSemantic = errors.New("policy generation does not match semantic changes")
)

type semanticLease struct {
	Domain       Domain     `json:"domain"`
	Capability   Capability `json:"capability"`
	SelectorKeys []string   `json:"selector_keys"`
	IssuedAt     string     `json:"issued_at"`
	ExpiresAt    string     `json:"expires_at"`
}

type semanticDomain struct {
	Domain Domain          `json:"domain"`
	Rules  []string        `json:"rules"`
	Leases []semanticLease `json:"authorization_leases"`
}

type semanticSnapshot struct {
	Schema       string         `json:"schema"`
	PolicySchema uint16         `json:"policy_schema"`
	StaticSHA256 string         `json:"static_sha256"`
	NotBefore    string         `json:"not_before"`
	ExpiresAt    string         `json:"expires_at"`
	Root         semanticDomain `json:"root"`
	User         semanticDomain `json:"user"`
}

func EffectiveSemanticSHA256(snapshot EffectiveSnapshot) (string, error) {
	if snapshot.Validate() != nil {
		return "", ErrInvalidEffectiveSnapshot
	}
	root, err := projectSemanticDomain(snapshot.Root)
	if err != nil {
		return "", err
	}
	user, err := projectSemanticDomain(snapshot.User)
	if err != nil {
		return "", err
	}
	digest, _, err := CanonicalSHA256(semanticSnapshot{
		Schema:       EffectiveSnapshotSchema,
		PolicySchema: snapshot.PolicySchema,
		StaticSHA256: snapshot.StaticSHA256,
		NotBefore:    snapshot.NotBefore,
		ExpiresAt:    snapshot.ExpiresAt,
		Root:         root,
		User:         user,
	})
	return digest, err
}

func DomainSemanticSHA256(payload DomainPayload) (string, error) {
	if payload.Validate() != nil {
		return "", ErrInvalidDomainPayload
	}
	projection, err := projectSemanticDomain(payload)
	if err != nil {
		return "", err
	}
	digest, _, err := CanonicalSHA256(projection)
	return digest, err
}

func IsSemanticNoOp(current, candidate EffectiveSnapshot) (bool, error) {
	currentDigest, err := EffectiveSemanticSHA256(current)
	if err != nil {
		return false, err
	}
	candidateDigest, err := EffectiveSemanticSHA256(candidate)
	if err != nil {
		return false, err
	}
	return currentDigest == candidateDigest, nil
}

func ValidateSemanticAdvance(current, candidate EffectiveSnapshot) error {
	noOp, err := IsSemanticNoOp(current, candidate)
	if err != nil {
		return err
	}
	if noOp {
		return ErrSemanticNoOp
	}
	if candidate.BundleGeneration <= current.BundleGeneration {
		return ErrInvalidGenerationSemantic
	}

	for _, pair := range [][2]DomainPayload{{current.Root, candidate.Root}, {current.User, candidate.User}} {
		currentDigest, err := DomainSemanticSHA256(pair[0])
		if err != nil {
			return err
		}
		candidateDigest, err := DomainSemanticSHA256(pair[1])
		if err != nil {
			return err
		}
		if currentDigest == candidateDigest {
			if candidateGeneration := pair[1].PolicyGeneration; candidateGeneration != pair[0].PolicyGeneration {
				return ErrInvalidGenerationSemantic
			}
			continue
		}
		if pair[1].PolicyGeneration <= pair[0].PolicyGeneration {
			return ErrInvalidGenerationSemantic
		}
	}
	return nil
}

func projectSemanticDomain(payload DomainPayload) (semanticDomain, error) {
	selectorKeys := make(map[string]string, len(payload.Rules))
	ruleSet := make(map[string]struct{}, len(payload.Rules))
	for _, rule := range payload.Rules {
		selectorKey, err := selectorSemanticKey(rule.Selector)
		if err != nil {
			return semanticDomain{}, err
		}
		selectorKeys[rule.Selector.ID] = selectorKey
		ruleKey, err := ruleSemanticKey(rule)
		if err != nil {
			return semanticDomain{}, err
		}
		ruleSet[ruleKey] = struct{}{}
	}
	rules := make([]string, 0, len(ruleSet))
	for key := range ruleSet {
		rules = append(rules, key)
	}
	sort.Strings(rules)

	leaseSet := make(map[string]semanticLease, len(payload.Leases))
	for _, lease := range payload.Leases {
		keys := make([]string, 0, len(lease.SelectorIDs))
		for _, selectorID := range lease.SelectorIDs {
			key, exists := selectorKeys[selectorID]
			if !exists {
				return semanticDomain{}, ErrInvalidDomainPayload
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		projected := semanticLease{
			Domain: lease.Domain, Capability: lease.Capability, SelectorKeys: keys,
			IssuedAt: lease.IssuedAt, ExpiresAt: lease.ExpiresAt,
		}
		key, _, err := CanonicalSHA256(projected)
		if err != nil {
			return semanticDomain{}, err
		}
		leaseSet[key] = projected
	}
	leaseKeys := make([]string, 0, len(leaseSet))
	for key := range leaseSet {
		leaseKeys = append(leaseKeys, key)
	}
	sort.Strings(leaseKeys)
	leases := make([]semanticLease, 0, len(leaseKeys))
	for _, key := range leaseKeys {
		leases = append(leases, leaseSet[key])
	}
	return semanticDomain{Domain: payload.Domain, Rules: rules, Leases: leases}, nil
}
