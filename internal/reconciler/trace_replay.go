package reconciler

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const SyntheticTraceReplaySchema = "hexroute.reconciler-synthetic-trace-replay.v1"

type SyntheticTraceReplay struct {
	Schema            string                    `json:"schema"`
	Scenario          SyntheticTraceScenario    `json:"scenario"`
	TraceSHA256       string                    `json:"trace_sha256"`
	RecordSHA256      []string                  `json:"record_sha256"`
	Provenance        []ActionProvenance        `json:"provenance"`
	SessionLifecycles []SessionLifecycle        `json:"session_lifecycles,omitempty"`
	CheckpointSHA256  []string                  `json:"checkpoint_sha256,omitempty"`
	PlanSHA256        []string                  `json:"plan_sha256,omitempty"`
	AttemptStates     []AttemptState            `json:"attempt_states,omitempty"`
	TerminalOutcomes  []TerminalOutcome         `json:"terminal_outcomes,omitempty"`
	Expected          SyntheticTraceExpectation `json:"expected"`
	ReplaySHA256      string                    `json:"replay_sha256"`
}

type syntheticTraceReplayDigestInput struct {
	Schema            string                    `json:"schema"`
	Scenario          SyntheticTraceScenario    `json:"scenario"`
	TraceSHA256       string                    `json:"trace_sha256"`
	RecordSHA256      []string                  `json:"record_sha256"`
	Provenance        []ActionProvenance        `json:"provenance"`
	SessionLifecycles []SessionLifecycle        `json:"session_lifecycles,omitempty"`
	CheckpointSHA256  []string                  `json:"checkpoint_sha256,omitempty"`
	PlanSHA256        []string                  `json:"plan_sha256,omitempty"`
	AttemptStates     []AttemptState            `json:"attempt_states,omitempty"`
	TerminalOutcomes  []TerminalOutcome         `json:"terminal_outcomes,omitempty"`
	Expected          SyntheticTraceExpectation `json:"expected"`
}

var ErrSyntheticTraceReplay = errors.New("invalid synthetic trace replay")

func ReplaySyntheticTrace(trace SyntheticTrace) (SyntheticTraceReplay, error) {
	if trace.Validate() != nil {
		return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
	}
	replay := SyntheticTraceReplay{
		Schema:      SyntheticTraceReplaySchema,
		Scenario:    trace.Scenario,
		TraceSHA256: trace.TraceSHA256,
		Expected:    trace.Expected,
	}
	for _, record := range trace.Records {
		encoded, digest, err := EncodeActionRecord(record)
		if err != nil {
			return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
		}
		decoded, err := DecodeActionRecord(encoded, record.Provenance.Kind)
		if err != nil || decoded.RecordSHA256 != digest {
			return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
		}
		replay.RecordSHA256 = append(replay.RecordSHA256, decoded.RecordSHA256)
		replay.Provenance = append(replay.Provenance, decoded.Provenance)
		switch payload := decoded.Payload.(type) {
		case OperationSessionRecord:
			replay.SessionLifecycles = append(replay.SessionLifecycles, payload.Lifecycle)
		case CheckpointRecord:
			replay.CheckpointSHA256 = append(replay.CheckpointSHA256, traceCheckpointDigest(trace, payload))
		case ActionPlanRecord:
			replay.PlanSHA256 = append(replay.PlanSHA256, payload.PlanSHA256)
		case AttemptRecord:
			replay.AttemptStates = append(replay.AttemptStates, payload.State)
		case OutcomeRecord:
			replay.TerminalOutcomes = append(replay.TerminalOutcomes, payload.Outcome)
		}
	}
	for _, entry := range trace.AttemptJournal {
		if !attemptStateInReplay(entry.Attempt.State, replay.AttemptStates) {
			return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
		}
	}
	for _, checkpoint := range trace.Checkpoints {
		if !digestInReplay(checkpoint.CheckpointSHA256, replay.CheckpointSHA256) {
			return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
		}
	}
	for _, continuation := range trace.ReplayContinuations {
		if !digestInReplay(continuation.PlanSHA256, replay.PlanSHA256) {
			replay.PlanSHA256 = append(replay.PlanSHA256, continuation.PlanSHA256)
		}
		if !sessionLifecycleInReplay(continuation.SessionLifecycle, replay.SessionLifecycles) {
			replay.SessionLifecycles = append(replay.SessionLifecycles, continuation.SessionLifecycle)
		}
	}
	if err := replay.matchesExpected(); err != nil {
		return SyntheticTraceReplay{}, err
	}
	digest, err := syntheticTraceReplayDigest(replay)
	if err != nil {
		return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
	}
	replay.ReplaySHA256 = digest
	if replay.Validate() != nil {
		return SyntheticTraceReplay{}, ErrSyntheticTraceReplay
	}
	return replay, nil
}

