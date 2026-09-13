package event

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	SchemaVersion        = 1
	MaxReferenceBytes    = 64
	MaxEncodedEventBytes = 8 * 1024
	MaxPayloadBytes      = 4 * 1024
)

type Schema string

const (
	SchemaObservation   Schema = "component.observation"
	SchemaTransition    Schema = "state.transition"
	SchemaAction        Schema = "recovery.action"
	SchemaIncident      Schema = "incident.lifecycle"
	SchemaDeployment    Schema = "deployment.lifecycle"
	SchemaConfigVersion Schema = "config.lifecycle"
	SchemaDiagnostic    Schema = "runtime.diagnostic"
	SchemaSleep         Schema = "node.sleep"
	// SchemaTunnelDecision is what a tunnel owner would have done. It is
	// recorded while another runtime owns the tunnel, so that the decision can
	// be compared against what that runtime actually did — a decision that
	// agreed and a decision never reached look the same in an empty log.
	SchemaTunnelDecision Schema = "tunnel.decision"
	SchemaPolicy         Schema = "policy.lifecycle"

	// A baseline restates a component in full and is what clears a gap, so
	// it is retained ahead of ordinary observations.
	SchemaConnectivityBaseline    Schema = "connectivity.baseline"
	SchemaConnectivityObservation Schema = "connectivity.observation"
	// The projection is what leaves the host. It is operational rather than
	// critical: losing one costs a sample, not evidence.
	SchemaConnectivityProjection Schema = "connectivity.projection"
	// An overflow says the local archive dropped something and what. It is
	// critical because it is the only record of an absence: evicting it would
	// make a bounded archive indistinguishable from one that was never full.
	SchemaArchiveOverflow Schema = "archive.overflow"
)

type Priority string

const (
	PriorityCritical    Priority = "critical"
	PriorityOperational Priority = "operational"
	PriorityDiagnostic  Priority = "diagnostic"
)

type Definition struct {
	Schema          Schema
	Version         uint16
	Priority        Priority
	MaxPayloadBytes int
}

type Record struct {
	Schema   Schema   `json:"schema"`
	Version  uint16   `json:"version"`
	Priority Priority `json:"priority"`
	Payload  any      `json:"payload"`
}

type wireRecord struct {
	Schema   Schema          `json:"schema"`
	Version  uint16          `json:"version"`
	Priority Priority        `json:"priority"`
	Payload  json.RawMessage `json:"payload"`
}

type Observation struct {
	Component           control.Component `json:"component"`
	Health              control.Health    `json:"health"`
	Reason              control.Reason    `json:"reason"`
	ConsecutiveFailures uint32            `json:"consecutive_failures"`
}

type Transition struct {
	Component  control.Component `json:"component"`
	From       control.State     `json:"from"`
	To         control.State     `json:"to"`
	Reason     control.Reason    `json:"reason"`
	Generation uint64            `json:"generation"`
}

type ActionOutcome string

const (
	ActionPlanned    ActionOutcome = "planned"
	ActionExecuted   ActionOutcome = "executed"
	ActionVerified   ActionOutcome = "verified"
	ActionFailed     ActionOutcome = "failed"
	ActionSuppressed ActionOutcome = "suppressed"
)

type Action struct {
	Kind       control.ActionKind   `json:"kind"`
	Target     control.ActionTarget `json:"target"`
	Outcome    ActionOutcome        `json:"outcome"`
	Attempt    uint32               `json:"attempt"`
	Generation uint64               `json:"generation"`
	Reason     control.Reason       `json:"reason"`
}

type IncidentStatus string

const (
	IncidentOpened   IncidentStatus = "opened"
	IncidentUpdated  IncidentStatus = "updated"
	IncidentResolved IncidentStatus = "resolved"
)

type IncidentSeverity string

const (
	SeverityInfo     IncidentSeverity = "info"
	SeverityWarning  IncidentSeverity = "warning"
	SeverityCritical IncidentSeverity = "critical"
)

type IncidentCategory string

