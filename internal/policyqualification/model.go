package policyqualification

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	RecordSchema            = "hexroute.policy-qualification-evidence.v1"
	MinimumEligibleDuration = 72 * time.Hour
	MaximumEligibleWindow   = 7 * 24 * time.Hour
	MaximumSourceReferences = 8
	MaximumRecordBytes      = 16 * 1024
	MaximumRecords          = 100_000
	ChainFilename           = "qualification.jsonl"
)

type Kind string

const (
	KindEligibleWindow   Kind = "eligible_window"
	KindSleepWake        Kind = "sleep_wake"
	KindReboot           Kind = "reboot"
	KindInvalidSignature Kind = "invalid_signature"
	KindSelectorConflict Kind = "selector_conflict"
	KindStaleGeneration  Kind = "stale_generation"
	KindCrossDomainCrash Kind = "cross_domain_crash"
	KindSafetyComparison Kind = "safety_comparison"
)

type Result string

const (
	ResultPassed Result = "passed"
	ResultFailed Result = "failed"
)

type Reason string

const (
	ReasonNone            Reason = "none"
	ReasonSourceInvalid   Reason = "source_invalid"
	ReasonTimingGap       Reason = "timing_gap"
	ReasonInjectionFailed Reason = "injection_failed"
	ReasonSafetyMismatch  Reason = "safety_mismatch"
)

type FaultOutcome string

const (
	OutcomeCandidateRejected     FaultOutcome = "candidate_rejected"
	OutcomeCandidateAccepted     FaultOutcome = "candidate_accepted"
	OutcomeMutationRejected      FaultOutcome = "mutation_rejected"
	OutcomeMutationAccepted      FaultOutcome = "mutation_accepted"
	OutcomeDomainMismatchBlocked FaultOutcome = "domain_mismatch_blocked"
	OutcomeDomainMismatchOpen    FaultOutcome = "domain_mismatch_open"
)

type Binding struct {
	SessionID            metadata.UUID `json:"session_id"`
	BundleGeneration     uint64        `json:"bundle_generation"`
	RootPolicyGeneration uint64        `json:"root_policy_generation"`
	UserPolicyGeneration uint64        `json:"user_policy_generation"`
	ManifestSHA256       string        `json:"manifest_sha256"`
}

type SourceReference struct {
	EventID metadata.UUID `json:"event_id"`
	SHA256  string        `json:"sha256"`
}

type Observation struct {
	BootID            metadata.UUID
	Sources           []SourceReference
	ObservedAt        string
	SourceMonotonicNS int64
	Result            Result
	Reason            Reason
}

type EligibleWindow struct {
	StartedAt          string `json:"started_at"`
	EndedAt            string `json:"ended_at"`
	StartedMonotonicNS int64  `json:"started_monotonic_ns"`
	EndedMonotonicNS   int64  `json:"ended_monotonic_ns"`
}

type SleepWake struct {
	SleptAt          string `json:"slept_at"`
	WokeAt           string `json:"woke_at"`
	SleptMonotonicNS int64  `json:"slept_monotonic_ns"`
	WokeMonotonicNS  int64  `json:"woke_monotonic_ns"`
}

type Reboot struct {
	PreviousBootID metadata.UUID `json:"previous_boot_id"`
	CurrentBootID  metadata.UUID `json:"current_boot_id"`
	ObservedAt     string        `json:"observed_at"`
}

type FaultInjection struct {
	Outcome FaultOutcome `json:"outcome"`
}

type SafetyComparison struct {
	ExpectedSHA256 string `json:"expected_sha256"`
	ObservedSHA256 string `json:"observed_sha256"`
}

