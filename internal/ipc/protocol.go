package ipc

import (
	"errors"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	ProtocolVersion = 1
	MaxFrameBytes   = 16 * 1024
	MaxRequestID    = 64
)

type DaemonRole string

const (
	RoleRoot DaemonRole = "root"
	RoleUser DaemonRole = "user"
)

type RuntimeMode string

const (
	ModeObserveOnly RuntimeMode = "observe-only"
	ModeActive      RuntimeMode = "active"
)

type Action string

const (
	ActionStatus                 Action = "status"
	ActionExportDiagnostics      Action = "export_diagnostics"
	ActionResumeTarget           Action = "resume_target"
	ActionRescuePritunlService   Action = "rescue_pritunl_service"
	ActionPolicyStatus           Action = "policy_status"
	ActionPreparePolicy          Action = "prepare_policy"
	ActionCommitPolicy           Action = "commit_policy"
	ActionAbortPolicy            Action = "abort_policy"
	ActionReconcilerShadowStatus Action = "reconciler_shadow_status"
)

type Request struct {
	Version                uint16                         `json:"version"`
	RequestID              string                         `json:"request_id"`
	Action                 Action                         `json:"action"`
	Target                 control.Component              `json:"target,omitempty"`
	ExpectedGeneration     uint64                         `json:"expected_generation"`
	PolicyStatus           *PolicyStatusRequest           `json:"policy_status,omitempty"`
	PreparePolicy          *PreparePolicyRequest          `json:"prepare_policy,omitempty"`
	CommitPolicy           *CommitPolicyRequest           `json:"commit_policy,omitempty"`
	AbortPolicy            *AbortPolicyRequest            `json:"abort_policy,omitempty"`
	ReconcilerShadowStatus *ReconcilerShadowStatusRequest `json:"reconciler_shadow_status,omitempty"`
}

type ErrorCode string

const (
	ErrorNone            ErrorCode = ""
	ErrorInvalidRequest  ErrorCode = "invalid_request"
	ErrorUnauthorized    ErrorCode = "unauthorized"
	ErrorStaleGeneration ErrorCode = "stale_generation"
	ErrorPrecondition    ErrorCode = "precondition_failed"
	ErrorInternal        ErrorCode = "internal"
)

type Response struct {
	Version                uint16                        `json:"version"`
	RequestID              string                        `json:"request_id"`
	OK                     bool                          `json:"ok"`
	Error                  ErrorCode                     `json:"error,omitempty"`
	Status                 *Status                       `json:"status,omitempty"`
	Diagnostics            *Diagnostics                  `json:"diagnostics,omitempty"`
	Resume                 *ResumeResult                 `json:"resume,omitempty"`
	PolicyStatus           *PolicyStatusResult           `json:"policy_status,omitempty"`
	PreparePolicy          *PreparePolicyResult          `json:"prepare_policy,omitempty"`
	CommitPolicy           *CommitPolicyResult           `json:"commit_policy,omitempty"`
	AbortPolicy            *AbortPolicyResult            `json:"abort_policy,omitempty"`
	ReconcilerShadowStatus *ReconcilerShadowStatusResult `json:"reconciler_shadow_status,omitempty"`
}

type Status struct {
	Role       DaemonRole          `json:"role"`
	Mode       RuntimeMode         `json:"mode"`
	State      control.State       `json:"state"`
	Generation uint64              `json:"generation"`
	SafeMode   bool                `json:"safe_mode"`
	Policy     *PolicyStatusResult `json:"policy,omitempty"`
}

type Diagnostics struct {
	Status              Status         `json:"status"`
	ConsecutiveFailures uint32         `json:"consecutive_failures"`
	Attempts            uint32         `json:"attempts"`
	LastTick            control.Tick   `json:"last_tick"`
	RecoveringSince     control.Tick   `json:"recovering_since"`
	NextActionAt        control.Tick   `json:"next_action_at"`
	SafeUntil           control.Tick   `json:"safe_until"`
	LastReason          control.Reason `json:"last_reason"`
}

type ResumeResult struct {
	Role               DaemonRole        `json:"role"`
	Target             control.Component `json:"target"`
	PreviousState      control.State     `json:"previous_state"`
	State              control.State     `json:"state"`
	PreviousGeneration uint64            `json:"previous_generation"`
	Generation         uint64            `json:"generation"`
}

var (
	ErrUnsupportedVersion       = errors.New("unsupported IPC protocol version")
	ErrInvalidRequestID         = errors.New("invalid IPC request id")
	ErrUnknownAction            = errors.New("IPC action is not allowlisted")
	ErrInvalidTarget            = errors.New("invalid IPC action target")
	ErrInvalidPolicyMessage     = errors.New("invalid IPC policy message")
	ErrInvalidReconcilerMessage = errors.New("invalid IPC reconciler message")
)