const (
	IncidentAvailability       IncidentCategory = "availability"
	IncidentRecoveryBudget     IncidentCategory = "recovery_budget"
	IncidentSpoolOverflow      IncidentCategory = "spool_overflow"
	IncidentSecurityValidation IncidentCategory = "security_validation"
	IncidentDeployment         IncidentCategory = "deployment"
	// IncidentPolicyExpiry is a deadline, not a failure. It has its own
	// category because the alternative was borrowing IncidentSecurityValidation,
	// which would tell the operator a security check failed when nothing failed.
	IncidentPolicyExpiry IncidentCategory = "policy_expiry"
)

type Incident struct {
	IncidentID string            `json:"incident_id"`
	Status     IncidentStatus    `json:"status"`
	Severity   IncidentSeverity  `json:"severity"`
	Category   IncidentCategory  `json:"category"`
	Component  control.Component `json:"component"`
	Generation uint64            `json:"generation"`
}

type DeploymentStatus string

const (
	DeploymentStaged     DeploymentStatus = "staged"
	DeploymentActivated  DeploymentStatus = "activated"
	DeploymentRolledBack DeploymentStatus = "rolled_back"
	DeploymentRejected   DeploymentStatus = "rejected"
)

type Deployment struct {
	DeploymentID string           `json:"deployment_id"`
	Release      string           `json:"release"`
	Status       DeploymentStatus `json:"status"`
	DigestSHA256 string           `json:"digest_sha256"`
}

type ConfigStatus string

const (
	ConfigStaged     ConfigStatus = "staged"
	ConfigActivated  ConfigStatus = "activated"
	ConfigRolledBack ConfigStatus = "rolled_back"
	ConfigRejected   ConfigStatus = "rejected"
)

type ConfigVersion struct {
	ConfigID     string       `json:"config_id"`
	Version      string       `json:"config_version"`
	Status       ConfigStatus `json:"status"`
	DigestSHA256 string       `json:"digest_sha256"`
}

type DiagnosticCode string

const (
	DiagnosticAdapterSampled            DiagnosticCode = "adapter_sampled"
	DiagnosticRetryScheduled            DiagnosticCode = "retry_scheduled"
	DiagnosticTelemetryGapUnrecoverable DiagnosticCode = "telemetry_gap_unrecoverable"
	DiagnosticUploadDeferred            DiagnosticCode = "upload_deferred"
	// DiagnosticArchiveRefusedRecord says the local archive was offered
	// something no registered schema describes. It carries a count because a
	// source producing one malformed record usually produces many, and a
	// diagnostic per refusal would let the failure evict the evidence.
	DiagnosticArchiveRefusedRecord DiagnosticCode = "archive_refused_record"
)

// ArchiveOverflowReason says which bound removed the records.
type ArchiveOverflowReason string

const (
	// ArchiveOverflowSize is eviction to stay inside the configured size.
	ArchiveOverflowSize ArchiveOverflowReason = "size"
	// ArchiveOverflowAge is eviction of records outside the configured window.
	ArchiveOverflowAge ArchiveOverflowReason = "age"
	// ArchiveOverflowRefused is an append refused because the only records
	// left to evict were critical. Nothing was dropped; something was not
	// accepted, and the two are different answers to the same bound.
	ArchiveOverflowRefused ArchiveOverflowReason = "refused"
)

// ArchiveOverflow names what the local archive dropped and where it sat.
//
// The sequence range is what makes the absence legible afterwards: a reader
// who finds records 100 and 140 has no way to tell an idle hour from an
// eviction without a record saying 101 through 139 were removed.
type ArchiveOverflow struct {
	Reason ArchiveOverflowReason `json:"reason"`
	// Dropped is the priority class removed, or the class refused when the
	// reason is refusal.
	Dropped       Priority `json:"dropped_priority"`
	FirstSequence uint64   `json:"first_sequence"`
	LastSequence  uint64   `json:"last_sequence"`
	Count         uint32   `json:"count"`
}

type Diagnostic struct {
	Component  control.Component `json:"component"`
	Code       DiagnosticCode    `json:"code"`
	Count      uint32            `json:"count"`
	DurationMS uint32            `json:"duration_ms"`
}

type SleepPhase string

const (
	SleepStarted SleepPhase = "started"
	SleepEnded   SleepPhase = "ended"
)