type EvidenceRecord struct {
	Schema            string            `json:"schema"`
	RecordID          metadata.UUID     `json:"record_id"`
	Sequence          uint64            `json:"sequence"`
	PreviousSHA256    string            `json:"previous_sha256,omitempty"`
	Kind              Kind              `json:"kind"`
	Binding           Binding           `json:"binding"`
	BootID            metadata.UUID     `json:"boot_id"`
	Sources           []SourceReference `json:"sources"`
	ObservedAt        string            `json:"observed_at"`
	SourceMonotonicNS int64             `json:"source_monotonic_ns"`
	Result            Result            `json:"result"`
	Reason            Reason            `json:"reason"`
	EligibleWindow    *EligibleWindow   `json:"eligible_window,omitempty"`
	SleepWake         *SleepWake        `json:"sleep_wake,omitempty"`
	Reboot            *Reboot           `json:"reboot,omitempty"`
	FaultInjection    *FaultInjection   `json:"fault_injection,omitempty"`
	SafetyComparison  *SafetyComparison `json:"safety_comparison,omitempty"`
	RecordSHA256      string            `json:"record_sha256"`
}

type Gate struct {
	complete bool
}

type Progress struct {
	RecordCount       uint64 `json:"record_count"`
	EligibleSeconds   uint64 `json:"eligible_seconds"`
	SleepWakeCycles   uint32 `json:"sleep_wake_cycles"`
	RebootObserved    bool   `json:"reboot_observed"`
	InvalidSignature  bool   `json:"invalid_signature"`
	SelectorConflict  bool   `json:"selector_conflict"`
	StaleGeneration   bool   `json:"stale_generation"`
	CrossDomainCrash  bool   `json:"cross_domain_crash"`
	SafetyComparisons uint32 `json:"safety_comparisons"`
	FailedEvidence    bool   `json:"failed_evidence"`
	Complete          bool   `json:"complete"`
}

var (
	ErrInvalidBinding     = errors.New("invalid qualification binding")
	ErrInvalidRecord      = errors.New("invalid qualification evidence record")
	ErrInvalidChain       = errors.New("invalid qualification evidence chain")
	ErrIncompleteEvidence = errors.New("incomplete policy enforcement qualification")
)

func (gate Gate) Complete() bool { return gate.complete }

func (binding Binding) Validate() error {
	if !validUUID(binding.SessionID) || binding.BundleGeneration == 0 ||
		binding.RootPolicyGeneration == 0 || binding.UserPolicyGeneration == 0 ||
		!validDigest(binding.ManifestSHA256) {
		return ErrInvalidBinding
	}
	return nil
}

