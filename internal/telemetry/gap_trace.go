package telemetry

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const GapRepairTraceSchema = "hexroute.telemetry-gap-repair-trace.v1"

type GapRepairTraceScenario string

const (
	GapTraceRetainedRange    GapRepairTraceScenario = "retained_range"
	GapTraceUnrecoverableGap GapRepairTraceScenario = "unrecoverable_gap"
)

type GapRepairTrace struct {
	Schema            string                    `json:"schema"`
	Scenario          GapRepairTraceScenario    `json:"scenario"`
	Acknowledgement   Acknowledgement           `json:"acknowledgement"`
	RetainedSequences []uint64                  `json:"retained_sequences"`
	Expected          GapRepairTraceExpectation `json:"expected"`
	TraceSHA256       string                    `json:"trace_sha256"`
}

type GapRepairTraceExpectation struct {
	Replay                bool                 `json:"replay"`
	FirstSequence         uint64               `json:"first_sequence,omitempty"`
	LastSequence          uint64               `json:"last_sequence,omitempty"`
	ReplayedEvents        int                  `json:"replayed_events,omitempty"`
	Unrecoverable         bool                 `json:"unrecoverable"`
	DiagnosticCode        event.DiagnosticCode `json:"diagnostic_code,omitempty"`
	NewerUploadsAllowed   bool                 `json:"newer_uploads_allowed"`
	LocalControlUnchanged bool                 `json:"local_control_unchanged"`
}

type gapRepairTraceDigestInput struct {
	Schema            string                    `json:"schema"`
	Scenario          GapRepairTraceScenario    `json:"scenario"`
	Acknowledgement   Acknowledgement           `json:"acknowledgement"`
	RetainedSequences []uint64                  `json:"retained_sequences"`
	Expected          GapRepairTraceExpectation `json:"expected"`
}

var ErrGapRepairTrace = errors.New("invalid telemetry gap repair trace")

