package policy

import "github.com/mrAndreyIsachenko/hexroute/internal/metadata"

const (
	ManifestSchema         = "hexroute.policy-manifest.v1"
	DomainPayloadSchema    = "hexroute.policy-domain.v1"
	ActionLeaseSchema      = "hexroute.action-lease.v1"
	PolicyStatusSchema     = "hexroute.policy-status.v1"
	MaxRules               = 256
	MaxAuthorizationLeases = 128
	MaxSelectorsPerLease   = 64
	MaxPortRanges          = 32
	MaxIdentifierBytes     = 64
	MaxTargetBytes         = 64
	MaxCompilerVersion     = 32
)

type Domain string

const (
	DomainRoot Domain = "root"
	DomainUser Domain = "user"
)

func (domain Domain) Valid() bool {
	return domain == DomainRoot || domain == DomainUser
}

type Capability string

const CapabilityOperatorResume Capability = "operator_resume"

func (capability Capability) Valid() bool {
	return capability == CapabilityOperatorResume
}

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

func (effect Effect) Valid() bool {
	return effect == EffectAllow || effect == EffectDeny
}

type SelectorKind string

const (
	SelectorEndpoint   SelectorKind = "endpoint"
	SelectorRoute      SelectorKind = "route"
	SelectorAction     SelectorKind = "action"
	SelectorCredential SelectorKind = "credential"
)

func (kind SelectorKind) Valid() bool {
	switch kind {
	case SelectorEndpoint, SelectorRoute, SelectorAction, SelectorCredential:
		return true
	default:
		return false
	}
}

type Protocol string

const (
	ProtocolTCP Protocol = "tcp"
	ProtocolUDP Protocol = "udp"
)

func (protocol Protocol) Valid() bool {
	return protocol == ProtocolTCP || protocol == ProtocolUDP
}

type TLSMode string

const (
	TLSDisabled    TLSMode = "disabled"
	TLSRequired    TLSMode = "required"
	TLSPassthrough TLSMode = "passthrough"
)

func (mode TLSMode) Valid() bool {
	switch mode {
	case TLSDisabled, TLSRequired, TLSPassthrough:
		return true
	default:
		return false
	}
}

type NetworkPath string

const (
	PathPhysical    NetworkPath = "physical"
	PathManagedTUN  NetworkPath = "managed_tun"
	PathUpstreamVPN NetworkPath = "upstream_vpn"
	PathTwilight    NetworkPath = "twilight"
)

func (path NetworkPath) Valid() bool {
	switch path {
	case PathPhysical, PathManagedTUN, PathUpstreamVPN, PathTwilight:
		return true
	default:
		return false
	}
}

type LeaseStatus string

const (
	LeasePending   LeaseStatus = "pending"
	LeaseCommitted LeaseStatus = "committed"
	LeaseAborted   LeaseStatus = "aborted"
	LeaseExpired   LeaseStatus = "expired"
)

func (status LeaseStatus) Valid() bool {
	switch status {
	case LeasePending, LeaseCommitted, LeaseAborted, LeaseExpired:
		return true
	default:
		return false
	}
}

type PolicyState string

const (
	PolicyNone                   PolicyState = "none"
	PolicyPrepared               PolicyState = "prepared"
	PolicyActive                 PolicyState = "active"
	PolicyRejected               PolicyState = "rejected"
	PolicyRestartRequired        PolicyState = "restart_required"
	PolicyDomainMismatch         PolicyState = "domain_mismatch"
	PolicyAuthorizationSuspended PolicyState = "authorization_suspended"
)

func (state PolicyState) Valid() bool {
	switch state {
	case PolicyNone,
		PolicyPrepared,
		PolicyActive,
		PolicyRejected,
		PolicyRestartRequired,
		PolicyDomainMismatch,
		PolicyAuthorizationSuspended:
		return true
	default:
		return false
	}
}

type PolicyReason string

const (
	ReasonNone              PolicyReason = "none"
	ReasonInvalidSignature  PolicyReason = "invalid_signature"
	ReasonDigestMismatch    PolicyReason = "digest_mismatch"
	ReasonUnsupportedSchema PolicyReason = "unsupported_schema"
	ReasonStaticMismatch    PolicyReason = "static_mismatch"
	ReasonSelectorConflict  PolicyReason = "selector_conflict"
	ReasonClockAnomaly      PolicyReason = "clock_anomaly"
	ReasonDomainMismatch    PolicyReason = "domain_mismatch"
	ReasonIPCOwnership      PolicyReason = "ipc_ownership"
	ReasonNoValidGeneration PolicyReason = "no_valid_generation"
)

func (reason PolicyReason) Valid() bool {
	switch reason {
	case ReasonNone,
		ReasonInvalidSignature,
		ReasonDigestMismatch,
		ReasonUnsupportedSchema,
		ReasonStaticMismatch,
		ReasonSelectorConflict,
		ReasonClockAnomaly,
		ReasonDomainMismatch,
		ReasonIPCOwnership,
		ReasonNoValidGeneration:
		return true
	default:
		return false
	}
}