type SleepReason string

const (
	SleepReasonLidClosed   SleepReason = "lid_closed"
	SleepReasonSystemSleep SleepReason = "system_sleep"
	SleepReasonFullWake    SleepReason = "full_wake"
)

// TunnelDecision is one cycle's answer to what a tunnel owner would do.
//
// Every cause that held is named, because the runtime this is compared against
// reports one reason: recording only the first would make an honest
// disagreement look like a wrong decision.
type TunnelDecision struct {
	Action string   `json:"action"`
	Causes []string `json:"causes,omitempty"`
	// Grounds is what the causes were reached from. A decision naming a cause
	// and nothing behind it reads as convincingly when it is wrong as when it
	// is right, and reading one such record took a second store laid beside it
	// and matched on time.
	Grounds *TunnelGrounds `json:"grounds,omitempty"`
	// Authorization is whether this runtime would have been permitted to do
	// what it decided. Deciding and being allowed are different questions, and
	// a record answering only the first cannot show the second was ever asked.
	//
	// Absent on cycles that decided to do nothing: there was nothing to be
	// permitted.
	Authorization *TunnelAuthorization `json:"authorization,omitempty"`
}

// TunnelAuthorization is the policy handler's answer, in its own vocabulary.
type TunnelAuthorization struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// TunnelGrounds is the observation behind each cause that can hold.
//
// What carries the configured destinations appears as a digest and a count and
// never as the destinations themselves, on the same terms as every other
// projection here: how many there are and whether they changed, never which.
//
// The fields an incomplete cycle could not observe are pointers, because a
// cycle that saw none and a cycle that saw nothing are different claims and a
// zero would say the first.
type TunnelGrounds struct {
	Complete        bool    `json:"complete"`
	ProcessRunning  bool    `json:"process_running"`
	SleptMS         int64   `json:"slept_ms"`
	CarrierDigest   string  `json:"carrier_digest,omitempty"`
	CarrierEntries  *int    `json:"carrier_entries,omitempty"`
	LinkPresent     bool    `json:"link_present"`
	LinkFailures    uint32  `json:"link_failures"`
	PayloadOK       bool    `json:"payload_ok"`
	PayloadFailures uint32  `json:"payload_failures"`
	RoutesPlanned   *uint16 `json:"routes_planned,omitempty"`
}

type Sleep struct {
	Phase  SleepPhase  `json:"phase"`
	Reason SleepReason `json:"reason"`
}

// PolicyLifecycle is the complete allowlisted policy projection accepted by
// local journals and cloud telemetry. It deliberately has no selector, source,
// lease, endpoint, credential or arbitrary detail field.
type PolicyLifecycle struct {
	Status                  policy.Status                  `json:"status"`
	AuthorizationSuspension policy.AuthorizationSuspension `json:"authorization_suspension"`
	ExistingState           *policy.ExistingStateStatus    `json:"existing_state,omitempty"`
}

var (
	ErrUnknownSchema      = errors.New("unknown event schema")
	ErrUnsupportedVersion = errors.New("unsupported event schema version")
	ErrPriorityMismatch   = errors.New("event priority does not match schema")
	ErrInvalidField       = errors.New("invalid event field")
	ErrPayloadTooLarge    = errors.New("event payload exceeds size limit")
	ErrEventTooLarge      = errors.New("encoded event exceeds size limit")
	ErrMalformedEvent     = errors.New("malformed event")
	ErrPayloadType        = errors.New("event payload type does not match schema")
)