func (record EvidenceRecord) Validate() error {
	if record.Schema != RecordSchema || !validUUID(record.RecordID) ||
		record.Sequence == 0 || record.Binding.Validate() != nil ||
		!validUUID(record.BootID) || !validSources(record.Sources) ||
		!canonicalTime(record.ObservedAt) || record.SourceMonotonicNS < 0 ||
		!record.Result.Valid() || !record.Reason.Valid() ||
		!resultReasonMatch(record.Result, record.Reason) ||
		(record.Sequence == 1) != (record.PreviousSHA256 == "") ||
		(record.PreviousSHA256 != "" && !validDigest(record.PreviousSHA256)) ||
		!validDigest(record.RecordSHA256) {
		return ErrInvalidRecord
	}

	payloads := 0
	for _, present := range []bool{
		record.EligibleWindow != nil,
		record.SleepWake != nil,
		record.Reboot != nil,
		record.FaultInjection != nil,
		record.SafetyComparison != nil,
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 || !record.payloadValid() {
		return ErrInvalidRecord
	}
	digest, err := record.digest()
	if err != nil || digest != record.RecordSHA256 {
		return ErrInvalidRecord
	}
	return nil
}

func (record EvidenceRecord) payloadValid() bool {
	switch record.Kind {
	case KindEligibleWindow:
		return record.EligibleWindow != nil && record.EligibleWindow.valid(record)
	case KindSleepWake:
		return record.SleepWake != nil && record.SleepWake.valid(record)
	case KindReboot:
		return record.Reboot != nil && record.Reboot.valid(record)
	case KindInvalidSignature, KindSelectorConflict, KindStaleGeneration, KindCrossDomainCrash:
		return record.FaultInjection != nil && record.FaultInjection.valid(record.Kind, record.Result)
	case KindSafetyComparison:
		return record.SafetyComparison != nil && record.SafetyComparison.valid(record.Result)
	default:
		return false
	}
}

func (window EligibleWindow) valid(record EvidenceRecord) bool {
	started, startedOK := parseTime(window.StartedAt)
	ended, endedOK := parseTime(window.EndedAt)
	duration := ended.Sub(started)
	monotonic := time.Duration(window.EndedMonotonicNS - window.StartedMonotonicNS)
	return startedOK && endedOK && duration > 0 && duration <= MaximumEligibleWindow &&
		window.StartedMonotonicNS >= 0 && window.EndedMonotonicNS > window.StartedMonotonicNS &&
		absDuration(duration-monotonic) <= 2*time.Minute &&
		record.ObservedAt == window.EndedAt && record.SourceMonotonicNS == window.EndedMonotonicNS
}

func (sleep SleepWake) valid(record EvidenceRecord) bool {
	slept, sleptOK := parseTime(sleep.SleptAt)
	woke, wokeOK := parseTime(sleep.WokeAt)
	duration := woke.Sub(slept)
	monotonic := time.Duration(sleep.WokeMonotonicNS - sleep.SleptMonotonicNS)
	return sleptOK && wokeOK && duration > 0 &&
		sleep.SleptMonotonicNS >= 0 && sleep.WokeMonotonicNS > sleep.SleptMonotonicNS &&
		absDuration(duration-monotonic) <= 2*time.Minute &&
		record.ObservedAt == sleep.WokeAt && record.SourceMonotonicNS == sleep.WokeMonotonicNS
}

func (reboot Reboot) valid(record EvidenceRecord) bool {
	return validUUID(reboot.PreviousBootID) && validUUID(reboot.CurrentBootID) &&
		reboot.PreviousBootID != reboot.CurrentBootID && reboot.CurrentBootID == record.BootID &&
		canonicalTime(reboot.ObservedAt) && reboot.ObservedAt == record.ObservedAt
}

func (fault FaultInjection) valid(kind Kind, result Result) bool {
	var passed, failed FaultOutcome
	switch kind {
	case KindInvalidSignature, KindSelectorConflict:
		passed, failed = OutcomeCandidateRejected, OutcomeCandidateAccepted
	case KindStaleGeneration:
		passed, failed = OutcomeMutationRejected, OutcomeMutationAccepted
	case KindCrossDomainCrash:
		passed, failed = OutcomeDomainMismatchBlocked, OutcomeDomainMismatchOpen
	default:
		return false
	}
	return (result == ResultPassed && fault.Outcome == passed) ||
		(result == ResultFailed && fault.Outcome == failed)
}

func (comparison SafetyComparison) valid(result Result) bool {
	if !validDigest(comparison.ExpectedSHA256) || !validDigest(comparison.ObservedSHA256) {
		return false
	}
	if result == ResultPassed {
		return comparison.ExpectedSHA256 == comparison.ObservedSHA256
	}
	return comparison.ExpectedSHA256 != comparison.ObservedSHA256
}

func (result Result) Valid() bool { return result == ResultPassed || result == ResultFailed }

func (reason Reason) Valid() bool {
	switch reason {
	case ReasonNone, ReasonSourceInvalid, ReasonTimingGap, ReasonInjectionFailed, ReasonSafetyMismatch:
		return true
	default:
		return false
	}
}

func resultReasonMatch(result Result, reason Reason) bool {
	return (result == ResultPassed && reason == ReasonNone) ||
		(result == ResultFailed && reason != ReasonNone)
}

func validSources(sources []SourceReference) bool {
	if len(sources) == 0 || len(sources) > MaximumSourceReferences {
		return false
	}
	seen := make(map[metadata.UUID]struct{}, len(sources))
	for _, source := range sources {
		if !validUUID(source.EventID) || !validDigest(source.SHA256) {
			return false
		}
		if _, duplicate := seen[source.EventID]; duplicate {
			return false
		}
		seen[source.EventID] = struct{}{}
	}
	return true
}

func (record EvidenceRecord) digest() (string, error) {
	unsigned := record
	unsigned.RecordSHA256 = ""
	digest, _, err := policy.CanonicalSHA256(unsigned)
	return digest, err
}

func parseTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.UTC().Format(time.RFC3339Nano) != value {
		return time.Time{}, false
	}
	return parsed, true
}

func canonicalTime(value string) bool {
	_, ok := parseTime(value)
	return ok
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