type DomainReference struct {
	Generation    uint64 `json:"generation" yaml:"generation"`
	PayloadSHA256 string `json:"payload_sha256" yaml:"payload_sha256"`
}

type Manifest struct {
	Schema                 string          `json:"schema" yaml:"schema"`
	PolicySchema           uint16          `json:"policy_schema" yaml:"policy_schema"`
	CompilerVersion        string          `json:"compiler_version" yaml:"compiler_version"`
	CompilerSHA256         string          `json:"compiler_sha256" yaml:"compiler_sha256"`
	BundleGeneration       uint64          `json:"bundle_generation" yaml:"bundle_generation"`
	ParentBundleGeneration uint64          `json:"parent_bundle_generation" yaml:"parent_bundle_generation"`
	Root                   DomainReference `json:"root" yaml:"root"`
	User                   DomainReference `json:"user" yaml:"user"`
	StaticSHA256           string          `json:"static_sha256" yaml:"static_sha256"`
	SignerFingerprint      string          `json:"signer_fingerprint" yaml:"signer_fingerprint"`
	IssuedAt               string          `json:"issued_at" yaml:"issued_at"`
	NotBefore              string          `json:"not_before" yaml:"not_before"`
	ExpiresAt              string          `json:"expires_at" yaml:"expires_at"`
}

type DomainPayload struct {
	Schema           string               `json:"schema" yaml:"schema"`
	Domain           Domain               `json:"domain" yaml:"domain"`
	PolicyGeneration uint64               `json:"policy_generation" yaml:"policy_generation"`
	Rules            []Rule               `json:"rules" yaml:"rules"`
	Leases           []AuthorizationLease `json:"authorization_leases,omitempty" yaml:"authorization_leases,omitempty"`
}

type Rule struct {
	ID       string   `json:"id" yaml:"id"`
	Effect   Effect   `json:"effect" yaml:"effect"`
	Selector Selector `json:"selector" yaml:"selector"`
}

type Selector struct {
	ID         string              `json:"id" yaml:"id"`
	Kind       SelectorKind        `json:"kind" yaml:"kind"`
	Endpoint   *EndpointSelector   `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Route      *RouteSelector      `json:"route,omitempty" yaml:"route,omitempty"`
	Action     *ActionSelector     `json:"action,omitempty" yaml:"action,omitempty"`
	Credential *CredentialSelector `json:"credential,omitempty" yaml:"credential,omitempty"`
}

type EndpointSelector struct {
	Host     string      `json:"host" yaml:"host"`
	Ports    []PortRange `json:"ports" yaml:"ports"`
	Protocol Protocol    `json:"protocol" yaml:"protocol"`
	TLS      TLSMode     `json:"tls" yaml:"tls"`
	Path     NetworkPath `json:"network_path" yaml:"network_path"`
}

type PortRange struct {
	First uint16 `json:"first" yaml:"first"`
	Last  uint16 `json:"last" yaml:"last"`
}

type RouteSelector struct {
	Prefix string      `json:"prefix" yaml:"prefix"`
	Path   NetworkPath `json:"network_path" yaml:"network_path"`
}

type ActionSelector struct {
	Capability Capability `json:"capability" yaml:"capability"`
	Target     string     `json:"target" yaml:"target"`
}

type CredentialSelector struct {
	Reference string `json:"reference" yaml:"reference"`
	Owner     Domain `json:"owner" yaml:"owner"`
}

type AuthorizationLease struct {
	ID          string     `json:"id" yaml:"id"`
	Domain      Domain     `json:"domain" yaml:"domain"`
	Capability  Capability `json:"capability" yaml:"capability"`
	SelectorIDs []string   `json:"selector_ids" yaml:"selector_ids"`
	IssuedAt    string     `json:"issued_at" yaml:"issued_at"`
	ExpiresAt   string     `json:"expires_at" yaml:"expires_at"`
}

type ActionLease struct {
	Schema                 string        `json:"schema"`
	ActionID               metadata.UUID `json:"action_id"`
	Domain                 Domain        `json:"domain"`
	Capability             Capability    `json:"capability"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	DomainPolicyGeneration uint64        `json:"domain_policy_generation"`
	ControlStateGeneration uint64        `json:"control_state_generation"`
	Target                 string        `json:"target"`
	PlanSHA256             string        `json:"plan_sha256"`
	IssuedAt               string        `json:"issued_at"`
	ExpiresAt              string        `json:"expires_at"`
	IssuedMonotonicNS      int64         `json:"issued_monotonic_ns"`
	ExpiresMonotonicNS     int64         `json:"expires_monotonic_ns"`
	BootID                 metadata.UUID `json:"boot_id"`
	Nonce                  metadata.UUID `json:"nonce"`
	Status                 LeaseStatus   `json:"status"`
}

type Status struct {
	Schema           string       `json:"schema"`
	Domain           Domain       `json:"domain"`
	State            PolicyState  `json:"state"`
	BundleGeneration uint64       `json:"bundle_generation"`
	PolicyGeneration uint64       `json:"policy_generation"`
	ManifestSHA256   string       `json:"manifest_sha256,omitempty"`
	ActivatedAt      string       `json:"activated_at,omitempty"`
	Reason           PolicyReason `json:"reason"`
}
