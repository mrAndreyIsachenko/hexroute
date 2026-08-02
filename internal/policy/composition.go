package policy

import (
	"errors"
	"fmt"
	"sort"
)

const EffectiveSnapshotSchema = "hexroute.effective-policy.v1"

var ErrInvalidEffectiveSnapshot = errors.New("invalid effective policy snapshot")

type EffectiveSnapshot struct {
	Schema                 string        `json:"schema"`
	PolicySchema           uint16        `json:"policy_schema"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	ParentBundleGeneration uint64        `json:"parent_bundle_generation"`
	StaticSHA256           string        `json:"static_sha256"`
	IssuedAt               string        `json:"issued_at"`
	NotBefore              string        `json:"not_before"`
	ExpiresAt              string        `json:"expires_at"`
	Root                   DomainPayload `json:"root"`
	User                   DomainPayload `json:"user"`
}

func (snapshot EffectiveSnapshot) Validate() error {
	issuedAt, issuedOK := parseCanonicalUTC(snapshot.IssuedAt)
	notBefore, notBeforeOK := parseCanonicalUTC(snapshot.NotBefore)
	expiresAt, expiresOK := parseCanonicalUTC(snapshot.ExpiresAt)
	if snapshot.Schema != EffectiveSnapshotSchema ||
		snapshot.PolicySchema == 0 ||
		snapshot.BundleGeneration == 0 ||
		snapshot.ParentBundleGeneration >= snapshot.BundleGeneration ||
		!validSHA256(snapshot.StaticSHA256) ||
		!issuedOK || !notBeforeOK || !expiresOK ||
		notBefore.Before(issuedAt) || !expiresAt.After(notBefore) ||
		expiresAt.Sub(notBefore) > maxPolicyValidity ||
		snapshot.Root.Validate() != nil || snapshot.Root.Domain != DomainRoot ||
		snapshot.User.Validate() != nil || snapshot.User.Domain != DomainUser ||
		snapshot.Root.BundleGeneration != snapshot.BundleGeneration ||
		snapshot.User.BundleGeneration != snapshot.BundleGeneration {
		return ErrInvalidEffectiveSnapshot
	}
	return nil
}

func ComposeEffectiveSnapshot(source OperatorSource, envelope SafetyEnvelope) (EffectiveSnapshot, error) {
	if err := ValidateAgainstEnvelope(source, envelope); err != nil {
		return EffectiveSnapshot{}, err
	}

	rootSource, _ := source.DomainPayload(DomainRoot)
	userSource, _ := source.DomainPayload(DomainUser)
	root, err := composeDomain(rootSource, envelope.Root)
	if err != nil {
		return EffectiveSnapshot{}, err
	}
	user, err := composeDomain(userSource, envelope.User)
	if err != nil {
		return EffectiveSnapshot{}, err
	}

	candidate := EffectiveSnapshot{
		Schema:                 EffectiveSnapshotSchema,
		PolicySchema:           source.PolicySchema,
		BundleGeneration:       source.BundleGeneration,
		ParentBundleGeneration: source.ParentBundleGeneration,
		StaticSHA256:           source.StaticSHA256,
		IssuedAt:               source.IssuedAt,
		NotBefore:              source.NotBefore,
		ExpiresAt:              source.ExpiresAt,
		Root:                   root,
		User:                   user,
	}
	if err := candidate.Validate(); err != nil {
		return EffectiveSnapshot{}, err
	}
	if report := FindConflicts(candidate); !report.Empty() {
		return EffectiveSnapshot{}, &ConflictError{Report: report}
	}
	return candidate, nil
}

func composeDomain(source DomainPayload, envelope DomainEnvelope) (DomainPayload, error) {
	rules, selectorRemap, err := normalizeAndDeduplicateRules(source.Rules)
	if err != nil {
		return DomainPayload{}, ErrInvalidEffectiveSnapshot
	}
	leases, err := normalizeLeases(source.Leases, selectorRemap, nil)
	if err != nil {
		return DomainPayload{}, ErrInvalidEffectiveSnapshot
	}

	activated := make(map[string]struct{})
	for _, lease := range leases {
		for _, selectorID := range lease.SelectorIDs {
			for _, rule := range rules {
				if rule.Selector.ID == selectorID && rule.Effect == EffectAllow &&
					leaseAuthorizesRule(lease, rule) {
					activated[selectorID] = struct{}{}
				}
			}
		}
	}

	retained := make([]Rule, 0, len(rules)+len(envelope.DeniedSelectors))
	for _, rule := range rules {
		if rule.Effect == EffectAllow {
			if _, allowed := activated[rule.Selector.ID]; !allowed {
				continue
			}
		}
		if overlapsAny(rule.Selector, envelope.DeniedSelectors) {
			continue
		}
		retained = append(retained, rule)
	}
	for index, selector := range envelope.DeniedSelectors {
		retained = append(retained, Rule{
			ID:       fmt.Sprintf("compiled.%s.deny-%03d", envelope.Domain, index),
			Effect:   EffectDeny,
			Selector: normalizeSelector(selector),
		})
	}

	retained, finalRemap, err := normalizeAndDeduplicateRules(retained)
	if err != nil {
		return DomainPayload{}, ErrInvalidEffectiveSnapshot
	}
	for original, intermediate := range selectorRemap {
		if final, exists := finalRemap[intermediate]; exists {
			selectorRemap[original] = final
		}
	}
	retainedIDs := make(map[string]struct{}, len(retained))
	for _, rule := range retained {
		retainedIDs[rule.Selector.ID] = struct{}{}
	}
	leases, err = normalizeLeases(source.Leases, selectorRemap, retainedIDs)
	if err != nil {
		return DomainPayload{}, ErrInvalidEffectiveSnapshot
	}

	result := DomainPayload{
		Schema:           DomainPayloadSchema,
		Domain:           source.Domain,
		BundleGeneration: source.BundleGeneration,
		PolicyGeneration: source.PolicyGeneration,
		Rules:            retained,
		Leases:           leases,
	}
	if result.Validate() != nil {
		return DomainPayload{}, ErrInvalidEffectiveSnapshot
	}
	return result, nil
}

func normalizeAndDeduplicateRules(input []Rule) ([]Rule, map[string]string, error) {
	type keyedRule struct {
		rule Rule
		key  string
	}
	keyed := make([]keyedRule, 0, len(input))
	for _, original := range input {
		rule := original
		rule.Selector = normalizeSelector(rule.Selector)
		key, err := ruleSemanticKey(rule)
		if err != nil {
			return nil, nil, err
		}
		keyed = append(keyed, keyedRule{rule: rule, key: key})
	}
	sort.Slice(keyed, func(left, right int) bool {
		if keyed[left].key != keyed[right].key {
			return keyed[left].key < keyed[right].key
		}
		if keyed[left].rule.ID != keyed[right].rule.ID {
			return keyed[left].rule.ID < keyed[right].rule.ID
		}
		return keyed[left].rule.Selector.ID < keyed[right].rule.Selector.ID
	})

	result := make([]Rule, 0, len(keyed))
	remap := make(map[string]string, len(keyed))
	for index := 0; index < len(keyed); {
		representative := keyed[index].rule
		result = append(result, representative)
		end := index + 1
		for end < len(keyed) && keyed[end].key == keyed[index].key {
			end++
		}
		for duplicate := index; duplicate < end; duplicate++ {
			remap[keyed[duplicate].rule.Selector.ID] = representative.Selector.ID
		}
		index = end
	}
	return result, remap, nil
}

func normalizeLeases(input []AuthorizationLease, remap map[string]string, retained map[string]struct{}) ([]AuthorizationLease, error) {
	type keyedLease struct {
		lease AuthorizationLease
		key   string
	}
	keyed := make([]keyedLease, 0, len(input))
	for _, original := range input {
		lease := original
		ids := make(map[string]struct{}, len(lease.SelectorIDs))
		for _, originalID := range lease.SelectorIDs {
			selectorID, exists := remap[originalID]
			if !exists {
				selectorID = originalID
			}
			if retained != nil {
				if _, exists := retained[selectorID]; !exists {
					continue
				}
			}
			ids[selectorID] = struct{}{}
		}
		lease.SelectorIDs = lease.SelectorIDs[:0]
		for selectorID := range ids {
			lease.SelectorIDs = append(lease.SelectorIDs, selectorID)
		}
		sort.Strings(lease.SelectorIDs)
		if len(lease.SelectorIDs) == 0 {
			continue
		}
		key, _, err := CanonicalSHA256(struct {
			Domain      Domain     `json:"domain"`
			Capability  Capability `json:"capability"`
			SelectorIDs []string   `json:"selector_ids"`
			IssuedAt    string     `json:"issued_at"`
			ExpiresAt   string     `json:"expires_at"`
		}{lease.Domain, lease.Capability, lease.SelectorIDs, lease.IssuedAt, lease.ExpiresAt})
		if err != nil {
			return nil, err
		}
		keyed = append(keyed, keyedLease{lease: lease, key: key})
	}
	sort.Slice(keyed, func(left, right int) bool {
		if keyed[left].key != keyed[right].key {
			return keyed[left].key < keyed[right].key
		}
		return keyed[left].lease.ID < keyed[right].lease.ID
	})
	result := make([]AuthorizationLease, 0, len(keyed))
	for index, item := range keyed {
		if index > 0 && item.key == keyed[index-1].key {
			continue
		}
		result = append(result, item.lease)
	}
	return result, nil
}

func leaseAuthorizesRule(lease AuthorizationLease, rule Rule) bool {
	if rule.Selector.Kind != SelectorAction {
		return true
	}
	return lease.Capability == rule.Selector.Action.Capability
}

func overlapsAny(selector Selector, denied []Selector) bool {
	for _, deny := range denied {
		if selectorsOverlap(selector, deny) {
			return true
		}
	}
	return false
}

func normalizeSelector(selector Selector) Selector {
	if selector.Endpoint == nil {
		return selector
	}
	endpoint := *selector.Endpoint
	endpoint.Ports = append([]PortRange(nil), endpoint.Ports...)
	sort.Slice(endpoint.Ports, func(left, right int) bool {
		if endpoint.Ports[left].First != endpoint.Ports[right].First {
			return endpoint.Ports[left].First < endpoint.Ports[right].First
		}
		return endpoint.Ports[left].Last < endpoint.Ports[right].Last
	})
	merged := make([]PortRange, 0, len(endpoint.Ports))
	for _, candidate := range endpoint.Ports {
		last := len(merged) - 1
		if last >= 0 && (candidate.First <= merged[last].Last ||
			(merged[last].Last < ^uint16(0) && candidate.First == merged[last].Last+1)) {
			if candidate.Last > merged[last].Last {
				merged[last].Last = candidate.Last
			}
			continue
		}
		merged = append(merged, candidate)
	}
	endpoint.Ports = merged
	selector.Endpoint = &endpoint
	return selector
}

func selectorSemanticKey(selector Selector) (string, error) {
	selector = normalizeSelector(selector)
	selector.ID = "semantic"
	digest, _, err := CanonicalSHA256(selector)
	return digest, err
}

func ruleSemanticKey(rule Rule) (string, error) {
	selectorKey, err := selectorSemanticKey(rule.Selector)
	if err != nil {
		return "", err
	}
	digest, _, err := CanonicalSHA256(struct {
		Effect   Effect `json:"effect"`
		Selector string `json:"selector"`
	}{rule.Effect, selectorKey})
	return digest, err
}
