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
	// EventStoreOpened reports one durable store opened, with how long it took.
	EventStoreOpened   EventName = "store_opened"
	EventDaemonStopped EventName = "daemon_stopped"
	// EventConnectivitySnapshot reports the observe-only read model's
	// aggregate. It carries no component detail: the operator surface shows
	// that, and a log line is not a status API.
	EventConnectivitySnapshot EventName = "connectivity_snapshot"

	// EventConnectivityPublication reports whether root took this domain's
	// facts. A publication root refuses costs nothing and must not fail the
	// cycle, but a refusal nobody records is an outage that lasts as long as
	// somebody's attention: the two components this domain speaks for keep
	// whatever they last said while the daemon goes on reporting healthy.
	EventConnectivityPublication EventName = "connectivity_publication"
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
	EventPritunlRescueRefused       EventName = "pritunl_rescue_refused"
	EventPolicyAuthorityUnreadable  EventName = "policy_authority_unreadable"
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
	ReasonInvalidFlags        Reason = "invalid_flags"
	ReasonUnexpectedArguments Reason = "unexpected_arguments"
	ReasonUnauthorizedPeer    Reason = "unauthorized_peer"
	// ReasonMalformedRequest is the last resort, not the usual answer. A
	// rejection that cannot say which check refused it sends the reader looking
	// in the wrong place — this one cost an evening of wrong diagnoses on a
	// machine where nothing was actually broken.
	ReasonMalformedRequest       Reason = "malformed_request"
	ReasonMalformedFrame         Reason = "malformed_frame"
	ReasonInvalidRequestID       Reason = "invalid_request_id"
	ReasonInvalidTarget          Reason = "invalid_target"
	ReasonInvalidPolicyMessage   Reason = "invalid_policy_message"
	ReasonInvalidReconcilerMsg   Reason = "invalid_reconciler_message"
	ReasonInvalidConnectivityMsg Reason = "invalid_connectivity_message"
	ReasonInvalidRescueMessage   Reason = "invalid_rescue_message"
	ReasonConnectivityDomain     Reason = "connectivity_domain_refused"
	ReasonRootInternal           Reason = "root_internal"
	ReasonPublicationTimeout     Reason = "publication_timeout"
	ReasonSocketAbsent           Reason = "socket_absent"
	ReasonSocketDenied           Reason = "socket_denied"
	ReasonPeerSilent             Reason = "peer_did_not_answer"
	// ReasonRecoveryRefused is the other side having looked and disagreed.
	//
	// A refusal is not a failure — it is what the second opinion is for — but a
	// rejected event needs a reason, and without one the attempt to write this
	// down returned an error that ended the observe loop.
	ReasonRecoveryRefused Reason = "recovery_refused"
	// The reasons a request for the one production act can be refused. They are
	// separate because they send the reader to different places: the peer, the
	// signed policy, this runtime's view of the path, and the service itself.
	ReasonUnsignedAuthority Reason = "unsigned_authority"
	ReasonOuterPathNotReady Reason = "outer_path_not_ready"
	ReasonServiceNotStale   Reason = "service_not_stale"
	// Refused above the act, by layers no evaluation reaches. A runtime that
	// may not mutate at all never gets to the act's own checks, and a request
	// no reader took up was never refused by anything — it was not answered.
	// Both used to arrive as a bare precondition failure with nothing written.
	ReasonMutationNotPermitted Reason = "mutation_not_permitted"
	ReasonRequestNotTaken      Reason = "request_not_taken"
	// A candidate configuration that would drop a setting the installed one
	// carries. It is not malformed — that is the point — so it cannot share a
	// reason with a file the daemon cannot parse.
	ReasonConfigurationReduced Reason = "configuration_reduced"
	// A store holding an authority the runtime is not configured to read. It
	// is not the store being unavailable: the store is there and answering.
	ReasonAuthorityUnreadable Reason = "authority_unreadable"
	// What a runtime could not do on its own behalf, as distinct from what
	// another refused it. Unequipped is an authority it was granted and cannot
	// exercise; failed is an attempt that did not work.
	ReasonRecoveryUnequipped Reason = "recovery_unequipped"
	ReasonRecoveryFailed     Reason = "recovery_failed"
	// Where an attempt stopped, as distinct from the fact that it did.
	ReasonCredentialsUnavailable Reason = "recovery_credentials_unavailable"
	ReasonCodeUnavailable        Reason = "recovery_code_unavailable"
	ReasonSessionNotStarted      Reason = "recovery_session_not_started"
	ReasonOversizedRequest       Reason = "oversized_request"
	ReasonUnsupportedAction      Reason = "unsupported_action"
	ReasonUnsupportedVersion     Reason = "unsupported_version"
	ReasonMissingGeneration      Reason = "missing_generation"
	ReasonGenerationConflict     Reason = "generation_conflict"
	ReasonSafetyPolicyViolation  Reason = "safety_policy_violation"
	ReasonInvalidConfiguration   Reason = "invalid_configuration"

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
	// Step and DurationMS say how long a named part of this component's own
	// work took. Both are absent unless something was timed: a step that took
	// no measurable time and a step that was not timed are different claims,
	// and a zero would say the first when the second is true.
	Step       Step  `json:"step,omitempty"`
	DurationMS int64 `json:"duration_ms,omitempty"`
}

