// Package connectivityqualification records what a shadow soak observed, as a
// chain that cannot be extended without saying so.
//
// The gate asks for 72 eligible hours, two sleep/wake cycles, one reboot and
// every fault trace injected, with each result bound to the evidence it came
// from. It also says how completion may be decided:
//
//	Completion SHALL be derived by replay of a durable, gap-free evidence
//	chain; aggregate flags or probabilistic confidence SHALL NOT complete the
//	gate.
//
// So there is no "complete" flag to set. Completion is recomputed from the
// records every time it is asked for, and a chain missing a link cannot be
// argued into completeness by anything a later record claims about it.
//
// This mirrors the policy qualification chain rather than reusing it. The
// shape transfers — hash-linked records, replay-derived progress — but the
// content does not: a policy result binds to bundle and manifest generations,
// and a connectivity result binds to a checkpoint, a snapshot, a diff, a
// proposal set and a fault trace. Sharing a binding that fitted neither would
// be worse than two that each fit.
package connectivityqualification

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	// RecordSchema names the wire contract for one evidence record.
	RecordSchema = "hexroute.connectivity-qualification-record.v1"
	// ChainFilename is the append-only chain inside a qualification root.
	ChainFilename = "connectivity-qualification.jsonl"

	// EligibleHours is how much awake, observing time the gate requires.
	EligibleHours = 72
	// RequiredSleepWakeCycles is how many full wakes must be survived.
	RequiredSleepWakeCycles = 2
	// RequiredReboots is how many boot transitions must be survived.
	RequiredReboots = 1
	// MaxRecords bounds one chain. A soak that produced more than this has
	// stopped being a soak and started being a loop.
	MaxRecords = 100_000
)

// Kind is what a record reports.
type Kind string

const (
	// KindEligibleWindow is a stretch of awake, observing time.
	KindEligibleWindow Kind = "eligible_window"
	// KindSleepWake is a full wake the read model re-baselined after.
	KindSleepWake Kind = "sleep_wake"
	// KindReboot is a boot transition.
	KindReboot Kind = "reboot"
	// KindFaultInjection is one trace injected and what it produced.
	KindFaultInjection Kind = "fault_injection"
	// KindVerification is a replay of the stored lineage against its journal.
	KindVerification Kind = "verification"
	// KindClockAnomaly is two clocks that cannot both be right.
	//
	// It is its own kind because it is the one event that makes every
	// measurement around it unusable: eligible time, sleep and the gap
	// between samples are all read off those clocks. Folding it into a window
	// would mean recording a duration derived from the readings that just
	// proved themselves untrustworthy.
	KindClockAnomaly Kind = "clock_anomaly"
)

// Result is the verdict a record carries.
type Result string

const (
	ResultObserved Result = "observed"
	ResultExpected Result = "expected"
	// ResultDiverged means the outcome differed from what the trace said it
	// must be, or a replay contradicted a published conclusion. One of these
	// anywhere in a chain prevents completion; it is not averaged away.
	ResultDiverged Result = "diverged"
)

// Binding ties a record to the evidence it describes.
//
// It is per record, not per chain. A fault injection produces its own
// checkpoint and its own outputs, and a result that named the chain's opening
// evidence would be a result read against something it was not derived from.
// Only the session is fixed for the whole chain.
//
// Every digest is required. A record that named only some of them would let a
// result be read against evidence it was not derived from, which is the way a
// chain stops meaning anything while still verifying.
type Binding struct {
	// SessionID separates one qualification run from another. Evidence from
	// two runs in one chain describes neither.
	SessionID metadata.UUID `json:"session_id"`
	BootID    string        `json:"boot_id"`

	CheckpointID    string `json:"checkpoint_id"`
	SnapshotSHA256  string `json:"snapshot_sha256"`
	DiffSHA256      string `json:"diff_sha256"`
	ProposalsSHA256 string `json:"proposals_sha256"`
}

// EligibleWindow is awake, observing time.
//
// Sleep is excluded rather than counted as failure, matching how the read
// model treats it: a host that slept did not fail to observe, it was not
// there to observe.
type EligibleWindow struct {
	Seconds uint64 `json:"seconds"`
}

// SleepWake reports a full wake and whether the components it invalidated were
// restated afterwards.
type SleepWake struct {
	Rebaselined bool `json:"rebaselined"`
}

// Reboot reports a boot transition and the identity it moved to.
type Reboot struct {
	ToBootID string `json:"to_boot_id"`
}

// FaultInjection reports one trace and what it made visible.
type FaultInjection struct {
	Fault Fault `json:"fault"`
	// TraceSHA256 binds this result to the trace content that produced it. A
	// result naming a trace whose content has since changed is a result about
	// something that no longer exists.
	TraceSHA256 string `json:"trace_sha256"`
	// Visible is what the read model actually reported, against the trace's
	// stated expectation.
	Visible string `json:"visible"`
	// GuessedHealthy must be false. It is recorded because it is the one
	// outcome that would make every other number in the chain meaningless.
	GuessedHealthy bool `json:"guessed_healthy"`
}

// Fault is the trace identity, kept as its own type so a record cannot carry
// a name no catalogue knows.
type Fault = connectivitytrace.Fault

// ClockAnomaly reports readings that cannot all be true in one boot.
//
// The continuous clock counts through sleep and the awake clock stops for it,
// so the awake reading may lag and may not lead, and neither may run
// backwards. The deltas are kept as observed rather than corrected: what makes
// this useful later is exactly what the clocks said.
type ClockAnomaly struct {
	ContinuousDeltaNS int64 `json:"continuous_delta_ns"`
	AwakeDeltaNS      int64 `json:"awake_delta_ns"`
	WallDeltaNS       int64 `json:"wall_delta_ns"`
}