func DefinitionFor(schema Schema) (Definition, bool) {
	var priority Priority
	switch schema {
	case SchemaObservation, SchemaConnectivityObservation, SchemaConnectivityProjection:
		priority = PriorityOperational
	case SchemaDiagnostic:
		priority = PriorityDiagnostic
	case SchemaTransition, SchemaAction, SchemaIncident, SchemaDeployment,
		SchemaConfigVersion, SchemaSleep, SchemaPolicy, SchemaConnectivityBaseline,
		SchemaArchiveOverflow:
		priority = PriorityCritical
	case SchemaTunnelDecision:
		// Operational rather than critical: it is a record of what this runtime
		// would have done while it owns nothing, and losing the oldest of them
		// to the archive's bound costs a comparison rather than an account of
		// something that happened.
		priority = PriorityOperational
	default:
		return Definition{}, false
	}
	maximum := MaxPayloadBytes
	if schema == SchemaConnectivityBaseline || schema == SchemaConnectivityObservation {
		maximum = MaxConnectivityPayloadBytes
	}
	return Definition{
		Schema:          schema,
		Version:         SchemaVersion,
		Priority:        priority,
		MaxPayloadBytes: maximum,
	}, true
}

func Encode(schema Schema, payload any) ([]byte, error) {
	definition, ok := DefinitionFor(schema)
	if !ok {
		return nil, ErrUnknownSchema
	}
	if err := validatePayload(schema, payload); err != nil {
		return nil, err
	}

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedEvent, err)
	}
	if len(encodedPayload) > definition.MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	encoded, err := json.Marshal(wireRecord{
		Schema:   schema,
		Version:  definition.Version,
		Priority: definition.Priority,
		Payload:  encodedPayload,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedEvent, err)
	}
	if len(encoded) > MaxEncodedEventBytes {
		return nil, ErrEventTooLarge
	}
	return encoded, nil
}

func Decode(data []byte) (Record, error) {
	if len(data) > MaxEncodedEventBytes {
		return Record{}, ErrEventTooLarge
	}

	var wire wireRecord
	if err := decodeStrict(data, &wire); err != nil {
		return Record{}, err
	}
	definition, ok := DefinitionFor(wire.Schema)
	if !ok {
		return Record{}, ErrUnknownSchema
	}
	if wire.Version != definition.Version {
		return Record{}, ErrUnsupportedVersion
	}
	if wire.Priority != definition.Priority {
		return Record{}, ErrPriorityMismatch
	}
	if len(wire.Payload) == 0 || len(wire.Payload) > definition.MaxPayloadBytes {
		return Record{}, ErrPayloadTooLarge
	}

	payload := newPayload(wire.Schema)
	if err := decodeStrict(wire.Payload, payload); err != nil {
		return Record{}, err
	}
	if err := validatePayload(wire.Schema, payload); err != nil {
		return Record{}, err
	}
	return Record{
		Schema:   wire.Schema,
		Version:  wire.Version,
		Priority: wire.Priority,
		Payload:  payload,
	}, nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrMalformedEvent
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrMalformedEvent
	}
	return nil
}

func newPayload(schema Schema) any {
	switch schema {
	case SchemaObservation:
		return &Observation{}
	case SchemaTransition:
		return &Transition{}
	case SchemaAction:
		return &Action{}
	case SchemaIncident:
		return &Incident{}
	case SchemaDeployment:
		return &Deployment{}
	case SchemaConfigVersion:
		return &ConfigVersion{}
	case SchemaDiagnostic:
		return &Diagnostic{}
	case SchemaSleep:
		return &Sleep{}
	case SchemaTunnelDecision:
		return &TunnelDecision{}
	case SchemaPolicy:
		return &PolicyLifecycle{}
	case SchemaConnectivityBaseline, SchemaConnectivityObservation:
		return &ConnectivityFact{}
	case SchemaArchiveOverflow:
		return &ArchiveOverflow{}
	case SchemaConnectivityProjection:
		return &ConnectivityProjection{}
	default:
		return nil
	}
}