// Step names a part of a component's own work that may be timed.
//
// It is a closed vocabulary for the same reason every other field of this
// record is: these logs are collected and this repository is public, and a
// free-text step name is somewhere a path or a hostname arrives by accident.
// The secret guard cannot tell a step name from a leak.
type Step string

const (
	StepReadModel    Step = "read_model"
	StepEventArchive Step = "event_archive"
	StepRootJournal  Step = "root_journal"
	StepUserJournal  Step = "user_journal"
	StepCheckpoints  Step = "checkpoints"
	StepReplay       Step = "replay"
	StepStoresTotal  Step = "stores_total"
)

func validStep(value Step) bool {
	switch value {
	case StepReadModel, StepEventArchive, StepRootJournal, StepUserJournal,
		StepCheckpoints, StepReplay, StepStoresTotal:
		return true
	default:
		return false
	}
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

// EmitTimed records an event together with how long a named step took.
//
// It exists because nothing else in this runtime could report a quantity about
// itself, so every question about how long the daemon's own start took was
// answered by sampling it from outside — and three such answers in one session
// were wrong by factors of two, thirteen and more, because a frame's presence in
// a sample tree was read as its weight.
func (l *Logger) EmitTimed(
	level Level,
	event EventName,
	result Result,
	step Step,
	took time.Duration,
) error {
	if !validStep(step) {
		return errors.New("event contains a non-allowlisted value")
	}
	if took < 0 {
		return errors.New("a step cannot take less than no time")
	}
	milliseconds := took.Milliseconds()
	if milliseconds == 0 {
		// Faster than the unit it is reported in. Saying so is a measurement;
		// omitting the field would say it was never timed.
		milliseconds = 1
	}
	return l.emit(level, event, result, "", step, milliseconds)
}

func (l *Logger) Emit(level Level, event EventName, result Result, reason Reason) error {
	return l.emit(level, event, result, reason, "", 0)
}

func (l *Logger) emit(
	level Level,
	event EventName,
	result Result,
	reason Reason,
	step Step,
	durationMS int64,
) error {
	if l == nil || l.out == nil || !validComponent(l.component) {
		return errors.New("invalid logger")
	}
	if !validLevel(level) || !validEvent(event) || !validResult(result) || !validReason(reason) {
		return errors.New("event contains a non-allowlisted value")
	}
	// A refusal has to explain itself. The reverse — that only a refusal may —
	// was the rule until a degraded outcome needed explaining and could not:
	// being unable to act and acting and failing arrived as the same
	// unexplained degraded cycle, and telling those apart is the difference
	// between a fault in the deployment and a fault in the attempt.
	if result == ResultRejected && reason == "" {
		return errors.New("a rejected event requires a reason")
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
		Step:              step,
		DurationMS:        durationMS,
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
		EventDaemonStarted, EventDaemonStopped, EventStoreOpened,
		EventConnectivitySnapshot,
		EventConnectivityPublication,
		EventReconcilerShadowUnavailable, EventEventArchiveUnavailable,
		EventSentinelRecoveryMonitoring, EventSentinelRecoveryWouldRestart,
		EventSentinelRecoveryVerifying, EventSentinelRecoveryCooldown,
		EventSentinelRecoveryBound,
		EventSentinelPlannerUnavailable,
		EventObservationCycle, EventIngressRoute,
		EventCorporateRoute, EventGitLabHTTPSRoute, EventCodexRoute, EventPritunlReconnect,
		EventPritunlRescueRefused, EventPolicyAuthorityUnreadable,
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
		ReasonPolicyStoreUnavailable, ReasonMalformedFrame, ReasonInvalidRequestID,
		ReasonInvalidTarget, ReasonInvalidPolicyMessage, ReasonInvalidReconcilerMsg,
		ReasonInvalidConnectivityMsg, ReasonConnectivityDomain,
		ReasonInvalidRescueMessage,
		ReasonRootInternal, ReasonPublicationTimeout,
		ReasonSocketAbsent, ReasonSocketDenied, ReasonPeerSilent,
		ReasonRecoveryRefused, ReasonUnsignedAuthority,
		ReasonMutationNotPermitted, ReasonRequestNotTaken,
		ReasonConfigurationReduced, ReasonAuthorityUnreadable,
		ReasonOuterPathNotReady, ReasonServiceNotStale,
		ReasonRecoveryUnequipped, ReasonRecoveryFailed,
		ReasonCredentialsUnavailable, ReasonCodeUnavailable,
		ReasonSessionNotStarted:
		return true
	default:
		return false
	}
}
