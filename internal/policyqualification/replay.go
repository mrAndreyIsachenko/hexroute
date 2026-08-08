package policyqualification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

const MaximumSourceBytes = 64 * 1024

type SourceLoader interface {
	LoadQualificationSource(metadata.UUID) ([]byte, error)
}

type SourceLoaderFunc func(metadata.UUID) ([]byte, error)

func (load SourceLoaderFunc) LoadQualificationSource(id metadata.UUID) ([]byte, error) {
	return load(id)
}

type chainState struct {
	lastDigest          string
	currentBoot         metadata.UUID
	lastObservedAt      time.Time
	lastMonotonicByBoot map[metadata.UUID]int64
	recordIDs           map[metadata.UUID]struct{}
	windows             []eligibleInterval
	nonWindowTimes      []time.Time
	sleepWakeCycles     uint32
	rebootObserved      bool
	faults              map[Kind]bool
	safetyComparisons   uint32
	failedEvidence      bool
	rebootTimes         []time.Time
}

type eligibleInterval struct {
	start time.Time
	end   time.Time
}

func Replay(root string, binding Binding, sources SourceLoader) (Gate, error) {
	progress, err := Inspect(root, binding, sources)
	if err != nil {
		return Gate{}, err
	}
	if !progress.Complete {
		return Gate{}, ErrIncompleteEvidence
	}
	return Gate{complete: true}, nil
}

func Inspect(root string, binding Binding, sources SourceLoader) (Progress, error) {
	if root == "" || filepath.Clean(root) == "." || binding.Validate() != nil || sources == nil {
		return Progress{}, ErrInvalidBinding
	}
	path := filepath.Join(root, ChainFilename)
	file, err := openLockedChain(path, unix.LOCK_SH)
	if err != nil {
		return Progress{}, err
	}
	records, readErr := readRecords(file)
	closeErr := closeLockedChain(file)
	if readErr != nil {
		return Progress{}, readErr
	}
	if closeErr != nil {
		return Progress{}, closeErr
	}
	state, err := validateChain(records, binding, false)
	if err != nil {
		return Progress{}, err
	}
	for _, record := range records {
		if err := verifySources(record.Sources, sources); err != nil {
			return Progress{}, invalidChain(record.Sequence, "source")
		}
	}
	return state.progress(uint64(len(records))), nil
}

func readRecords(reader io.Reader) ([]EvidenceRecord, error) {
	if reader == nil {
		return nil, ErrInvalidChain
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaximumRecordBytes)
	scanner.Split(scanCompleteLines)
	records := make([]EvidenceRecord, 0)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) == 0 || len(line) > MaximumRecordBytes || len(records) >= MaximumRecords {
			return nil, ErrInvalidChain
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var record EvidenceRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, ErrInvalidChain
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, ErrInvalidChain
		}
		if err := record.Validate(); err != nil {
			return nil, err
		}
		_, canonical, err := policy.CanonicalSHA256(record)
		if err != nil || !bytes.Equal(line, canonical) {
			return nil, ErrInvalidChain
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, ErrInvalidChain
	}
	return records, nil
}

func scanCompleteLines(data []byte, atEOF bool) (int, []byte, error) {
	if index := bytes.IndexByte(data, '\n'); index >= 0 {
		return index + 1, data[:index], nil
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, nil
		}
		return 0, nil, ErrInvalidChain
	}
	return 0, nil, nil
}

func validateChain(records []EvidenceRecord, binding Binding, requireComplete bool) (chainState, error) {
	if binding.Validate() != nil {
		return chainState{}, ErrInvalidBinding
	}
	state := chainState{
		lastMonotonicByBoot: make(map[metadata.UUID]int64),
		recordIDs:           make(map[metadata.UUID]struct{}),
		faults:              make(map[Kind]bool),
	}
	for index, record := range records {
		if record.Validate() != nil {
			return chainState{}, invalidChain(record.Sequence, "record")
		}
		if _, duplicate := state.recordIDs[record.RecordID]; duplicate {
			return chainState{}, invalidChain(record.Sequence, "record_id")
		}
		if record.Binding != binding {
			return chainState{}, invalidChain(record.Sequence, "binding")
		}
		if record.Sequence != uint64(index+1) {
			return chainState{}, invalidChain(record.Sequence, "sequence")
		}
		if record.PreviousSHA256 != state.lastDigest {
			return chainState{}, invalidChain(record.Sequence, "parent")
		}
		if err := validateNextRecord(state, record); err != nil {
			return chainState{}, err
		}
		applyRecord(&state, record)
	}
	if requireComplete && !state.complete() {
		return chainState{}, ErrIncompleteEvidence
	}
	return state, nil
}

func validateNextRecord(state chainState, record EvidenceRecord) error {
	observedAt, _ := parseTime(record.ObservedAt)
	if !state.lastObservedAt.IsZero() && observedAt.Before(state.lastObservedAt) {
		return invalidChain(record.Sequence, "wall_clock")
	}
	if state.currentBoot == "" {
		if record.Kind == KindReboot {
			return invalidChain(record.Sequence, "first_record_reboot")
		}
	} else if record.Kind == KindReboot {
		if record.Reboot == nil || record.Reboot.PreviousBootID != state.currentBoot ||
			record.Reboot.CurrentBootID != record.BootID || record.BootID == state.currentBoot {
			return invalidChain(record.Sequence, "reboot_transition")
		}
	} else if record.BootID != state.currentBoot {
		return invalidChain(record.Sequence, "boot_transition")
	}
	if record.Kind != KindReboot {
		if previous, exists := state.lastMonotonicByBoot[record.BootID]; exists && record.SourceMonotonicNS < previous {
			return invalidChain(record.Sequence, "monotonic_clock")
		}
	}
	return nil
}

