package telemetry

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/signing"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

const MaxGapReplayBatchesPerRun = 1

type GapReplayBudget struct {
	remaining uint8
}

type GapReplayRequest struct {
	Envelope       signing.SignedEnvelope
	Body           []byte
	BatchID        metadata.UUID
	RequestID      metadata.UUID
	FirstSequence  uint64
	LastSequence   uint64
	ReplayedEvents int
}

type GapUnrecoverableReporter struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

var (
	ErrGapRepairRejected    = errors.New("telemetry gap repair rejected")
	ErrGapReplayRateLimited = errors.New("telemetry gap replay rate limited")
	ErrGapEvidenceExpired   = errors.New("telemetry gap evidence expired")
)

func NewGapReplayBudget(max uint8) GapReplayBudget {
	if max == 0 || max > MaxGapReplayBatchesPerRun {
		max = MaxGapReplayBatchesPerRun
	}
	return GapReplayBudget{remaining: max}
}

func (budget *GapReplayBudget) Claim() error {
	if budget == nil || budget.remaining == 0 {
		return ErrGapReplayRateLimited
	}
	budget.remaining--
	return nil
}

func PrepareGapReplay(
	journal *spool.Spool,
	key signing.Key,
	ranges []SequenceRange,
	batchID metadata.UUID,
	requestID metadata.UUID,
	timestamp time.Time,
	budget *GapReplayBudget,
) (GapReplayRequest, error) {
	if _, err := metadata.ParseUUID(string(batchID)); err != nil {
		return GapReplayRequest{}, ErrGapRepairRejected
	}
	if _, err := metadata.ParseUUID(string(requestID)); err != nil {
		return GapReplayRequest{}, ErrGapRepairRejected
	}
	entries, complete, err := RetainedEntriesForRanges(journal, ranges)
	if err != nil {
		return GapReplayRequest{}, err
	}
	if !complete {
		return GapReplayRequest{}, ErrGapEvidenceExpired
	}
	if len(entries) == 0 {
		return GapReplayRequest{}, ErrGapRepairRejected
	}
	if budget == nil || budget.Claim() != nil {
		return GapReplayRequest{}, ErrGapReplayRateLimited
	}
	body, err := EncodeBatch(batchID, entries)
	if err != nil {
		return GapReplayRequest{}, err
	}
	envelope, err := signing.Sign(key, requestID, timestamp, body)
	if err != nil {
		return GapReplayRequest{}, err
	}
	return GapReplayRequest{
		Envelope:       envelope,
		Body:           body,
		BatchID:        batchID,
		RequestID:      requestID,
		FirstSequence:  entries[0].Sequence,
		LastSequence:   entries[len(entries)-1].Sequence,
		ReplayedEvents: len(entries),
	}, nil
}

func RetainedEntriesForRanges(
	journal *spool.Spool,
	ranges []SequenceRange,
) ([]spool.Entry, bool, error) {
	if journal == nil {
		return nil, false, ErrGapRepairRejected
	}
	if err := validateMissingRanges(ranges, ^uint64(0)); err != nil {
		return nil, false, err
	}
	spoolRanges := make([]spool.SequenceRange, 0, len(ranges))
	for _, item := range ranges {
		spoolRanges = append(spoolRanges, spool.SequenceRange{
			First: item.First,
			Last:  item.Last,
		})
	}
	entries, complete, err := journal.EntriesBySequenceRanges(
		spoolRanges,
		MaxBatchEvents,
	)
	if err != nil {
		return nil, false, ErrGapRepairRejected
	}
	return entries, complete, nil
}

func (reporter *GapUnrecoverableReporter) EmitOnce(
	journal *spool.Spool,
	ranges []SequenceRange,
) (bool, error) {
	if reporter == nil || journal == nil ||
		validateMissingRanges(ranges, ^uint64(0)) != nil {
		return false, ErrGapRepairRejected
	}
	key := missingRangeKey(ranges)

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if reporter.seen == nil {
		reporter.seen = make(map[string]struct{})
	}
	if _, exists := reporter.seen[key]; exists {
		return false, nil
	}
	encoded, err := event.Encode(event.SchemaDiagnostic, event.Diagnostic{
		Component: control.ComponentRuntime,
		Code:      event.DiagnosticTelemetryGapUnrecoverable,
		Count:     1,
	})
	if err != nil {
		return false, err
	}
	if _, err := journal.Append(encoded); err != nil {
		return false, err
	}
	reporter.seen[key] = struct{}{}
	return true, nil
}

func validateMissingRanges(ranges []SequenceRange, highWatermark uint64) error {
	if len(ranges) == 0 || len(ranges) > MaxMissingRanges {
		return ErrInvalidAcknowledgement
	}
	var previousLast uint64
	for index, item := range ranges {
		if item.First == 0 || item.Last < item.First ||
			item.Last-item.First+1 > MaxMissingRangeWidth ||
			item.Last > highWatermark {
			return ErrInvalidAcknowledgement
		}
		if index > 0 && item.First <= previousLast {
			return ErrInvalidAcknowledgement
		}
		previousLast = item.Last
	}
	return nil
}

func missingRangeKey(ranges []SequenceRange) string {
	parts := make([]string, 0, len(ranges))
	for _, item := range ranges {
		parts = append(parts, strconv.FormatUint(item.First, 10)+"-"+strconv.FormatUint(item.Last, 10))
	}
	return strings.Join(parts, ",")
}
