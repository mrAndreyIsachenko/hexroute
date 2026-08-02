package policy

import (
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const maxPolicyValidity = 30 * 24 * time.Hour

var (
	ErrInvalidManifest           = errors.New("invalid policy manifest")
	ErrInvalidDomainPayload      = errors.New("invalid policy domain payload")
	ErrInvalidSelector           = errors.New("invalid policy selector")
	ErrInvalidAuthorizationLease = errors.New("invalid authorization lease")
	ErrInvalidActionLease        = errors.New("invalid action lease")
	ErrInvalidStatus             = errors.New("invalid policy status")
)

func (manifest Manifest) Validate() error {
	issuedAt, issuedOK := parseCanonicalUTC(manifest.IssuedAt)
	notBefore, notBeforeOK := parseCanonicalUTC(manifest.NotBefore)
	expiresAt, expiresOK := parseCanonicalUTC(manifest.ExpiresAt)
	if manifest.Schema != ManifestSchema ||
		manifest.PolicySchema == 0 ||
		!validVersion(manifest.CompilerVersion) ||
		!validSHA256(manifest.CompilerSHA256) ||
		manifest.BundleGeneration == 0 ||
		manifest.ParentBundleGeneration >= manifest.BundleGeneration ||
		!manifest.Root.valid() ||
		!manifest.User.valid() ||
		!validSHA256(manifest.StaticSHA256) ||
		!validSHA256(manifest.SignerFingerprint) ||
		!issuedOK ||
		!notBeforeOK ||
		!expiresOK ||
		notBefore.Before(issuedAt) ||
		!expiresAt.After(notBefore) ||
		expiresAt.Sub(notBefore) > maxPolicyValidity {
		return ErrInvalidManifest
	}
	return nil
}

func (reference DomainReference) valid() bool {
	return reference.Generation > 0 && validSHA256(reference.PayloadSHA256)
}

func (payload DomainPayload) Validate() error {
	if payload.Schema != DomainPayloadSchema ||
		!payload.Domain.Valid() ||
		payload.PolicyGeneration == 0 ||
		len(payload.Rules) > MaxRules ||
		len(payload.Leases) > MaxAuthorizationLeases {
		return ErrInvalidDomainPayload
	}
	ruleIDs := make(map[string]struct{}, len(payload.Rules))
	selectorIDs := make(map[string]struct{}, len(payload.Rules))
	for _, rule := range payload.Rules {
		if !validIdentifier(rule.ID) || !rule.Effect.Valid() || rule.Selector.Validate() != nil {
			return ErrInvalidDomainPayload
		}
		if _, exists := ruleIDs[rule.ID]; exists {
			return ErrInvalidDomainPayload
		}
		if _, exists := selectorIDs[rule.Selector.ID]; exists {
			return ErrInvalidDomainPayload
		}
		ruleIDs[rule.ID] = struct{}{}
		selectorIDs[rule.Selector.ID] = struct{}{}
	}
	leaseIDs := make(map[string]struct{}, len(payload.Leases))
	for _, lease := range payload.Leases {
		if lease.Validate() != nil || lease.Domain != payload.Domain {
			return ErrInvalidDomainPayload
		}
		if _, exists := leaseIDs[lease.ID]; exists {
			return ErrInvalidDomainPayload
		}
		leaseIDs[lease.ID] = struct{}{}
		for _, selectorID := range lease.SelectorIDs {
			if _, exists := selectorIDs[selectorID]; !exists {
				return ErrInvalidDomainPayload
			}
		}
	}
	return nil
}

func (selector Selector) Validate() error {
	if !validIdentifier(selector.ID) || !selector.Kind.Valid() {
		return ErrInvalidSelector
	}
	present := 0
	if selector.Endpoint != nil {
		present++
	}
	if selector.Route != nil {
		present++
	}
	if selector.Action != nil {
		present++
	}
	if selector.Credential != nil {
		present++
	}
	if present != 1 {
		return ErrInvalidSelector
	}

	valid := false
	switch selector.Kind {
	case SelectorEndpoint:
		valid = selector.Endpoint != nil && selector.Endpoint.valid()
	case SelectorRoute:
		valid = selector.Route != nil && selector.Route.valid()
	case SelectorAction:
		valid = selector.Action != nil && selector.Action.valid()
	case SelectorCredential:
		valid = selector.Credential != nil && selector.Credential.valid()
	}
	if !valid {
		return ErrInvalidSelector
	}
	return nil
}

func (selector EndpointSelector) valid() bool {
	if !validHost(selector.Host) ||
		len(selector.Ports) == 0 ||
		len(selector.Ports) > MaxPortRanges ||
		!selector.Protocol.Valid() ||
		!selector.TLS.Valid() ||
		!selector.Path.Valid() {
		return false
	}
	for _, portRange := range selector.Ports {
		if portRange.First == 0 || portRange.Last < portRange.First {
			return false
		}
	}
	return true
}

func (selector RouteSelector) valid() bool {
	prefix, err := netip.ParsePrefix(selector.Prefix)
	return err == nil && prefix.Masked().String() == selector.Prefix && selector.Path.Valid()
}

func (selector ActionSelector) valid() bool {
	return selector.Capability.Valid() && validTarget(selector.Target)
}

func (selector CredentialSelector) valid() bool {
	return validIdentifier(selector.Reference) && selector.Owner.Valid()
}

func (lease AuthorizationLease) Validate() error {
	issuedAt, issuedOK := parseCanonicalUTC(lease.IssuedAt)
	expiresAt, expiresOK := parseCanonicalUTC(lease.ExpiresAt)
	if !validIdentifier(lease.ID) ||
		!lease.Domain.Valid() ||
		!lease.Capability.Valid() ||
		len(lease.SelectorIDs) == 0 ||
		len(lease.SelectorIDs) > MaxSelectorsPerLease ||
		!issuedOK ||
		!expiresOK ||
		!expiresAt.After(issuedAt) ||
		expiresAt.Sub(issuedAt) > maxPolicyValidity {
		return ErrInvalidAuthorizationLease
	}
	seen := make(map[string]struct{}, len(lease.SelectorIDs))
	for _, selectorID := range lease.SelectorIDs {
		if !validIdentifier(selectorID) {
			return ErrInvalidAuthorizationLease
		}
		if _, exists := seen[selectorID]; exists {
			return ErrInvalidAuthorizationLease
		}
		seen[selectorID] = struct{}{}
	}
	return nil
}

func (lease ActionLease) Validate() error {
	issuedAt, issuedOK := parseCanonicalUTC(lease.IssuedAt)
	expiresAt, expiresOK := parseCanonicalUTC(lease.ExpiresAt)
	if lease.Schema != ActionLeaseSchema ||
		!validUUID(lease.ActionID) ||
		!lease.Domain.Valid() ||
		!lease.Capability.Valid() ||
		lease.BundleGeneration == 0 ||
		lease.DomainPolicyGeneration == 0 ||
		lease.ControlStateGeneration == 0 ||
		!validTarget(lease.Target) ||
		!validSHA256(lease.PlanSHA256) ||
		!issuedOK ||
		!expiresOK ||
		!expiresAt.After(issuedAt) ||
		lease.IssuedMonotonicNS < 0 ||
		lease.ExpiresMonotonicNS <= lease.IssuedMonotonicNS ||
		!validUUID(lease.BootID) ||
		!validUUID(lease.Nonce) ||
		!lease.Status.Valid() {
		return ErrInvalidActionLease
	}
	return nil
}

func (status Status) Validate() error {
	if status.Schema != PolicyStatusSchema || !status.Domain.Valid() ||
		!status.State.Valid() || !status.Reason.Valid() {
		return ErrInvalidStatus
	}
	if status.State == PolicyNone {
		if status.BundleGeneration != 0 || status.PolicyGeneration != 0 ||
			status.ManifestSHA256 != "" || status.ActivatedAt != "" ||
			status.Reason != ReasonNoValidGeneration {
			return ErrInvalidStatus
		}
		return nil
	}
	if status.BundleGeneration == 0 || status.PolicyGeneration == 0 ||
		!validSHA256(status.ManifestSHA256) {
		return ErrInvalidStatus
	}
	switch status.State {
	case PolicyPrepared:
		if status.ActivatedAt != "" || status.Reason != ReasonNone {
			return ErrInvalidStatus
		}
	case PolicyActive:
		if _, ok := parseCanonicalUTC(status.ActivatedAt); !ok || status.Reason != ReasonNone {
			return ErrInvalidStatus
		}
	case PolicyRejected:
		if status.ActivatedAt != "" || status.Reason == ReasonNone {
			return ErrInvalidStatus
		}
	case PolicyRestartRequired:
		if status.ActivatedAt != "" || status.Reason != ReasonStaticMismatch {
			return ErrInvalidStatus
		}
	case PolicyDomainMismatch:
		if status.Reason != ReasonDomainMismatch || !validOptionalUTC(status.ActivatedAt) {
			return ErrInvalidStatus
		}
	case PolicyAuthorizationSuspended:
		if status.Reason == ReasonNone || !validOptionalUTC(status.ActivatedAt) {
			return ErrInvalidStatus
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validVersion(value string) bool {
	if value == "" || len(value) > MaxCompilerVersion {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._+-", character) {
			continue
		}
		return false
	}
	return true
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > MaxIdentifierBytes || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func validTarget(value string) bool {
	return len(value) <= MaxTargetBytes && validIdentifier(value)
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func parseCanonicalUTC(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, false
	}
	return parsed, true
}

func validOptionalUTC(value string) bool {
	if value == "" {
		return true
	}
	_, ok := parseCanonicalUTC(value)
	return ok
}

func validHost(value string) bool {
	if value == "" || len(value) > 253 || strings.ToLower(value) != value {
		return false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String() == value
	}
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*.")
		if value == "" {
			return false
		}
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}