func (request Request) Validate() error {
	if request.Version != ProtocolVersion {
		return ErrUnsupportedVersion
	}
	if !validRequestID(request.RequestID) {
		return ErrInvalidRequestID
	}

	payloads := request.payloadCount()
	switch request.Action {
	case ActionStatus, ActionExportDiagnostics:
		if payloads != 0 {
			return ErrInvalidPolicyMessage
		}
		if request.Target != "" || request.ExpectedGeneration != 0 {
			return ErrInvalidTarget
		}
	case ActionResumeTarget:
		if payloads != 0 {
			return ErrInvalidPolicyMessage
		}
		if !validComponent(request.Target) {
			return ErrInvalidTarget
		}
	case ActionRescuePritunlService:
		if payloads != 0 {
			return ErrInvalidPolicyMessage
		}
		if request.Target != control.ComponentPritunl {
			return ErrInvalidTarget
		}
	case ActionPolicyStatus:
		if !request.validPolicyEnvelope(payloads, request.PolicyStatus != nil) {
			return ErrInvalidPolicyMessage
		}
	case ActionPreparePolicy:
		if !request.validPolicyEnvelope(payloads, request.PreparePolicy != nil) ||
			request.PreparePolicy.Validate() != nil {
			return ErrInvalidPolicyMessage
		}
	case ActionCommitPolicy:
		if !request.validPolicyEnvelope(payloads, request.CommitPolicy != nil) ||
			request.CommitPolicy.Validate() != nil {
			return ErrInvalidPolicyMessage
		}
	case ActionAbortPolicy:
		if !request.validPolicyEnvelope(payloads, request.AbortPolicy != nil) ||
			request.AbortPolicy.Validate() != nil {
			return ErrInvalidPolicyMessage
		}
	case ActionReconcilerShadowStatus:
		if request.Target != "" || request.ExpectedGeneration != 0 ||
			payloads != 1 || request.ReconcilerShadowStatus == nil {
			return ErrInvalidReconcilerMessage
		}
	default:
		return ErrUnknownAction
	}
	return nil
}

func (response Response) Validate() error {
	if response.Version != ProtocolVersion || !validRequestID(response.RequestID) {
		return ErrMalformedFrame
	}
	payloads := 0
	if response.Status != nil {
		payloads++
		if !response.Status.valid() {
			return ErrMalformedFrame
		}
	}
	if response.Diagnostics != nil {
		payloads++
		if !response.Diagnostics.valid() {
			return ErrMalformedFrame
		}
	}
	if response.Resume != nil {
		payloads++
		if !response.Resume.valid() {
			return ErrMalformedFrame
		}
	}
	if response.PolicyStatus != nil {
		payloads++
		if response.PolicyStatus.Validate() != nil {
			return ErrMalformedFrame
		}
	}
	if response.PreparePolicy != nil {
		payloads++
		if response.PreparePolicy.Validate() != nil {
			return ErrMalformedFrame
		}
	}
	if response.CommitPolicy != nil {
		payloads++
		if response.CommitPolicy.Validate() != nil {
			return ErrMalformedFrame
		}
	}
	if response.AbortPolicy != nil {
		payloads++
		if response.AbortPolicy.Validate() != nil {
			return ErrMalformedFrame
		}
	}
	if response.ReconcilerShadowStatus != nil {
		payloads++
		if response.ReconcilerShadowStatus.Validate() != nil {
			return ErrMalformedFrame
		}
	}
	if response.OK {
		if response.Error != ErrorNone || payloads != 1 {
			return ErrMalformedFrame
		}
		return nil
	}
	if !validError(response.Error) || response.Error == ErrorNone || payloads != 0 {
		return ErrMalformedFrame
	}
	return nil
}

func (request Request) payloadCount() int {
	payloads := 0
	for _, present := range []bool{
		request.PolicyStatus != nil,
		request.PreparePolicy != nil,
		request.CommitPolicy != nil,
		request.AbortPolicy != nil,
		request.ReconcilerShadowStatus != nil,
	} {
		if present {
			payloads++
		}
	}
	return payloads
}

func (request Request) validPolicyEnvelope(payloads int, expected bool) bool {
	return request.Target == "" && request.ExpectedGeneration == 0 &&
		payloads == 1 && expected
}

func (status Status) valid() bool {
	if !validRole(status.Role) ||
		!validMode(status.Mode) ||
		!status.State.Valid() ||
		status.SafeMode != (status.State == control.StateSafeMode) {
		return false
	}
	if status.Policy == nil {
		return true
	}
	expectedDomain := policyDomainForRole(status.Role)
	return status.Policy.Validate() == nil &&
		status.Policy.Status.Domain == expectedDomain
}

func policyDomainForRole(role DaemonRole) policy.Domain {
	if role == RoleRoot {
		return policy.DomainRoot
	}
	return policy.DomainUser
}

func (diagnostics Diagnostics) valid() bool {
	return diagnostics.Status.valid() &&
		diagnostics.LastTick >= 0 &&
		diagnostics.RecoveringSince >= 0 &&
		diagnostics.NextActionAt >= 0 &&
		diagnostics.SafeUntil >= 0 &&
		diagnostics.LastReason.Valid()
}

func (result ResumeResult) valid() bool {
	return validRole(result.Role) &&
		validComponent(result.Target) &&
		result.PreviousState == control.StateSafeMode &&
		result.State == control.StateDegraded &&
		result.Generation == result.PreviousGeneration+1
}

func validRole(role DaemonRole) bool {
	switch role {
	case RoleRoot, RoleUser:
		return true
	default:
		return false
	}
}

func validMode(mode RuntimeMode) bool {
	switch mode {
	case ModeObserveOnly, ModeActive:
		return true
	default:
		return false
	}
}

func validError(code ErrorCode) bool {
	switch code {
	case ErrorNone,
		ErrorInvalidRequest,
		ErrorUnauthorized,
		ErrorStaleGeneration,
		ErrorPrecondition,
		ErrorInternal:
		return true
	default:
		return false
	}
}

func validRequestID(requestID string) bool {
	if requestID == "" || len(requestID) > MaxRequestID {
		return false
	}
	for _, character := range requestID {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("-_", character) {
			continue
		}
		return false
	}
	return true
}

func validComponent(component control.Component) bool {
	switch component {
	case control.ComponentNetwork,
		control.ComponentTunnel,
		control.ComponentRoutes,
		control.ComponentPritunl,
		control.ComponentCodex,
		control.ComponentTelegram:
		return true
	default:
		return false
	}
}
