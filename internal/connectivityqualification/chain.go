package connectivityqualification

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// Recorder appends evidence, and refuses to open onto a chain it cannot prove.
//
// Opening validates what is already there. A recorder that appended to a
// tampered chain would produce a longer tampered chain, and the run would look
// like it was progressing.
type Recorder struct {
	mu       sync.Mutex
	path     string
	binding  Binding
	previous string
	sequence uint64
}

// OpenRecorder prepares the chain under a qualification root.
func OpenRecorder(root string, binding Binding) (*Recorder, error) {
	if root == "" || filepath.Clean(root) == "." {
		return nil, ErrInvalidChain
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}
	path := filepath.Join(root, ChainFilename)
	records, err := ReadRecords(root)
	if err != nil {
		return nil, err
	}
	if err := validate(records, binding); err != nil {
		return nil, err
	}
	recorder := &Recorder{path: path, binding: binding}
	if len(records) > 0 {
		last := records[len(records)-1]
		recorder.previous = last.RecordSHA256
		recorder.sequence = last.Sequence
	}
	return recorder, nil
}

// Append adds one record, sealing it to the chain it extends.
func (recorder *Recorder) Append(
	kind Kind,
	result Result,
	observedAt string,
	monotonicNS int64,
	fill func(*EvidenceRecord),
) (EvidenceRecord, error) {
	if recorder == nil {
		return EvidenceRecord{}, ErrInvalidChain
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.sequence >= MaxRecords {
		return EvidenceRecord{}, fmt.Errorf("%w: chain is full", ErrInvalidChain)
	}
	id, err := metadata.NewUUID(nil)
	if err != nil {
		return EvidenceRecord{}, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	record := EvidenceRecord{
		Schema: RecordSchema, RecordID: id,
		Sequence: recorder.sequence + 1, PreviousSHA256: recorder.previous,
		Kind: kind, Binding: recorder.binding, Result: result,
		ObservedAt: observedAt, SourceMonotonicNS: monotonicNS,
	}
	if fill != nil {
		fill(&record)
	}
	// A record binds to the evidence it describes, and each fault injection
	// produces its own checkpoint, snapshot, diff and proposals. So the
	// opening binding is a default the caller may replace, not a stamp —
	// otherwise every fault result would name the same evidence and none of
	// them would name the run that produced it.
	//
	// The session is the exception. It is what separates one qualification
	// from another, and a caller that could move it would be writing records
	// this recorder will refuse to reopen, with the run looking healthy right
	// up to the moment somebody reads the chain.
	record.Binding.SessionID = recorder.binding.SessionID
	digest, err := seal(record)
	if err != nil {
		return EvidenceRecord{}, err
	}
	record.RecordSHA256 = digest
	if err := record.Validate(); err != nil {
		return EvidenceRecord{}, err
	}
	if err := appendLine(recorder.path, record); err != nil {
		return EvidenceRecord{}, err
	}
	recorder.previous = digest
	recorder.sequence = record.Sequence
	return record, nil
}

// seal digests every field except the seal itself.
func seal(record EvidenceRecord) (string, error) {
	record.RecordSHA256 = ""
	digest, _, err := policy.CanonicalSHA256(record)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	return digest, nil
}

func appendLine(path string, record EvidenceRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}
	// A record that reached the page cache and not the disk is a record a
	// crash can take back, which is exactly what an append-only chain must
	// not allow.
	return file.Sync()
}

// ReadRecords returns the chain as stored, in order.
func ReadRecords(root string) ([]EvidenceRecord, error) {
	file, err := os.Open(filepath.Join(root, ChainFilename))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}
	defer file.Close()
	records := make([]EvidenceRecord, 0, 64)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record EvidenceRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
		}
		records = append(records, record)
		if len(records) > MaxRecords {
			return nil, fmt.Errorf("%w: chain is longer than may be held", ErrInvalidChain)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChain, err)
	}
	return records, nil
}

