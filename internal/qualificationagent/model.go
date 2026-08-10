package qualificationagent

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
)

const (
	stateSchema        = "hexroute.policy-qualification-state.v1"
	statusSchema       = "hexroute.policy-qualification-status.v1"
	statusSourceSchema = "hexroute.policy-qualification-status-source.v1"
	faultSourceSchema  = "hexroute.policy-qualification-fault-source.v1"
	stateFilename      = "current.json"
	lockFilename       = "agent.lock"
	maximumStateBytes  = 32 * 1024
	maximumArmDuration = 24 * time.Hour
)

type sleepWakeDecision uint8

const (
	sleepWakeInvalid sleepWakeDecision = iota
	sleepWakePending
	sleepWakeComplete
)

type Lifecycle string

const (
	LifecycleCollecting Lifecycle = "collecting"
	LifecycleInvalid    Lifecycle = "invalid"
	LifecycleComplete   Lifecycle = "complete"
)

type StateReason string

const (
	ReasonNone              StateReason = "none"
	ReasonBindingChanged    StateReason = "binding_changed"
	ReasonAuthorization     StateReason = "authorization_suspended"
	ReasonTimingGap         StateReason = "timing_gap"
	ReasonClockAnomaly      StateReason = "clock_anomaly"
	ReasonChainInvalid      StateReason = "chain_invalid"
	ReasonSourceInvalid     StateReason = "source_invalid"
	ReasonWakeInvalid       StateReason = "wake_invalid"
	ReasonAgentRunChanged   StateReason = "agent_run_changed"
	ReasonStatusUnavailable StateReason = "status_unavailable"
)

type Config struct {
	Root           string
	RootSocket     string
	UserSocket     string
	SampleInterval time.Duration
	MaximumGap     time.Duration
	ReadTimeout    time.Duration
}

type PolicySnapshot struct {
	Root ipc.PolicyStatusResult `json:"root"`
	User ipc.PolicyStatusResult `json:"user"`
}

type PlatformSample struct {
	BootID      metadata.UUID
	ObservedAt  time.Time
	MonotonicNS int64
}

type SleepArm struct {
	AgentRunID  metadata.UUID `json:"agent_run_id"`
	BootID      metadata.UUID `json:"boot_id"`
	ArmedAt     string        `json:"armed_at"`
	MonotonicNS int64         `json:"monotonic_ns"`
}

type State struct {
	Schema          string                      `json:"schema"`
	Lifecycle       Lifecycle                   `json:"lifecycle"`
	Reason          StateReason                 `json:"reason"`
	Binding         policyqualification.Binding `json:"binding"`
	CurrentBootID   metadata.UUID               `json:"current_boot_id"`
	LastObservedAt  string                      `json:"last_observed_at"`
	LastMonotonicNS int64                       `json:"last_monotonic_ns"`
	AgentRunID      metadata.UUID               `json:"agent_run_id,omitempty"`
	SleepArm        *SleepArm                   `json:"sleep_arm,omitempty"`
}

type Status struct {
	Schema    string                       `json:"schema"`
	Lifecycle Lifecycle                    `json:"lifecycle"`
	Reason    StateReason                  `json:"reason"`
	Binding   policyqualification.Binding  `json:"binding"`
	Progress  policyqualification.Progress `json:"progress"`
}

type policyProjection struct {
	RootBundleGeneration uint64             `json:"root_bundle_generation"`
	UserBundleGeneration uint64             `json:"user_bundle_generation"`
	RootPolicyGeneration uint64             `json:"root_policy_generation"`
	UserPolicyGeneration uint64             `json:"user_policy_generation"`
	RootManifestSHA256   string             `json:"root_manifest_sha256"`
	UserManifestSHA256   string             `json:"user_manifest_sha256"`
	RootState            policy.PolicyState `json:"root_state"`
	UserState            policy.PolicyState `json:"user_state"`
	RootSuspended        bool               `json:"root_suspended"`
	UserSuspended        bool               `json:"user_suspended"`
}

type statusSource struct {
	Schema     string                      `json:"schema"`
	EventID    metadata.UUID               `json:"event_id"`
	ObservedAt string                      `json:"observed_at"`
	BootID     metadata.UUID               `json:"boot_id"`
	Binding    policyqualification.Binding `json:"binding"`
	Projection policyProjection            `json:"projection"`
}

type faultSource struct {
	Schema           string                           `json:"schema"`
	EventID          metadata.UUID                    `json:"event_id"`
	ObservedAt       string                           `json:"observed_at"`
	BootID           metadata.UUID                    `json:"boot_id"`
	Binding          policyqualification.Binding      `json:"binding"`
	Kind             policyqualification.Kind         `json:"kind"`
	Outcome          policyqualification.FaultOutcome `json:"outcome"`
	TestReportSHA256 string                           `json:"test_report_sha256"`
}