func (replay SyntheticTraceReplay) Validate() error {
	if replay.Schema != SyntheticTraceReplaySchema ||
		!replay.Scenario.Valid() ||
		!validDigest(replay.TraceSHA256) ||
		!validDigestList(replay.RecordSHA256, 1, MaxDigestReferences*MaxPlanSteps) ||
		len(replay.Provenance) != len(replay.RecordSHA256) ||
		replay.Expected.Validate() != nil ||
		!validDigest(replay.ReplaySHA256) {
		return ErrSyntheticTraceReplay
	}
	for _, provenance := range replay.Provenance {
		if provenance.Validate(provenance.Kind) != nil {
			return ErrSyntheticTraceReplay
		}
	}
	for _, lifecycle := range replay.SessionLifecycles {
		if !lifecycle.Valid() {
			return ErrSyntheticTraceReplay
		}
	}
	if len(replay.CheckpointSHA256) > 0 && !validDigestList(replay.CheckpointSHA256, 1, MaxDigestReferences) {
		return ErrSyntheticTraceReplay
	}
	if len(replay.PlanSHA256) > 0 && !validDigestList(replay.PlanSHA256, 1, MaxDigestReferences) {
		return ErrSyntheticTraceReplay
	}
	for _, state := range replay.AttemptStates {
		if !state.Valid() {
			return ErrSyntheticTraceReplay
		}
	}
	for _, outcome := range replay.TerminalOutcomes {
		if !outcome.Valid() {
			return ErrSyntheticTraceReplay
		}
	}
	digest, err := syntheticTraceReplayDigest(replay)
	if err != nil || digest != replay.ReplaySHA256 {
		return ErrSyntheticTraceReplay
	}
	return nil
}

func (replay SyntheticTraceReplay) matchesExpected() error {
	if replay.Expected.PlanSHA256 != "" && !digestInReplay(replay.Expected.PlanSHA256, replay.PlanSHA256) {
		return ErrSyntheticTraceReplay
	}
	if replay.Expected.AttemptState != "" && !attemptStateInReplay(replay.Expected.AttemptState, replay.AttemptStates) {
		return ErrSyntheticTraceReplay
	}
	if replay.Expected.TerminalOutcome != "" && !terminalOutcomeInReplay(replay.Expected.TerminalOutcome, replay.TerminalOutcomes) {
		return ErrSyntheticTraceReplay
	}
	if replay.Expected.SessionLifecycle != "" && !sessionLifecycleInReplay(replay.Expected.SessionLifecycle, replay.SessionLifecycles) {
		return ErrSyntheticTraceReplay
	}
	return nil
}

func syntheticTraceReplayDigest(replay SyntheticTraceReplay) (string, error) {
	if replay.Schema != SyntheticTraceReplaySchema || !replay.Scenario.Valid() {
		return "", ErrSyntheticTraceReplay
	}
	encoded, err := policy.MarshalCanonical(syntheticTraceReplayDigestInput{
		Schema:            replay.Schema,
		Scenario:          replay.Scenario,
		TraceSHA256:       replay.TraceSHA256,
		RecordSHA256:      replay.RecordSHA256,
		Provenance:        replay.Provenance,
		SessionLifecycles: replay.SessionLifecycles,
		CheckpointSHA256:  replay.CheckpointSHA256,
		PlanSHA256:        replay.PlanSHA256,
		AttemptStates:     replay.AttemptStates,
		TerminalOutcomes:  replay.TerminalOutcomes,
		Expected:          replay.Expected,
	})
	if err != nil {
		return "", err
	}
	return policy.SHA256Hex(encoded), nil
}

func traceCheckpointDigest(trace SyntheticTrace, record CheckpointRecord) string {
	for _, checkpoint := range trace.Checkpoints {
		if checkpoint.Checkpoint.OperationID == record.OperationID &&
			checkpoint.Checkpoint.Sequence == record.Sequence {
			return checkpoint.CheckpointSHA256
		}
	}
	return ""
}

func attemptStateInReplay(state AttemptState, states []AttemptState) bool {
	for _, candidate := range states {
		if candidate == state {
			return true
		}
	}
	return false
}

func terminalOutcomeInReplay(outcome TerminalOutcome, outcomes []TerminalOutcome) bool {
	for _, candidate := range outcomes {
		if candidate == outcome {
			return true
		}
	}
	return false
}

func sessionLifecycleInReplay(lifecycle SessionLifecycle, lifecycles []SessionLifecycle) bool {
	for _, candidate := range lifecycles {
		if candidate == lifecycle {
			return true
		}
	}
	return false
}

func digestInReplay(digest string, digests []string) bool {
	for _, candidate := range digests {
		if candidate == digest {
			return true
		}
	}
	return false
}