func CanonicalGapRepairTraces() ([]GapRepairTrace, error) {
	scenarios := []GapRepairTraceScenario{
		GapTraceRetainedRange,
		GapTraceUnrecoverableGap,
	}
	traces := make([]GapRepairTrace, 0, len(scenarios))
	for _, scenario := range scenarios {
		trace, err := BuildCanonicalGapRepairTrace(scenario)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, nil
}

func BuildCanonicalGapRepairTrace(scenario GapRepairTraceScenario) (GapRepairTrace, error) {
	switch scenario {
	case GapTraceRetainedRange:
		return finalizeGapRepairTrace(GapRepairTrace{
			Schema:   GapRepairTraceSchema,
			Scenario: scenario,
			Acknowledgement: gapTraceAcknowledgement(
				scenario,
				3,
				[]SequenceRange{{First: 2, Last: 3}},
			),
			RetainedSequences: []uint64{1, 2, 3},
			Expected: GapRepairTraceExpectation{
				Replay: true, FirstSequence: 2, LastSequence: 3, ReplayedEvents: 2,
				NewerUploadsAllowed: true, LocalControlUnchanged: true,
			},
		})
	case GapTraceUnrecoverableGap:
		return finalizeGapRepairTrace(GapRepairTrace{
			Schema:   GapRepairTraceSchema,
			Scenario: scenario,
			Acknowledgement: gapTraceAcknowledgement(
				scenario,
				3,
				[]SequenceRange{{First: 1, Last: 2}},
			),
			RetainedSequences: []uint64{1},
			Expected: GapRepairTraceExpectation{
				Replay: false, Unrecoverable: true,
				DiagnosticCode:        event.DiagnosticTelemetryGapUnrecoverable,
				NewerUploadsAllowed:   true,
				LocalControlUnchanged: true,
			},
		})
	default:
		return GapRepairTrace{}, ErrGapRepairTrace
	}
}

func (trace GapRepairTrace) Validate() error {
	if trace.Schema != GapRepairTraceSchema ||
		!trace.Scenario.Valid() ||
		validateAcknowledgement(trace.Acknowledgement) != nil ||
		len(trace.RetainedSequences) == 0 ||
		trace.Expected.Validate() != nil ||
		!gapRetainedSequencesSorted(trace.RetainedSequences) ||
		!validGapTraceDigest(trace.TraceSHA256) {
		return ErrGapRepairTrace
	}
	if trace.Expected.Replay {
		if !gapRetainsRange(trace.RetainedSequences, trace.Expected.FirstSequence, trace.Expected.LastSequence) {
			return ErrGapRepairTrace
		}
	} else if !trace.Expected.Unrecoverable {
		return ErrGapRepairTrace
	}
	digest, err := gapRepairTraceDigest(trace)
	if err != nil || digest != trace.TraceSHA256 {
		return ErrGapRepairTrace
	}
	return nil
}

func (expectation GapRepairTraceExpectation) Validate() error {
	if !expectation.NewerUploadsAllowed || !expectation.LocalControlUnchanged {
		return ErrGapRepairTrace
	}
	if expectation.Replay {
		if expectation.Unrecoverable ||
			expectation.FirstSequence == 0 ||
			expectation.LastSequence < expectation.FirstSequence ||
			expectation.ReplayedEvents != int(expectation.LastSequence-expectation.FirstSequence+1) ||
			expectation.DiagnosticCode != "" {
			return ErrGapRepairTrace
		}
		return nil
	}
	if !expectation.Unrecoverable ||
		expectation.FirstSequence != 0 ||
		expectation.LastSequence != 0 ||
		expectation.ReplayedEvents != 0 ||
		expectation.DiagnosticCode != event.DiagnosticTelemetryGapUnrecoverable {
		return ErrGapRepairTrace
	}
	return nil
}

func (scenario GapRepairTraceScenario) Valid() bool {
	return scenario == GapTraceRetainedRange || scenario == GapTraceUnrecoverableGap
}

func finalizeGapRepairTrace(trace GapRepairTrace) (GapRepairTrace, error) {
	digest, err := gapRepairTraceDigest(trace)
	if err != nil {
		return GapRepairTrace{}, err
	}
	trace.TraceSHA256 = digest
	if trace.Validate() != nil {
		return GapRepairTrace{}, ErrGapRepairTrace
	}
	return trace, nil
}

func gapTraceAcknowledgement(
	scenario GapRepairTraceScenario,
	highWatermark uint64,
	missing []SequenceRange,
) Acknowledgement {
	return Acknowledgement{
		Schema:           AcknowledgementSchema,
		Version:          ProtocolVersion,
		BatchID:          deterministicGapTraceUUID(scenario, "batch"),
		NodeID:           deterministicGapTraceUUID(scenario, "node"),
		RequestID:        deterministicGapTraceUUID(scenario, "request"),
		HighWatermark:    highWatermark,
		AcceptedEventIDs: []metadata.UUID{deterministicGapTraceUUID(scenario, "event")},
		MissingSequences: missing,
	}
}

func gapRepairTraceDigest(trace GapRepairTrace) (string, error) {
	if trace.Schema != GapRepairTraceSchema || !trace.Scenario.Valid() {
		return "", ErrGapRepairTrace
	}
	encoded, err := policy.MarshalCanonical(gapRepairTraceDigestInput{
		Schema:            trace.Schema,
		Scenario:          trace.Scenario,
		Acknowledgement:   trace.Acknowledgement,
		RetainedSequences: trace.RetainedSequences,
		Expected:          trace.Expected,
	})
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(encoded), nil
}

func gapRetainedSequencesSorted(sequences []uint64) bool {
	var previous uint64
	for index, sequence := range sequences {
		if sequence == 0 || (index > 0 && sequence <= previous) {
			return false
		}
		previous = sequence
	}
	return true
}

func gapRetainsRange(sequences []uint64, first, last uint64) bool {
	if first == 0 || last < first {
		return false
	}
	next := first
	for _, sequence := range sequences {
		if sequence == next {
			if sequence == last {
				return true
			}
			next++
		}
	}
	return false
}

func validGapTraceDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func deterministicGapTraceUUID(scenario GapRepairTraceScenario, label string) metadata.UUID {
	sum := sha256.Sum256([]byte("hexroute.telemetry.gap.trace." + string(scenario) + "." + label))
	raw := append([]byte(nil), sum[:16]...)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return metadata.UUID(fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]))
}