// Verification reports a replay of the stored lineage against its journals.
type Verification struct {
	Reproduced   int `json:"reproduced"`
	Diverged     int `json:"diverged"`
	Unreplayable int `json:"unreplayable"`
}

// EvidenceRecord is one link in the chain.
type EvidenceRecord struct {
	Schema   string        `json:"schema"`
	RecordID metadata.UUID `json:"record_id"`
	Sequence uint64        `json:"sequence"`
	// PreviousSHA256 is empty only at the first record. It is what makes a
	// removed link visible: the one after it names a predecessor that is not
	// there.
	PreviousSHA256 string `json:"previous_sha256,omitempty"`

	Kind    Kind    `json:"kind"`
	Binding Binding `json:"binding"`
	Result  Result  `json:"result"`

	ObservedAt        string `json:"observed_at"`
	SourceMonotonicNS int64  `json:"source_monotonic_ns"`

	EligibleWindow *EligibleWindow `json:"eligible_window,omitempty"`
	SleepWake      *SleepWake      `json:"sleep_wake,omitempty"`
	Reboot         *Reboot         `json:"reboot,omitempty"`
	FaultInjection *FaultInjection `json:"fault_injection,omitempty"`
	Verification   *Verification   `json:"verification,omitempty"`
	ClockAnomaly   *ClockAnomaly   `json:"clock_anomaly,omitempty"`

	// RecordSHA256 covers every field above. Rewriting any of them breaks it,
	// and breaking it breaks every link after.
	RecordSHA256 string `json:"record_sha256"`
}

// Progress is what a chain adds up to. It is derived, never stored.
type Progress struct {
	Records         uint64 `json:"records"`
	EligibleSeconds uint64 `json:"eligible_seconds"`
	SleepWakeCycles uint32 `json:"sleep_wake_cycles"`
	Reboots         uint32 `json:"reboots"`

	// FaultsInjected names the traces this chain has covered, so a run that
	// skipped one is visibly short rather than merely incomplete.
	FaultsInjected []Fault `json:"faults_injected"`
	FaultsMissing  []Fault `json:"faults_missing"`

	// Diverged counts records whose outcome contradicted its expectation.
	// Any of these prevents completion.
	Diverged uint32 `json:"diverged"`
	// GuessedHealthy records that some injection produced a healthy-looking
	// state it had no right to. It ends the run rather than reducing a score.
	GuessedHealthy bool `json:"guessed_healthy"`

	Complete bool `json:"complete"`
	// Blocking says, in one bounded phrase, what stops completion. An
	// incomplete gate that cannot say why is a gate nobody can finish.
	Blocking string `json:"blocking,omitempty"`
}

var (
	ErrInvalidBinding = errors.New("invalid connectivity qualification binding")
	ErrInvalidRecord  = errors.New("invalid connectivity qualification record")
	ErrInvalidChain   = errors.New("invalid connectivity qualification chain")
)

// Validate rejects a binding that names only part of its evidence.
func (binding Binding) Validate() error {
	if !validUUID(binding.SessionID) ||
		!validBootID(binding.BootID) ||
		binding.CheckpointID == "" ||
		!validDigest(binding.SnapshotSHA256) ||
		!validDigest(binding.DiffSHA256) ||
		!validDigest(binding.ProposalsSHA256) {
		return ErrInvalidBinding
	}
	return nil
}

// Validate rejects a record this recorder could not have produced.
func (record EvidenceRecord) Validate() error {
	if record.Schema != RecordSchema ||
		!validUUID(record.RecordID) ||
		record.Sequence == 0 ||
		record.Binding.Validate() != nil ||
		record.ObservedAt == "" ||
		record.SourceMonotonicNS < 0 ||
		!validDigest(record.RecordSHA256) {
		return ErrInvalidRecord
	}
	if !record.Result.valid() {
		return ErrInvalidRecord
	}
	// A record carries exactly the detail its kind is about. One carrying
	// none says nothing, and one carrying another kind's detail says two
	// things that cannot both be checked against it.
	payloads := 0
	for _, present := range []bool{
		record.EligibleWindow != nil, record.SleepWake != nil,
		record.Reboot != nil, record.FaultInjection != nil,
		record.Verification != nil, record.ClockAnomaly != nil,
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return ErrInvalidRecord
	}
	switch record.Kind {
	case KindEligibleWindow:
		if record.EligibleWindow == nil || record.EligibleWindow.Seconds == 0 {
			return ErrInvalidRecord
		}
	case KindSleepWake:
		if record.SleepWake == nil {
			return ErrInvalidRecord
		}
	case KindReboot:
		if record.Reboot == nil || !validBootID(record.Reboot.ToBootID) {
			return ErrInvalidRecord
		}
	case KindFaultInjection:
		if record.FaultInjection == nil ||
			record.FaultInjection.Fault == "" ||
			!validDigest(record.FaultInjection.TraceSHA256) ||
			record.FaultInjection.Visible == "" {
			return ErrInvalidRecord
		}
	case KindVerification:
		if record.Verification == nil {
			return ErrInvalidRecord
		}
	case KindClockAnomaly:
		// An anomaly nobody can reconstruct is an assertion, not evidence.
		if record.ClockAnomaly == nil || record.Result != ResultDiverged {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

func (result Result) valid() bool {
	switch result {
	case ResultObserved, ResultExpected, ResultDiverged:
		return true
	default:
		return false
	}
}

func validUUID(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err == nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') {
			continue
		}
		return false
	}
	return true
}

// validBootID accepts the bounded alphabet a boot identity uses, without
// importing the fact model to ask.
func validBootID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return true
}
