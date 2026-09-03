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
	EventCommandStatus    EventName = "command_status"
	EventStartupCheck     EventName = "startup_check"
	EventVersionRequested EventName = "version_requested"
	EventArgumentRejected EventName = "argument_rejected"
	EventIPCRejected      EventName = "ipc_request_rejected"
	EventDaemonStarted    EventName = "daemon_started"
	EventDaemonStopped    EventName = "daemon_stopped"
	// EventConnectivitySnapshot reports the observe-only read model's
	// aggregate. It carries no component detail: the operator surface shows
	// that, and a log line is not a status API.
	EventConnectivitySnapshot EventName = "connectivity_snapshot"
	// EventReconcilerShadowUnavailable reports that the shadow store could
	// not be opened, so its status cannot be answered.
	EventReconcilerShadowUnavailable EventName = "reconciler_shadow_unavailable"
	// EventEventArchiveUnavailable reports that the durable local archive
	// would not open, so this host is retaining nothing it can review later.
	// It is said out loud because a host retaining nothing looks exactly like
	// a host with nothing to retain.
	EventEventArchiveUnavailable EventName = "event_archive_unavailable"
	// The recovery plan is reported as a named event rather than one generic
	// event carrying a payload, because this log has a fixed vocabulary and
	// no free-form fields — deliberately, since a log line is not a status
	// API. A single sentinel_recovery_plan event satisfied that vocabulary
	// and said nothing: the first one written on a real host named neither
	// the phase nor the action, which is what the requirement asks for.
	//
	// Each fires on a change, not every cycle.
	EventSentinelRecoveryMonitoring EventName = "sentinel_recovery_monitoring"
	// EventSentinelRecoveryWouldRestart is the one line worth waking for:
	// the planner selected a restart of the root daemon, and nothing was
	// done because this sentinel holds no means of doing it.
	EventSentinelRecoveryWouldRestart EventName = "sentinel_recovery_would_restart"
	EventSentinelRecoveryVerifying    EventName = "sentinel_recovery_verifying"
	EventSentinelRecoveryCooldown     EventName = "sentinel_recovery_cooldown"
	// EventSentinelRecoveryBound reports the point at which an authorized
	// sentinel would have spent its one permitted attempt and stopped. An
	// observing sentinel has no attempt to spend, so the moment is invisible
	// unless it is written down on its own.
	EventSentinelRecoveryBound EventName = "sentinel_recovery_bound"
	// EventSentinelPlannerUnavailable reports that the planner refused an
	// input. The sentinel keeps watching; what it stops doing is planning,
	// and the difference has to be visible.
	EventSentinelPlannerUnavailable EventName = "sentinel_planner_unavailable"
	EventObservationCycle           EventName = "observation_cycle"
	EventIngressRoute               EventName = "ingress_route_proposed"
	EventCorporateRoute             EventName = "corporate_route_proposed"
	EventGitLabHTTPSRoute           EventName = "gitlab_https_route_proposed"
	EventCodexRoute                 EventName = "codex_fallback_route_proposed"
	EventPritunlReconnect           EventName = "pritunl_reconnect_proposed"
	EventSentinelEvidence           EventName = "sentinel_restart_evidence"
	EventLocalNotification          EventName = "local_notification"
	EventCloudAPIStarted            EventName = "cloud_api_started"
	EventCloudAPIStopped            EventName = "cloud_api_stopped"
	EventCloudWorkerStarted         EventName = "cloud_worker_started"
	EventCloudWorkerStopped         EventName = "cloud_worker_stopped"
	EventCloudMigration             EventName = "cloud_migration"
	EventCloudHeartbeat             EventName = "cloud_heartbeat"
	EventCloudReconcile             EventName = "cloud_reconcile"
	EventCloudAlertQueue            EventName = "cloud_alert_queue"
	EventCloudAlertDelivery         EventName = "cloud_alert_delivery"
	EventCloudRetention             EventName = "cloud_retention"
	// EventCloudConnectivity names the pass that folds uploaded connectivity
	// projections into the cloud read model.
	EventCloudConnectivity EventName = "cloud_connectivity_projection"
	// EventCloudSLO names the pass that measures availability over closed
	// windows from evidence already stored.
	EventCloudSLO EventName = "cloud_slo"
	// EventCloudIncidentBundle names the pass that assembles evidence for
	// closed incidents that have never been bundled, and acts on bundles that
	// have reached their recorded expiry.
	EventCloudIncidentBundle EventName = "cloud_incident_bundle"
	// EventCloudIncidentBundleUnconfigured names the same pass reached by a
	// deployment that was never given storage to put a bundle in. A record
	// has to be a name here, because a log record carries a fixed field set
	// and cannot say in a field what it did not do. Without this name, a
	// deployment that was never finished and one with nothing to bundle
	// produce identical logs: silence.
	EventCloudIncidentBundleUnconfigured EventName = "cloud_incident_bundle_unconfigured"
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

	// The reasons below name the subsystem a startup refused on. They exist
	// because a daemon under KeepAlive that reports one reason for every
	// failure leaves whoever installed it bisecting by hand: the config, the
	// heartbeat, the read model, the qualification chain, the operator socket
	// and the policy store all refuse for different causes and all read the
	// same in a log.
	//
	// They name a subsystem and nothing else. No path, no identity, no value:
	// the vocabulary stays a closed allowlist, and knowing which door was shut
	// is not the same as knowing where it is.
	ReasonHeartbeatUnavailable     Reason = "heartbeat_unavailable"
	ReasonReadModelUnavailable     Reason = "read_model_unavailable"
	ReasonQualificationUnavailable Reason = "qualification_unavailable"
	ReasonSocketUnavailable        Reason = "socket_unavailable"
	ReasonPolicyStoreUnavailable   Reason = "policy_store_unavailable"
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
		EventDaemonStarted, EventDaemonStopped, EventConnectivitySnapshot,
		EventReconcilerShadowUnavailable, EventEventArchiveUnavailable,
		EventSentinelRecoveryMonitoring, EventSentinelRecoveryWouldRestart,
		EventSentinelRecoveryVerifying, EventSentinelRecoveryCooldown,
		EventSentinelRecoveryBound,
		EventSentinelPlannerUnavailable,
		EventObservationCycle, EventIngressRoute,
		EventCorporateRoute, EventGitLabHTTPSRoute, EventCodexRoute, EventPritunlReconnect,
		EventSentinelEvidence, EventLocalNotification, EventCloudAPIStarted,
		EventCloudAPIStopped, EventCloudWorkerStarted, EventCloudWorkerStopped,
		EventCloudMigration,
		EventCloudHeartbeat, EventCloudReconcile, EventCloudAlertQueue,
		EventCloudAlertDelivery, EventCloudRetention, EventCloudConnectivity,
		EventCloudSLO, EventCloudIncidentBundle,
		EventCloudIncidentBundleUnconfigured:
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
		ReasonSafetyPolicyViolation, ReasonInvalidConfiguration,
		ReasonHeartbeatUnavailable, ReasonReadModelUnavailable,
		ReasonQualificationUnavailable, ReasonSocketUnavailable,
		ReasonPolicyStoreUnavailable:
		return true
	default:
		return false
	}
}
