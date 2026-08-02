package policy

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"
)

const (
	OperatorSourceSchema  = "hexroute.operator-policy.v1"
	MaxOperatorSourceSize = 256 * 1024
)

type OperatorSource struct {
	Schema                 string       `json:"schema" yaml:"schema"`
	PolicySchema           uint16       `json:"policy_schema" yaml:"policy_schema"`
	BundleGeneration       uint64       `json:"bundle_generation" yaml:"bundle_generation"`
	ParentBundleGeneration uint64       `json:"parent_bundle_generation" yaml:"parent_bundle_generation"`
	StaticSHA256           string       `json:"static_sha256" yaml:"static_sha256"`
	IssuedAt               string       `json:"issued_at" yaml:"issued_at"`
	NotBefore              string       `json:"not_before" yaml:"not_before"`
	ExpiresAt              string       `json:"expires_at" yaml:"expires_at"`
	Root                   DomainSource `json:"root" yaml:"root"`
	User                   DomainSource `json:"user" yaml:"user"`
}

type DomainSource struct {
	PolicyGeneration uint64               `json:"policy_generation" yaml:"policy_generation"`
	Rules            []Rule               `json:"rules" yaml:"rules"`
	Leases           []AuthorizationLease `json:"authorization_leases,omitempty" yaml:"authorization_leases,omitempty"`
}

var ErrInvalidOperatorSource = errors.New("invalid operator policy source")

func DecodeOperatorSource(reader io.Reader) (OperatorSource, error) {
	content, err := io.ReadAll(io.LimitReader(reader, MaxOperatorSourceSize+1))
	if err != nil || len(content) == 0 || len(content) > MaxOperatorSourceSize {
		return OperatorSource{}, ErrInvalidOperatorSource
	}

	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 {
		return OperatorSource{}, ErrInvalidOperatorSource
	}
	if err := rejectUnsafeYAML(&document); err != nil {
		return OperatorSource{}, ErrInvalidOperatorSource
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return OperatorSource{}, ErrInvalidOperatorSource
	}

	var source OperatorSource
	typedDecoder := yaml.NewDecoder(bytes.NewReader(content))
	typedDecoder.KnownFields(true)
	if err := typedDecoder.Decode(&source); err != nil {
		return OperatorSource{}, ErrInvalidOperatorSource
	}
	if err := typedDecoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return OperatorSource{}, ErrInvalidOperatorSource
	}
	if err := source.Validate(); err != nil {
		return OperatorSource{}, err
	}
	return source, nil
}

func (source OperatorSource) Validate() error {
	issuedAt, issuedOK := parseCanonicalUTC(source.IssuedAt)
	notBefore, notBeforeOK := parseCanonicalUTC(source.NotBefore)
	expiresAt, expiresOK := parseCanonicalUTC(source.ExpiresAt)
	if source.Schema != OperatorSourceSchema ||
		source.PolicySchema == 0 ||
		source.BundleGeneration == 0 ||
		source.ParentBundleGeneration >= source.BundleGeneration ||
		!validSHA256(source.StaticSHA256) ||
		!issuedOK ||
		!notBeforeOK ||
		!expiresOK ||
		notBefore.Before(issuedAt) ||
		!expiresAt.After(notBefore) ||
		expiresAt.Sub(notBefore) > maxPolicyValidity {
		return ErrInvalidOperatorSource
	}
	for _, domain := range []Domain{DomainRoot, DomainUser} {
		payload, err := source.DomainPayload(domain)
		if err != nil || payload.Validate() != nil {
			return ErrInvalidOperatorSource
		}
	}
	return nil
}

func (source OperatorSource) DomainPayload(domain Domain) (DomainPayload, error) {
	var input DomainSource
	switch domain {
	case DomainRoot:
		input = source.Root
	case DomainUser:
		input = source.User
	default:
		return DomainPayload{}, ErrInvalidOperatorSource
	}
	return DomainPayload{
		Schema:           DomainPayloadSchema,
		Domain:           domain,
		BundleGeneration: source.BundleGeneration,
		PolicyGeneration: input.PolicyGeneration,
		Rules:            append([]Rule(nil), input.Rules...),
		Leases:           append([]AuthorizationLease(nil), input.Leases...),
	}, nil
}

func rejectUnsafeYAML(node *yaml.Node) error {
	if node == nil || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return ErrInvalidOperatorSource
	}
	if node.Kind == yaml.MappingNode {
		if len(node.Content)%2 != 0 {
			return ErrInvalidOperatorSource
		}
		keys := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return ErrInvalidOperatorSource
			}
			if _, exists := keys[key.Value]; exists {
				return ErrInvalidOperatorSource
			}
			keys[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := rejectUnsafeYAML(child); err != nil {
			return err
		}
	}
	return nil
}
