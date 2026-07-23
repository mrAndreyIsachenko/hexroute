package ipc

import (
	"errors"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

const (
	ProtocolVersion = 1
	MaxFrameBytes   = 16 * 1024
	MaxRequestID    = 64
)

type Action string

const (
	ActionStatus               Action = "status"
	ActionExportDiagnostics    Action = "export_diagnostics"
	ActionResumeTarget         Action = "resume_target"
	ActionRescuePritunlService Action = "rescue_pritunl_service"
)

type Request struct {
	Version            uint16            `json:"version"`
	RequestID          string            `json:"request_id"`
	Action             Action            `json:"action"`
	Target             control.Component `json:"target,omitempty"`
	ExpectedGeneration uint64            `json:"expected_generation"`
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
	Version    uint16        `json:"version"`
	RequestID  string        `json:"request_id"`
	OK         bool          `json:"ok"`
	Error      ErrorCode     `json:"error,omitempty"`
	State      control.State `json:"state,omitempty"`
	Generation uint64        `json:"generation"`
}

var (
	ErrUnsupportedVersion = errors.New("unsupported IPC protocol version")
	ErrInvalidRequestID   = errors.New("invalid IPC request id")
	ErrUnknownAction      = errors.New("IPC action is not allowlisted")
	ErrInvalidTarget      = errors.New("invalid IPC action target")
)

func (request Request) Validate() error {
	if request.Version != ProtocolVersion {
		return ErrUnsupportedVersion
	}
	if !validRequestID(request.RequestID) {
		return ErrInvalidRequestID
	}

	switch request.Action {
	case ActionStatus, ActionExportDiagnostics:
		if request.Target != "" {
			return ErrInvalidTarget
		}
	case ActionResumeTarget:
		if !validComponent(request.Target) {
			return ErrInvalidTarget
		}
	case ActionRescuePritunlService:
		if request.Target != control.ComponentPritunl {
			return ErrInvalidTarget
		}
	default:
		return ErrUnknownAction
	}
	return nil
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