func validatePayload(schema Schema, payload any) error {
	switch schema {
	case SchemaConnectivityBaseline:
		return validateConnectivityFact(payload, true)
	case SchemaConnectivityObservation:
		return validateConnectivityFact(payload, false)
	case SchemaConnectivityProjection:
		return validateConnectivityProjection(payload)
	case SchemaObservation:
		value, ok := asObservation(payload)
		if !ok || !validComponent(value.Component) || !validHealth(value.Health) ||
			!validReason(value.Reason) {
			return ErrInvalidField
		}
	case SchemaTransition:
		value, ok := asTransition(payload)
		if !ok || !validComponent(value.Component) || !value.From.Valid() || !value.To.Valid() ||
			!validReason(value.Reason) || value.Generation == 0 {
			return ErrInvalidField
		}
	case SchemaAction:
		value, ok := asAction(payload)
		if !ok || !validActionKind(value.Kind) || !validActionTarget(value.Target) ||
			!validActionOutcome(value.Outcome) || !validReason(value.Reason) ||
			value.Attempt == 0 || value.Generation == 0 {
			return ErrInvalidField
		}
	case SchemaIncident:
		value, ok := asIncident(payload)
		if !ok || !validReference(value.IncidentID) || !validIncidentStatus(value.Status) ||
			!validSeverity(value.Severity) || !validCategory(value.Category) ||
			!validComponent(value.Component) || value.Generation == 0 {
			return ErrInvalidField
		}
	case SchemaDeployment:
		value, ok := asDeployment(payload)
		if !ok || !validReference(value.DeploymentID) || !validReference(value.Release) ||
			!validDeploymentStatus(value.Status) || !validDigest(value.DigestSHA256) {
			return ErrInvalidField
		}
	case SchemaConfigVersion:
		value, ok := asConfigVersion(payload)
		if !ok || !validReference(value.ConfigID) || !validReference(value.Version) ||
			!validConfigStatus(value.Status) || !validDigest(value.DigestSHA256) {
			return ErrInvalidField
		}
	case SchemaDiagnostic:
		value, ok := asDiagnostic(payload)
		if !ok || !validComponent(value.Component) || !validDiagnosticCode(value.Code) {
			return ErrInvalidField
		}
	case SchemaArchiveOverflow:
		value, ok := asArchiveOverflow(payload)
		if !ok || !validArchiveOverflow(value) {
			return ErrInvalidField
		}
	case SchemaSleep:
		value, ok := asSleep(payload)
		if !ok || !validSleep(value) {
			return ErrInvalidField
		}
	case SchemaTunnelDecision:
		value, ok := asTunnelDecision(payload)
		if !ok || !validTunnelDecision(value) {
			return ErrInvalidField
		}
	case SchemaPolicy:
		value, ok := asPolicyLifecycle(payload)
		if !ok || value.Status.Validate() != nil ||
			value.AuthorizationSuspension.Validate() != nil {
			return ErrInvalidField
		}
		if value.ExistingState != nil &&
			(value.ExistingState.Validate() != nil ||
				value.ExistingState.Domain != value.Status.Domain ||
				value.Status.State == policy.PolicyNone) {
			return ErrInvalidField
		}
	default:
		return ErrUnknownSchema
	}
	return nil
}

