package telemetry

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const GapRepairTraceReplaySchema = "hexroute.telemetry-gap-repair-trace-replay.v1"

type GapRepairTraceReplay struct {
	Schema            string                    `json:"schema"`
	Scenario          GapRepairTraceScenario    `json:"scenario"`
	TraceSHA256       string                    `json:"trace_sha256"`
	HighWatermark     uint64                    `json:"high_watermark"`
	MissingSequences  []SequenceRange           `json:"missing_sequences"`
	ReplayedSequences []uint64                  `json:"replayed_sequences,omitempty"`
	Expected          GapRepairTraceExpectation `json:"expected"`
	ReplaySHA256      string                    `json:"replay_sha256"`
}

type gapRepairTraceReplayDigestInput struct {
	Schema            string                    `json:"schema"`
	Scenario          GapRepairTraceScenario    `json:"scenario"`
	TraceSHA256       string                    `json:"trace_sha256"`
	HighWatermark     uint64                    `json:"high_watermark"`
	MissingSequences  []SequenceRange           `json:"missing_sequences"`
	ReplayedSequences []uint64                  `json:"replayed_sequences,omitempty"`
	Expected          GapRepairTraceExpectation `json:"expected"`
}

var ErrGapRepairTraceReplay = errors.New("invalid telemetry gap repair trace replay")

func ReplayGapRepairTrace(trace GapRepairTrace) (GapRepairTraceReplay, error) {
	if trace.Validate() != nil {
		return GapRepairTraceReplay{}, ErrGapRepairTraceReplay
	}
	replay := GapRepairTraceReplay{
		Schema:           GapRepairTraceReplaySchema,
		Scenario:         trace.Scenario,
		TraceSHA256:      trace.TraceSHA256,
		HighWatermark:    trace.Acknowledgement.HighWatermark,
		MissingSequences: append([]SequenceRange(nil), trace.Acknowledgement.MissingSequences...),
		Expected:         trace.Expected,
	}
	if trace.Expected.Replay {
		replayed, ok := retainedSequencesForRange(
			trace.RetainedSequences,
			trace.Expected.FirstSequence,
			trace.Expected.LastSequence,
		)
		if !ok || len(replayed) != trace.Expected.ReplayedEvents {
			return GapRepairTraceReplay{}, ErrGapRepairTraceReplay
		}
		replay.ReplayedSequences = replayed
	} else if gapRetainsRange(
		trace.RetainedSequences,
		trace.Acknowledgement.MissingSequences[0].First,
		trace.Acknowledgement.MissingSequences[0].Last,
	) {
		return GapRepairTraceReplay{}, ErrGapRepairTraceReplay
	}
	digest, err := gapRepairTraceReplayDigest(replay)
	if err != nil {
		return GapRepairTraceReplay{}, ErrGapRepairTraceReplay
	}
	replay.ReplaySHA256 = digest
	if replay.Validate() != nil {
		return GapRepairTraceReplay{}, ErrGapRepairTraceReplay
	}
	return replay, nil
}

func (replay GapRepairTraceReplay) Validate() error {
	if replay.Schema != GapRepairTraceReplaySchema ||
		!replay.Scenario.Valid() ||
		!validGapTraceDigest(replay.TraceSHA256) ||
		replay.HighWatermark == 0 ||
		validateMissingRanges(replay.MissingSequences, replay.HighWatermark) != nil ||
		replay.Expected.Validate() != nil ||
		!validGapTraceDigest(replay.ReplaySHA256) {
		return ErrGapRepairTraceReplay
	}
	if replay.Expected.Replay {
		if len(replay.ReplayedSequences) != replay.Expected.ReplayedEvents ||
			!gapRetainsRange(replay.ReplayedSequences, replay.Expected.FirstSequence, replay.Expected.LastSequence) {
			return ErrGapRepairTraceReplay
		}
	} else if len(replay.ReplayedSequences) != 0 {
		return ErrGapRepairTraceReplay
	}
	digest, err := gapRepairTraceReplayDigest(replay)
	if err != nil || digest != replay.ReplaySHA256 {
		return ErrGapRepairTraceReplay
	}
	return nil
}

func gapRepairTraceReplayDigest(replay GapRepairTraceReplay) (string, error) {
	if replay.Schema != GapRepairTraceReplaySchema || !replay.Scenario.Valid() {
		return "", ErrGapRepairTraceReplay
	}
	encoded, err := policy.MarshalCanonical(gapRepairTraceReplayDigestInput{
		Schema:            replay.Schema,
		Scenario:          replay.Scenario,
		TraceSHA256:       replay.TraceSHA256,
		HighWatermark:     replay.HighWatermark,
		MissingSequences:  replay.MissingSequences,
		ReplayedSequences: replay.ReplayedSequences,
		Expected:          replay.Expected,
	})
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(encoded), nil
}

func retainedSequencesForRange(sequences []uint64, first, last uint64) ([]uint64, bool) {
	if first == 0 || last < first {
		return nil, false
	}
	out := make([]uint64, 0, last-first+1)
	next := first
	for _, sequence := range sequences {
		if sequence == next {
			out = append(out, sequence)
			if sequence == last {
				return out, true
			}
			next++
		}
	}
	return out, false
}