// validate proves the chain is whole, sealed and from one session.
func validate(records []EvidenceRecord, binding Binding) error {
	previous := ""
	for index, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		if record.Sequence != uint64(index)+1 {
			return fmt.Errorf("%w: sequence %d at position %d",
				ErrInvalidChain, record.Sequence, index+1)
		}
		if record.PreviousSHA256 != previous {
			return fmt.Errorf("%w: record %d does not follow the one before it",
				ErrInvalidChain, record.Sequence)
		}
		digest, err := seal(record)
		if err != nil {
			return err
		}
		if digest != record.RecordSHA256 {
			return fmt.Errorf("%w: record %d was rewritten after it was sealed",
				ErrInvalidChain, record.Sequence)
		}
		// Evidence from another run describes another host state. Two
		// sessions in one chain add up to a number about neither.
		if record.Binding.SessionID != binding.SessionID {
			return fmt.Errorf("%w: record %d belongs to another session",
				ErrInvalidChain, record.Sequence)
		}
		previous = record.RecordSHA256
	}
	return nil
}

// Inspect derives what the chain amounts to.
//
// Completion is recomputed here every time. Nothing stores it, so no record
// can assert a gate is finished — the records say what happened and this
// decides what that adds up to.
func Inspect(root string, binding Binding) (Progress, error) {
	records, err := ReadRecords(root)
	if err != nil {
		return Progress{}, err
	}
	if err := validate(records, binding); err != nil {
		return Progress{}, err
	}
	progress := Progress{Records: uint64(len(records))}
	injected := make(map[Fault]struct{})
	for _, record := range records {
		// One record, one divergence. A verification that both carries a
		// diverged result and reports diverged links is still one thing that
		// went wrong, and counting it twice makes the number unreadable —
		// which it was: a single failing verification reported two.
		if record.Result == ResultDiverged {
			progress.Diverged++
		}
		switch record.Kind {
		case KindEligibleWindow:
			// A diverged window is time that passed without being eligible:
			// the host was up and nothing was observing it, or the clocks
			// disagreed about how long it was. Counting it would let a gate
			// reach 72 hours on the strength of the hours it failed.
			if record.EligibleWindow != nil && record.Result != ResultDiverged {
				progress.EligibleSeconds += record.EligibleWindow.Seconds
			}
		case KindSleepWake:
			progress.SleepWakeCycles++
		case KindReboot:
			progress.Reboots++
		case KindFaultInjection:
			if record.FaultInjection == nil {
				continue
			}
			injected[record.FaultInjection.Fault] = struct{}{}
			if record.FaultInjection.GuessedHealthy {
				progress.GuessedHealthy = true
			}
		case KindVerification:
			if record.Verification == nil {
				continue
			}
			if record.Verification.Diverged > 0 && record.Result != ResultDiverged {
				progress.Diverged++
			}
			if record.Verification.Unbound > 0 {
				progress.Unbound++
			}
		case KindClockAnomaly:
			// Already counted: an anomaly record is diverged by construction.
		}
	}
	for _, fault := range connectivitytrace.Faults() {
		if _, covered := injected[fault]; covered {
			progress.FaultsInjected = append(progress.FaultsInjected, fault)
			continue
		}
		progress.FaultsMissing = append(progress.FaultsMissing, fault)
	}
	progress.Complete, progress.Blocking = complete(progress)
	return progress, nil
}

// complete says whether the gate is met and, when it is not, what stops it.
func complete(progress Progress) (bool, string) {
	switch {
	case progress.GuessedHealthy:
		return false, "an injected fault produced a healthy-looking state"
	case progress.Diverged > 0:
		return false, "an outcome contradicted its expectation"
	case progress.Unbound > 0:
		return false, "a recorded result rests on evidence that cannot be replayed"
	case progress.EligibleSeconds < EligibleHours*3600:
		return false, "not enough eligible time"
	case progress.SleepWakeCycles < RequiredSleepWakeCycles:
		return false, "not enough sleep/wake cycles"
	case progress.Reboots < RequiredReboots:
		return false, "no reboot was survived"
	case len(progress.FaultsMissing) > 0:
		return false, "some fault traces were never injected"
	default:
		return true, ""
	}
}
