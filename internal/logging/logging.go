package logging

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

const Schema = "hexroute.log.v1"

type Component string

const (
	ComponentDaemon   Component = "hexrouted"
	ComponentUser     Component = "hexroute-userd"
	ComponentCLI      Component = "hexroutectl"
	ComponentSentinel Component = "hexroute-sentinel"
	ComponentIngest   Component = "hexroute-ingest"
)

type Level string

const (
	LevelInfo Level = "info"
	LevelWarn Level = "warn"
)

type EventName string

const (
	EventCommandStatus      EventName = "command_status"
	EventStartupCheck       EventName = "startup_check"
	EventVersionRequested   EventName = "version_requested"
	EventArgumentRejected   EventName = "argument_rejected"
	EventIPCRejected        EventName = "ipc_request_rejected"
	EventDaemonStarted      EventName = "daemon_started"
	EventDaemonStopped      EventName = "daemon_stopped"
	EventObservationCycle   EventName = "observation_cycle"
	EventIngressRoute       EventName = "ingress_route_proposed"
	EventCorporateRoute     EventName = "corporate_route_proposed"
	EventGitLabHTTPSRoute   EventName = "gitlab_https_route_proposed"
	EventCodexRoute         EventName = "codex_fallback_route_proposed"
	EventPritunlReconnect   EventName = "pritunl_reconnect_proposed"
	EventSentinelEvidence   EventName = "sentinel_restart_evidence"
	EventLocalNotification  EventName = "local_notification"
	EventCloudAPIStarted    EventName = "cloud_api_started"
	EventCloudAPIStopped    EventName = "cloud_api_stopped"
	EventCloudWorkerStarted EventName = "cloud_worker_started"
	EventCloudWorkerStopped EventName = "cloud_worker_stopped"
	EventCloudHeartbeat     EventName = "cloud_heartbeat"
	EventCloudReconcile     EventName = "cloud_reconcile"
	EventCloudAlertQueue    EventName = "cloud_alert_queue"
	EventCloudAlertDelivery EventName = "cloud_alert_delivery"
	EventCloudRetention     EventName = "cloud_retention"
)

type Result string

const (
	ResultOK        Result = "ok"
	ResultReported  Result = "reported"
	ResultRejected  Result = "rejected"
	ResultSkeleton  Result = "skeleton"
	ResultDegraded  Result = "degraded"
	ResultSuspended Result = "suspended"
	ResultProposed  Result = "proposed"
)

type Reason string

const (
	ReasonInvalidFlags          Reason = "invalid_flags"
	ReasonUnexpectedArguments   Reason = "unexpected_arguments"
	ReasonUnauthorizedPeer      Reason = "unauthorized_peer"
	ReasonMalformedRequest      Reason = "malformed_request"
	ReasonOversizedRequest      Reason = "oversized_request"
	ReasonUnsupportedAction     Reason = "unsupported_action"
	ReasonUnsupportedVersion    Reason = "unsupported_version"
	ReasonMissingGeneration     Reason = "missing_generation"
	ReasonGenerationConflict    Reason = "generation_conflict"
	ReasonSafetyPolicyViolation Reason = "safety_policy_violation"
	ReasonInvalidConfiguration  Reason = "invalid_configuration"
)

type wireEvent struct {
	Schema            string    `json:"schema"`
	Timestamp         time.Time `json:"timestamp"`
	Level             Level     `json:"level"`
	Component         Component `json:"component"`
	Event             EventName `json:"event"`
	Result            Result    `json:"result"`
	Mode              string    `json:"mode"`
	MutationAuthority string    `json:"mutation_authority"`
	Reason            Reason    `json:"reason,omitempty"`
}

type Logger struct {
	mu        sync.Mutex
	out       io.Writer
	component Component
	now       func() time.Time
}

func ParseComponent(value string) (Component, error) {
	component := Component(value)
	if !validComponent(component) {
		return "", errors.New("unsupported component")
	}
	return component, nil
}

func New(out io.Writer, component Component) (*Logger, error) {
	if out == nil {
		return nil, errors.New("log writer is required")
	}
	if !validComponent(component) {
		return nil, errors.New("unsupported component")
	}
	return &Logger{
		out:       out,
		component: component,
		now:       time.Now,
	}, nil
}

func (l *Logger) Emit(level Level, event EventName, result Result, reason Reason) error {
	if l == nil || l.out == nil || !validComponent(l.component) {
		return errors.New("invalid logger")
	}
	if !validLevel(level) || !validEvent(event) || !validResult(result) || !validReason(reason) {
		return errors.New("event contains a non-allowlisted value")
	}
	if (result == ResultRejected) != (reason != "") {
		return errors.New("rejected events require exactly one reason")
	}

	record := wireEvent{
		Schema:            Schema,
		Timestamp:         l.now().UTC(),
		Level:             level,
		Component:         l.component,
		Event:             event,
		Result:            result,
		Mode:              componentMode(l.component),
		MutationAuthority: "none",
		Reason:            reason,
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	return json.NewEncoder(l.out).Encode(record)
}

func validComponent(value Component) bool {
	switch value {
	case ComponentDaemon, ComponentUser, ComponentCLI, ComponentSentinel, ComponentIngest:
		return true
	default:
		return false
	}
}

func validLevel(value Level) bool {
	switch value {
	case LevelInfo, LevelWarn:
		return true
	default:
		return false
	}
}

func validEvent(value EventName) bool {
	switch value {
	case EventCommandStatus, EventStartupCheck, EventVersionRequested, EventArgumentRejected, EventIPCRejected,
		EventDaemonStarted, EventDaemonStopped, EventObservationCycle, EventIngressRoute,
		EventCorporateRoute, EventGitLabHTTPSRoute, EventCodexRoute, EventPritunlReconnect,
		EventSentinelEvidence, EventLocalNotification, EventCloudAPIStarted,
		EventCloudAPIStopped, EventCloudWorkerStarted, EventCloudWorkerStopped,
		EventCloudHeartbeat, EventCloudReconcile, EventCloudAlertQueue,
		EventCloudAlertDelivery, EventCloudRetention:
		return true
	default:
		return false
	}
}

func componentMode(component Component) string {
	if component == ComponentIngest {
		return "telemetry-only"
	}
	return "observe-only"
}

func validResult(value Result) bool {
	switch value {
	case ResultOK, ResultReported, ResultRejected, ResultSkeleton, ResultDegraded,
		ResultSuspended, ResultProposed:
		return true
	default:
		return false
	}
}

func validReason(value Reason) bool {
	switch value {
	case "", ReasonInvalidFlags, ReasonUnexpectedArguments, ReasonUnauthorizedPeer,
		ReasonMalformedRequest, ReasonOversizedRequest, ReasonUnsupportedAction,
		ReasonUnsupportedVersion, ReasonMissingGeneration, ReasonGenerationConflict,
		ReasonSafetyPolicyViolation, ReasonInvalidConfiguration:
		return true
	default:
		return false
	}
}