func invalidChain(sequence uint64, reason string) error {
	return fmt.Errorf("%w: sequence=%d reason=%s", ErrInvalidChain, sequence, reason)
}

func applyRecord(state *chainState, record EvidenceRecord) {
	observedAt, _ := parseTime(record.ObservedAt)
	if state.currentBoot == "" || record.Kind == KindReboot {
		state.currentBoot = record.BootID
	}
	state.lastDigest = record.RecordSHA256
	state.recordIDs[record.RecordID] = struct{}{}
	state.lastObservedAt = observedAt
	state.lastMonotonicByBoot[record.BootID] = record.SourceMonotonicNS
	if record.Result == ResultFailed {
		state.failedEvidence = true
	}

	switch record.Kind {
	case KindEligibleWindow:
		start, _ := parseTime(record.EligibleWindow.StartedAt)
		end, _ := parseTime(record.EligibleWindow.EndedAt)
		if record.Result == ResultPassed {
			state.windows = append(state.windows, eligibleInterval{start: start, end: end})
		}
	case KindSleepWake:
		state.nonWindowTimes = append(state.nonWindowTimes, observedAt)
		if record.Result == ResultPassed {
			state.sleepWakeCycles++
		}
	case KindReboot:
		state.nonWindowTimes = append(state.nonWindowTimes, observedAt)
		if record.Result == ResultPassed {
			state.rebootObserved = true
			state.rebootTimes = append(state.rebootTimes, observedAt)
		}
	case KindInvalidSignature, KindSelectorConflict, KindStaleGeneration, KindCrossDomainCrash:
		state.nonWindowTimes = append(state.nonWindowTimes, observedAt)
		if record.Result == ResultPassed {
			state.faults[record.Kind] = true
		}
	case KindSafetyComparison:
		state.nonWindowTimes = append(state.nonWindowTimes, observedAt)
		if record.Result == ResultPassed {
			state.safetyComparisons++
		}
	}
}

func (state chainState) complete() bool {
	if state.failedEvidence || state.sleepWakeCycles < 2 || !state.rebootObserved ||
		state.safetyComparisons == 0 || !state.faults[KindInvalidSignature] ||
		!state.faults[KindSelectorConflict] || !state.faults[KindStaleGeneration] ||
		!state.faults[KindCrossDomainCrash] || len(state.windows) == 0 {
		return false
	}
	intervals := append([]eligibleInterval(nil), state.windows...)
	sort.Slice(intervals, func(left, right int) bool {
		return intervals[left].start.Before(intervals[right].start)
	})
	var total time.Duration
	end := intervals[0].end
	total += end.Sub(intervals[0].start)
	for _, interval := range intervals[1:] {
		if interval.start.Before(end) ||
			(!interval.start.Equal(end) && !containsTime(state.rebootTimes, interval.start)) {
			return false
		}
		end = interval.end
		total += interval.end.Sub(interval.start)
	}
	if total < MinimumEligibleDuration {
		return false
	}
	for _, observedAt := range state.nonWindowTimes {
		if !insideIntervals(intervals, observedAt) && !containsTime(state.rebootTimes, observedAt) {
			return false
		}
	}
	return true
}

func (state chainState) progress(recordCount uint64) Progress {
	var eligible time.Duration
	for _, window := range state.windows {
		eligible += window.end.Sub(window.start)
	}
	return Progress{
		RecordCount: recordCount, EligibleSeconds: uint64(eligible / time.Second),
		SleepWakeCycles: state.sleepWakeCycles, RebootObserved: state.rebootObserved,
		InvalidSignature:  state.faults[KindInvalidSignature],
		SelectorConflict:  state.faults[KindSelectorConflict],
		StaleGeneration:   state.faults[KindStaleGeneration],
		CrossDomainCrash:  state.faults[KindCrossDomainCrash],
		SafetyComparisons: state.safetyComparisons, FailedEvidence: state.failedEvidence,
		Complete: state.complete(),
	}
}

func insideIntervals(intervals []eligibleInterval, value time.Time) bool {
	for _, interval := range intervals {
		if !value.Before(interval.start) && !value.After(interval.end) {
			return true
		}
	}
	return false
}

func containsTime(values []time.Time, value time.Time) bool {
	for _, candidate := range values {
		if candidate.Equal(value) {
			return true
		}
	}
	return false
}

func verifySources(references []SourceReference, loader SourceLoader) error {
	for _, reference := range references {
		content, err := loader.LoadQualificationSource(reference.EventID)
		if err != nil || len(content) == 0 || len(content) > MaximumSourceBytes ||
			policy.SHA256Hex(content) != reference.SHA256 {
			return ErrInvalidChain
		}
	}
	return nil
}

func ReadRecords(root string) ([]EvidenceRecord, error) {
	path := filepath.Join(root, ChainFilename)
	file, err := openLockedChain(path, unix.LOCK_SH)
	if err != nil {
		return nil, err
	}
	records, readErr := readRecords(file)
	closeErr := closeLockedChain(file)
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return records, nil
}
