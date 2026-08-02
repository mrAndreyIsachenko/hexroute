package policy

import (
	"errors"
	"strings"
)

const SafetyEnvelopeSchema = "hexroute.policy-envelope.v1"

type ProtectedField string

const (
	ProtectedDomainIdentity      ProtectedField = "domain_identity"
	ProtectedUIDGID              ProtectedField = "uid_gid"
	ProtectedSocketIdentity      ProtectedField = "socket_identity"
	ProtectedLaunchdIdentity     ProtectedField = "launchd_identity"
	ProtectedExecutableIdentity  ProtectedField = "executable_identity"
	ProtectedStorageRoot         ProtectedField = "storage_root"
	ProtectedCredentialOwnership ProtectedField = "credential_ownership"
	ProtectedActionAllowlist     ProtectedField = "action_allowlist"
	ProtectedNamespace           ProtectedField = "namespace"
	ProtectedSignerFingerprint   ProtectedField = "signer_fingerprint"
	ProtectedSchemaRange         ProtectedField = "schema_range"
)

type SafetyEnvelope struct {
	Schema          string           `json:"schema"`
	ProtectedFields []ProtectedField `json:"protected_fields"`
	Root            DomainEnvelope   `json:"root"`
	User            DomainEnvelope   `json:"user"`
}

type DomainEnvelope struct {
	Domain               Domain         `json:"domain"`
	NamespacePrefix      string         `json:"namespace_prefix"`
	AllowedCapabilities  []Capability   `json:"allowed_capabilities"`
	AllowedSelectorKinds []SelectorKind `json:"allowed_selector_kinds"`
	AllowedTargets       []string       `json:"allowed_targets"`
}

var (
	ErrInvalidSafetyEnvelope = errors.New("invalid compiled safety envelope")
	ErrOutsideSafetyEnvelope = errors.New("policy source exceeds compiled safety envelope")
)

func DefaultSafetyEnvelope() SafetyEnvelope {
	return SafetyEnvelope{
		Schema: SafetyEnvelopeSchema,
		ProtectedFields: []ProtectedField{
			ProtectedDomainIdentity,
			ProtectedUIDGID,
			ProtectedSocketIdentity,
			ProtectedLaunchdIdentity,
			ProtectedExecutableIdentity,
			ProtectedStorageRoot,
			ProtectedCredentialOwnership,
			ProtectedActionAllowlist,
			ProtectedNamespace,
			ProtectedSignerFingerprint,
			ProtectedSchemaRange,
		},
		Root: DomainEnvelope{
			Domain:               DomainRoot,
			NamespacePrefix:      "root.",
			AllowedCapabilities:  []Capability{CapabilityOperatorResume},
			AllowedSelectorKinds: []SelectorKind{SelectorAction},
			AllowedTargets:       []string{"codex", "network", "routes", "runtime", "telegram", "tunnel"},
		},
		User: DomainEnvelope{
			Domain:               DomainUser,
			NamespacePrefix:      "user.",
			AllowedCapabilities:  []Capability{CapabilityOperatorResume},
			AllowedSelectorKinds: []SelectorKind{SelectorAction},
			AllowedTargets:       []string{"pritunl"},
		},
	}
}

func (envelope SafetyEnvelope) Validate() error {
	if envelope.Schema != SafetyEnvelopeSchema ||
		!envelope.Root.valid() || envelope.Root.Domain != DomainRoot ||
		!envelope.User.valid() || envelope.User.Domain != DomainUser ||
		envelope.Root.NamespacePrefix == envelope.User.NamespacePrefix {
		return ErrInvalidSafetyEnvelope
	}
	required := DefaultSafetyEnvelope().ProtectedFields
	if len(envelope.ProtectedFields) != len(required) {
		return ErrInvalidSafetyEnvelope
	}
	seen := make(map[ProtectedField]struct{}, len(envelope.ProtectedFields))
	for _, field := range envelope.ProtectedFields {
		if _, duplicate := seen[field]; duplicate {
			return ErrInvalidSafetyEnvelope
		}
		seen[field] = struct{}{}
	}
	for _, field := range required {
		if _, exists := seen[field]; !exists {
			return ErrInvalidSafetyEnvelope
		}
	}
	return nil
}