func validReference(value string) bool {
	if value == "" || len(value) > MaxReferenceBytes {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		if index > 0 && strings.ContainsRune(".:-", character) {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validComponent(value control.Component) bool {
	switch value {
	case control.ComponentNetwork, control.ComponentTunnel, control.ComponentRoutes,
		control.ComponentPritunl, control.ComponentCodex, control.ComponentTelegram,
		control.ComponentRuntime:
		return true
	default:
		return false
	}
}

func validHealth(value control.Health) bool {
	switch value {
	case control.HealthUnknown, control.HealthReady, control.HealthFailed, control.HealthSuspended:
		return true
	default:
		return false
	}
}

func validReason(value control.Reason) bool {
	switch value {
	case control.ReasonNone, control.ReasonProbeSucceeded, control.ReasonProbeFailed,
		control.ReasonFailureThreshold, control.ReasonRecoveryAllowed,
		control.ReasonRecoveryBudget, control.ReasonVerificationPassed,
		control.ReasonCooldownElapsed, control.ReasonDependenciesReady,
		control.ReasonDependenciesNotReady, control.ReasonIntentionalSleep,
		control.ReasonOperatorResume:
		return true
	default:
		return false
	}
}

func validActionKind(value control.ActionKind) bool {
	switch value {
	case control.ActionRestart, control.ActionApplyScopedRoutes, control.ActionSelectIngress:
		return true
	default:
		return false
	}
}

func validActionTarget(value control.ActionTarget) bool {
	switch value {
	case control.TargetSingBox, control.TargetScopedRoutes, control.TargetIngress,
		control.TargetPritunlService:
		return true
	default:
		return false
	}
}

func validActionOutcome(value ActionOutcome) bool {
	switch value {
	case ActionPlanned, ActionExecuted, ActionVerified, ActionFailed, ActionSuppressed:
		return true
	default:
		return false
	}
}

func validIncidentStatus(value IncidentStatus) bool {
	switch value {
	case IncidentOpened, IncidentUpdated, IncidentResolved:
		return true
	default:
		return false
	}
}

func validSeverity(value IncidentSeverity) bool {
	switch value {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

func validCategory(value IncidentCategory) bool {
	switch value {
	case IncidentAvailability, IncidentRecoveryBudget, IncidentSpoolOverflow,
		IncidentSecurityValidation, IncidentDeployment, IncidentPolicyExpiry:
		return true
	default:
		return false
	}
}

func validDeploymentStatus(value DeploymentStatus) bool {
	switch value {
	case DeploymentStaged, DeploymentActivated, DeploymentRolledBack, DeploymentRejected:
		return true
	default:
		return false
	}
}

func validConfigStatus(value ConfigStatus) bool {
	switch value {
	case ConfigStaged, ConfigActivated, ConfigRolledBack, ConfigRejected:
		return true
	default:
		return false
	}
}

func validDiagnosticCode(value DiagnosticCode) bool {
	switch value {
	case DiagnosticAdapterSampled, DiagnosticRetryScheduled,
		DiagnosticTelemetryGapUnrecoverable, DiagnosticUploadDeferred,
		DiagnosticArchiveRefusedRecord:
		return true
	default:
		return false
	}
}

func validSleep(value Sleep) bool {
	switch value.Phase {
	case SleepStarted:
		return value.Reason == SleepReasonLidClosed ||
			value.Reason == SleepReasonSystemSleep
	case SleepEnded:
		return value.Reason == SleepReasonFullWake
	default:
		return false
	}
}

func asObservation(payload any) (Observation, bool) {
	switch value := payload.(type) {
	case Observation:
		return value, true
	case *Observation:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Observation{}, false
}

func asTransition(payload any) (Transition, bool) {
	switch value := payload.(type) {
	case Transition:
		return value, true
	case *Transition:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Transition{}, false
}

func asAction(payload any) (Action, bool) {
	switch value := payload.(type) {
	case Action:
		return value, true
	case *Action:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Action{}, false
}

func asIncident(payload any) (Incident, bool) {
	switch value := payload.(type) {
	case Incident:
		return value, true
	case *Incident:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Incident{}, false
}

func asDeployment(payload any) (Deployment, bool) {
	switch value := payload.(type) {
	case Deployment:
		return value, true
	case *Deployment:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Deployment{}, false
}

func asConfigVersion(payload any) (ConfigVersion, bool) {
	switch value := payload.(type) {
	case ConfigVersion:
		return value, true
	case *ConfigVersion:
		if value != nil {
			return *value, true
		}
	default:
	}
	return ConfigVersion{}, false
}

func asPolicyLifecycle(payload any) (PolicyLifecycle, bool) {
	switch value := payload.(type) {
	case PolicyLifecycle:
		return value, true
	case *PolicyLifecycle:
		if value != nil {
			return *value, true
		}
	}
	return PolicyLifecycle{}, false
}

func asArchiveOverflow(payload any) (ArchiveOverflow, bool) {
	switch value := payload.(type) {
	case ArchiveOverflow:
		return value, true
	case *ArchiveOverflow:
		if value != nil {
			return *value, true
		}
	}
	return ArchiveOverflow{}, false
}

// A refusal covers no range: nothing was removed, so naming one would claim
// records had been dropped that are still there.
func validArchiveOverflow(value ArchiveOverflow) bool {
	switch value.Dropped {
	case PriorityCritical, PriorityOperational, PriorityDiagnostic:
	default:
		return false
	}
	switch value.Reason {
	case ArchiveOverflowSize, ArchiveOverflowAge:
		return value.Count > 0 && value.FirstSequence > 0 &&
			value.LastSequence >= value.FirstSequence &&
			uint64(value.Count) <= value.LastSequence-value.FirstSequence+1
	case ArchiveOverflowRefused:
		return value.Count == 0 && value.FirstSequence == 0 && value.LastSequence == 0
	default:
		return false
	}
}

func asDiagnostic(payload any) (Diagnostic, bool) {
	switch value := payload.(type) {
	case Diagnostic:
		return value, true
	case *Diagnostic:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Diagnostic{}, false
}

func asSleep(payload any) (Sleep, bool) {
	switch value := payload.(type) {
	case Sleep:
		return value, true
	case *Sleep:
		if value != nil {
			return *value, true
		}
	default:
	}
	return Sleep{}, false
}

func asTunnelDecision(payload any) (TunnelDecision, bool) {
	switch value := payload.(type) {
	case TunnelDecision:
		return value, true
	case *TunnelDecision:
		if value == nil {
			return TunnelDecision{}, false
		}
		return *value, true
	default:
		return TunnelDecision{}, false
	}
}

// validTunnelDecision keeps the vocabulary closed. The actions and causes are a
// fixed set decided by a planner, and free text here would carry whatever a
// future planner happened to name something.
func validTunnelDecision(value TunnelDecision) bool {
	switch value.Action {
	case "none", "rebuild_tunnel", "reapply_routes":
	default:
		return false
	}
	if len(value.Causes) > 6 {
		return false
	}
	seen := make(map[string]struct{}, len(value.Causes))
	for _, cause := range value.Causes {
		switch cause {
		case "process_gone", "wake_gap", "carrier_changed",
			"link_returned", "payload_failed", "routes_drifted":
		default:
			return false
		}
		if _, duplicate := seen[cause]; duplicate {
			return false
		}
		seen[cause] = struct{}{}
	}
	if value.Action == "none" && len(value.Causes) > 0 {
		return false
	}
	if !validTunnelAuthorization(value.Authorization) {
		return false
	}
	if value.Action == "none" && value.Authorization != nil {
		return false
	}
	return validTunnelGrounds(value.Grounds)
}

// validTunnelAuthorization keeps the handler's vocabulary closed here too.
//
// The reasons are the policy model's own, and copying them into a free string
// would let this record say something the handler cannot.
func validTunnelAuthorization(authorization *TunnelAuthorization) bool {
	if authorization == nil {
		return true
	}
	switch authorization.Reason {
	case "authorized", "invalid_request", "inactive_policy",
		"authorization_suspended", "domain_mismatch", "generation_mismatch",
		"selector_mismatch", "authorization_lease_inactive", "explicitly_denied":
	default:
		return false
	}
	// Allowed and its reason are one statement. A refusal that says authorized,
	// or an authorization that names a refusal, is a record nobody can read.
	return authorization.Allowed == (authorization.Reason == "authorized")
}

// validTunnelGrounds refuses grounds that cannot be read.
//
// The digest is a fixed-width hex identity or absent. Anything longer is a
// signature arriving where a digest belongs, which is how the destinations this
// projection exists to keep out would get in.
func validTunnelGrounds(grounds *TunnelGrounds) bool {
	if grounds == nil {
		return true
	}
	if grounds.SleptMS < 0 {
		return false
	}
	if grounds.CarrierDigest != "" {
		if len(grounds.CarrierDigest) != tunnelCarrierDigestLength {
			return false
		}
		for _, letter := range grounds.CarrierDigest {
			if (letter < '0' || letter > '9') && (letter < 'a' || letter > 'f') {
				return false
			}
		}
	}
	if grounds.CarrierEntries != nil && *grounds.CarrierEntries < 0 {
		return false
	}
	// A cycle that saw nothing has nothing to report about what it saw, and a
	// cycle that completed has both. Reporting one without the other says a
	// cycle both did and did not observe.
	if grounds.Complete != (grounds.CarrierEntries != nil) {
		return false
	}
	if grounds.Complete != (grounds.RoutesPlanned != nil) {
		return false
	}
	return true
}

// tunnelCarrierDigestLength is what Signature.Digest emits: enough to tell two
// carriers apart, not enough to be a handle on either.
const tunnelCarrierDigestLength = 12
