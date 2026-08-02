package policy

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type RollbackValidity struct {
	IssuedAt  string
	NotBefore string
	ExpiresAt string
}

var (
	ErrInvalidRollback          = errors.New("invalid policy rollback")
	ErrRollbackCredentialChange = errors.New("rollback credential state differs from current policy")
)

// CompileRollback reissues historical effective content under generations that
// advance from current. Signing and activation remain normal separate gates.
func CompileRollback(
	target CandidateBundle,
	current CandidateBundle,
	envelope SafetyEnvelope,
	identity CompilerIdentity,
	signerFingerprint string,
	validity RollbackValidity,
) (CandidateBundle, error) {
	if target.Validate() != nil || current.Validate() != nil || envelope.Validate() != nil ||
		identity.Validate() != nil || !validSHA256(signerFingerprint) ||
		target.Manifest.BundleGeneration >= current.Manifest.BundleGeneration ||
		target.Manifest.PolicySchema != current.Manifest.PolicySchema ||
		target.Manifest.StaticSHA256 != current.Manifest.StaticSHA256 ||
		current.Manifest.StaticSHA256 != current.Snapshot.StaticSHA256 ||
		current.Manifest.BundleGeneration == math.MaxUint64 {
		return CandidateBundle{}, ErrInvalidRollback
	}
	envelopeDigest, err := envelope.SHA256()
	if err != nil || envelopeDigest != current.Manifest.StaticSHA256 {
		return CandidateBundle{}, ErrInvalidRollback
	}
	issuedAt, issuedOK := parseCanonicalUTC(validity.IssuedAt)
	notBefore, notBeforeOK := parseCanonicalUTC(validity.NotBefore)
	expiresAt, expiresOK := parseCanonicalUTC(validity.ExpiresAt)
	if !issuedOK || !notBeforeOK || !expiresOK || notBefore.Before(issuedAt) ||
		!expiresAt.After(notBefore) || expiresAt.Sub(notBefore) > maxPolicyValidity {
		return CandidateBundle{}, ErrInvalidRollback
	}

	for _, pair := range [][2]DomainPayload{
		{target.Root, current.Root},
		{target.User, current.User},
	} {
		targetCredentials, err := credentialPolicySHA256(pair[0])
		if err != nil {
			return CandidateBundle{}, ErrInvalidRollback
		}
		currentCredentials, err := credentialPolicySHA256(pair[1])
		if err != nil {
			return CandidateBundle{}, ErrInvalidRollback
		}
		if targetCredentials != currentCredentials {
			return CandidateBundle{}, ErrRollbackCredentialChange
		}
	}

	source := OperatorSource{
		Schema:                 OperatorSourceSchema,
		PolicySchema:           current.Manifest.PolicySchema,
		BundleGeneration:       current.Manifest.BundleGeneration + 1,
		ParentBundleGeneration: current.Manifest.BundleGeneration,
		StaticSHA256:           current.Manifest.StaticSHA256,
		IssuedAt:               validity.IssuedAt,
		NotBefore:              validity.NotBefore,
		ExpiresAt:              validity.ExpiresAt,
		Root:                   rollbackDomainSource(target.Root, current.Root.PolicyGeneration, notBefore),
		User:                   rollbackDomainSource(target.User, current.User.PolicyGeneration, notBefore),
	}
	probe, err := ComposeEffectiveSnapshot(source, envelope)
	if err != nil {
		return CandidateBundle{}, err
	}
	for _, domain := range []Domain{DomainRoot, DomainUser} {
		before := current.Root
		after := probe.Root
		generation := &source.Root.PolicyGeneration
		if domain == DomainUser {
			before = current.User
			after = probe.User
			generation = &source.User.PolicyGeneration
		}
		beforeDigest, err := DomainSemanticSHA256(before)
		if err != nil {
			return CandidateBundle{}, ErrInvalidRollback
		}
		afterDigest, err := DomainSemanticSHA256(after)
		if err != nil {
			return CandidateBundle{}, ErrInvalidRollback
		}
		if beforeDigest != afterDigest {
			if *generation == math.MaxUint64 {
				return CandidateBundle{}, ErrInvalidRollback
			}
			*generation = *generation + 1
		}
	}

	candidate, err := CompileBundle(source, envelope, identity, signerFingerprint, &current.Snapshot)
	if err != nil {
		return CandidateBundle{}, err
	}
	return candidate, nil
}

func rollbackDomainSource(target DomainPayload, currentGeneration uint64, at time.Time) DomainSource {
	rules := make([]Rule, 0, len(target.Rules))
	compiledPrefix := compiledDenyPrefix(target.Domain)
	for _, rule := range target.Rules {
		if strings.HasPrefix(rule.ID, compiledPrefix) || strings.HasPrefix(rule.Selector.ID, compiledPrefix) {
			continue
		}
		rules = append(rules, cloneRollbackRule(rule))
	}
	leases := make([]AuthorizationLease, 0, len(target.Leases))
	for _, lease := range target.Leases {
		issuedAt, issuedOK := parseCanonicalUTC(lease.IssuedAt)
		expiresAt, expiresOK := parseCanonicalUTC(lease.ExpiresAt)
		if !issuedOK || !expiresOK || issuedAt.After(at) || !expiresAt.After(at) {
			continue
		}
		cloned := lease
		cloned.SelectorIDs = append([]string(nil), lease.SelectorIDs...)
		leases = append(leases, cloned)
	}
	return DomainSource{PolicyGeneration: currentGeneration, Rules: rules, Leases: leases}
}

func cloneRollbackRule(rule Rule) Rule {
	cloned := rule
	selector := rule.Selector
	if selector.Endpoint != nil {
		value := *selector.Endpoint
		value.Ports = append([]PortRange(nil), selector.Endpoint.Ports...)
		selector.Endpoint = &value
	}
	if selector.Route != nil {
		value := *selector.Route
		selector.Route = &value
	}
	if selector.Action != nil {
		value := *selector.Action
		selector.Action = &value
	}
	if selector.Credential != nil {
		value := *selector.Credential
		selector.Credential = &value
	}
	cloned.Selector = selector
	return cloned
}

func credentialPolicySHA256(payload DomainPayload) (string, error) {
	if payload.Validate() != nil {
		return "", ErrInvalidDomainPayload
	}
	rules := make([]string, 0)
	for _, rule := range payload.Rules {
		if rule.Selector.Kind != SelectorCredential {
			continue
		}
		digest, err := ruleSemanticKey(rule)
		if err != nil {
			return "", err
		}
		rules = append(rules, digest)
	}
	sort.Strings(rules)
	digest, _, err := CanonicalSHA256(struct {
		Domain Domain   `json:"domain"`
		Rules  []string `json:"rules"`
	}{Domain: payload.Domain, Rules: rules})
	return digest, err
}