func (envelope DomainEnvelope) valid() bool {
	if !envelope.Domain.Valid() ||
		!validNamespacePrefix(envelope.NamespacePrefix) ||
		len(envelope.AllowedCapabilities) == 0 ||
		len(envelope.AllowedSelectorKinds) == 0 ||
		len(envelope.AllowedTargets) == 0 ||
		hasDuplicateCapabilities(envelope.AllowedCapabilities) ||
		hasDuplicateSelectorKinds(envelope.AllowedSelectorKinds) ||
		hasDuplicateStrings(envelope.AllowedTargets) {
		return false
	}
	for _, capability := range envelope.AllowedCapabilities {
		if !capability.Valid() {
			return false
		}
	}
	for _, kind := range envelope.AllowedSelectorKinds {
		if !kind.Valid() {
			return false
		}
	}
	for _, target := range envelope.AllowedTargets {
		if !validTarget(target) {
			return false
		}
	}
	return true
}

func (envelope SafetyEnvelope) SHA256() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	digest, _, err := CanonicalSHA256(envelope)
	return digest, err
}

func ValidateAgainstEnvelope(source OperatorSource, envelope SafetyEnvelope) error {
	if source.Validate() != nil || envelope.Validate() != nil {
		return ErrOutsideSafetyEnvelope
	}
	digest, err := envelope.SHA256()
	if err != nil || source.StaticSHA256 != digest {
		return ErrOutsideSafetyEnvelope
	}
	for _, boundary := range []DomainEnvelope{envelope.Root, envelope.User} {
		payload, err := source.DomainPayload(boundary.Domain)
		if err != nil || !payloadWithinEnvelope(payload, boundary) {
			return ErrOutsideSafetyEnvelope
		}
	}
	return nil
}

func payloadWithinEnvelope(payload DomainPayload, boundary DomainEnvelope) bool {
	if payload.Domain != boundary.Domain || payload.Validate() != nil {
		return false
	}
	for _, rule := range payload.Rules {
		if !strings.HasPrefix(rule.ID, boundary.NamespacePrefix) ||
			!strings.HasPrefix(rule.Selector.ID, boundary.NamespacePrefix) ||
			!containsSelectorKind(boundary.AllowedSelectorKinds, rule.Selector.Kind) {
			return false
		}
		switch rule.Selector.Kind {
		case SelectorAction:
			if !containsCapability(boundary.AllowedCapabilities, rule.Selector.Action.Capability) ||
				!containsString(boundary.AllowedTargets, rule.Selector.Action.Target) {
				return false
			}
		case SelectorCredential:
			if rule.Selector.Credential.Owner != payload.Domain {
				return false
			}
		}
	}
	for _, lease := range payload.Leases {
		if !strings.HasPrefix(lease.ID, boundary.NamespacePrefix) ||
			lease.Domain != boundary.Domain ||
			!containsCapability(boundary.AllowedCapabilities, lease.Capability) {
			return false
		}
		for _, selectorID := range lease.SelectorIDs {
			if !strings.HasPrefix(selectorID, boundary.NamespacePrefix) {
				return false
			}
		}
	}
	return true
}

func validNamespacePrefix(value string) bool {
	return len(value) >= 2 && len(value) <= MaxIdentifierBytes &&
		strings.HasSuffix(value, ".") && validIdentifier(strings.TrimSuffix(value, "."))
}

func containsCapability(values []Capability, candidate Capability) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsSelectorKind(values []SelectorKind, candidate SelectorKind) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func hasDuplicateCapabilities(values []Capability) bool {
	seen := make(map[Capability]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func hasDuplicateSelectorKinds(values []SelectorKind) bool {
	seen := make(map[SelectorKind]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