var (
	ErrInvalidConfig       = errors.New("invalid qualification agent configuration")
	ErrInvalidState        = errors.New("invalid qualification agent state")
	ErrSessionInvalid      = errors.New("qualification session is invalid")
	ErrStatusUnavailable   = errors.New("policy status unavailable")
	ErrUnsupportedPlatform = errors.New("qualification platform is unsupported")
)

func (snapshot PolicySnapshot) Validate() error {
	if snapshot.Root.Validate() != nil || snapshot.User.Validate() != nil ||
		snapshot.Root.Status.Domain != policy.DomainRoot ||
		snapshot.User.Status.Domain != policy.DomainUser {
		return ErrStatusUnavailable
	}
	return nil
}

func (snapshot PolicySnapshot) activeBinding(sessionID metadata.UUID) (policyqualification.Binding, error) {
	if snapshot.Validate() != nil || snapshot.Root.Status.State != policy.PolicyActive ||
		snapshot.User.Status.State != policy.PolicyActive ||
		snapshot.Root.AuthorizationSuspension.Suspended ||
		snapshot.User.AuthorizationSuspension.Suspended ||
		snapshot.Root.Status.BundleGeneration != snapshot.User.Status.BundleGeneration ||
		snapshot.Root.Status.ManifestSHA256 != snapshot.User.Status.ManifestSHA256 {
		return policyqualification.Binding{}, ErrStatusUnavailable
	}
	binding := policyqualification.Binding{
		SessionID: sessionID, BundleGeneration: snapshot.Root.Status.BundleGeneration,
		RootPolicyGeneration: snapshot.Root.Status.PolicyGeneration,
		UserPolicyGeneration: snapshot.User.Status.PolicyGeneration,
		ManifestSHA256:       snapshot.Root.Status.ManifestSHA256,
	}
	return binding, binding.Validate()
}

func (snapshot PolicySnapshot) projection() policyProjection {
	return policyProjection{
		RootBundleGeneration: snapshot.Root.Status.BundleGeneration,
		UserBundleGeneration: snapshot.User.Status.BundleGeneration,
		RootPolicyGeneration: snapshot.Root.Status.PolicyGeneration,
		UserPolicyGeneration: snapshot.User.Status.PolicyGeneration,
		RootManifestSHA256:   snapshot.Root.Status.ManifestSHA256,
		UserManifestSHA256:   snapshot.User.Status.ManifestSHA256,
		RootState:            snapshot.Root.Status.State, UserState: snapshot.User.Status.State,
		RootSuspended: snapshot.Root.AuthorizationSuspension.Suspended,
		UserSuspended: snapshot.User.AuthorizationSuspension.Suspended,
	}
}

func expectedProjection(binding policyqualification.Binding) policyProjection {
	return policyProjection{
		RootBundleGeneration: binding.BundleGeneration,
		UserBundleGeneration: binding.BundleGeneration,
		RootPolicyGeneration: binding.RootPolicyGeneration,
		UserPolicyGeneration: binding.UserPolicyGeneration,
		RootManifestSHA256:   binding.ManifestSHA256,
		UserManifestSHA256:   binding.ManifestSHA256,
		RootState:            policy.PolicyActive, UserState: policy.PolicyActive,
	}
}

func (state State) Validate() error {
	if state.Schema != stateSchema || state.Binding.Validate() != nil ||
		state.Lifecycle != LifecycleCollecting && state.Lifecycle != LifecycleInvalid &&
			state.Lifecycle != LifecycleComplete || !state.Reason.valid() ||
		state.Lifecycle == LifecycleCollecting && state.Reason != ReasonNone ||
		state.Lifecycle == LifecycleComplete && state.Reason != ReasonNone ||
		state.Lifecycle == LifecycleInvalid && state.Reason == ReasonNone ||
		metadataUUID(state.CurrentBootID) != nil || !canonicalTime(state.LastObservedAt) ||
		state.LastMonotonicNS < 0 || state.AgentRunID != "" && metadataUUID(state.AgentRunID) != nil {
		return ErrInvalidState
	}
	if state.SleepArm != nil && state.SleepArm.Validate() != nil {
		return ErrInvalidState
	}
	return nil
}

func (arm SleepArm) Validate() error {
	if metadataUUID(arm.AgentRunID) != nil || metadataUUID(arm.BootID) != nil ||
		!canonicalTime(arm.ArmedAt) || arm.MonotonicNS < 0 {
		return ErrInvalidState
	}
	return nil
}

func (reason StateReason) valid() bool {
	switch reason {
	case ReasonNone, ReasonBindingChanged, ReasonAuthorization, ReasonTimingGap,
		ReasonClockAnomaly, ReasonChainInvalid, ReasonSourceInvalid, ReasonWakeInvalid,
		ReasonAgentRunChanged, ReasonStatusUnavailable:
		return true
	default:
		return false
	}
}

func canonicalTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func metadataUUID(value metadata.UUID) error {
	_, err := metadata.ParseUUID(string(value))
	return err
}
