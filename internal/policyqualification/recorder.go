package policyqualification

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

type Recorder struct {
	mu      sync.Mutex
	root    string
	path    string
	binding Binding
}

func OpenRecorder(root string, binding Binding) (*Recorder, error) {
	if root == "" || filepath.Clean(root) == "." || binding.Validate() != nil {
		return nil, ErrInvalidBinding
	}
	if err := ensurePrivateRoot(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ChainFilename)
	created, err := ensureChainFile(path)
	if err != nil {
		return nil, err
	}
	if created {
		if err := syncDirectory(root); err != nil {
			return nil, err
		}
	}
	recorder := &Recorder{root: root, path: path, binding: binding}
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
	if _, err := validateChain(records, binding, false); err != nil {
		return nil, err
	}
	return recorder, nil
}

func (recorder *Recorder) RecordEligibleWindow(
	observation Observation,
	window EligibleWindow,
) (EvidenceRecord, error) {
	return recorder.append(observation, KindEligibleWindow, func(record *EvidenceRecord) {
		record.EligibleWindow = &window
	})
}

func (recorder *Recorder) RecordSleepWake(
	observation Observation,
	sleepWake SleepWake,
) (EvidenceRecord, error) {
	return recorder.append(observation, KindSleepWake, func(record *EvidenceRecord) {
		record.SleepWake = &sleepWake
	})
}

func (recorder *Recorder) RecordReboot(
	observation Observation,
	reboot Reboot,
) (EvidenceRecord, error) {
	return recorder.append(observation, KindReboot, func(record *EvidenceRecord) {
		record.Reboot = &reboot
	})
}

func (recorder *Recorder) RecordFaultInjection(
	kind Kind,
	observation Observation,
	fault FaultInjection,
) (EvidenceRecord, error) {
	switch kind {
	case KindInvalidSignature, KindSelectorConflict, KindStaleGeneration, KindCrossDomainCrash:
	default:
		return EvidenceRecord{}, ErrInvalidRecord
	}
	return recorder.append(observation, kind, func(record *EvidenceRecord) {
		record.FaultInjection = &fault
	})
}

func (recorder *Recorder) RecordSafetyComparison(
	observation Observation,
	comparison SafetyComparison,
) (EvidenceRecord, error) {
	return recorder.append(observation, KindSafetyComparison, func(record *EvidenceRecord) {
		record.SafetyComparison = &comparison
	})
}

func (recorder *Recorder) append(
	observation Observation,
	kind Kind,
	setPayload func(*EvidenceRecord),
) (EvidenceRecord, error) {
	if recorder == nil || setPayload == nil {
		return EvidenceRecord{}, ErrInvalidRecord
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	file, err := openLockedChain(recorder.path, unix.LOCK_EX)
	if err != nil {
		return EvidenceRecord{}, err
	}
	defer func() { _ = closeLockedChain(file) }()

	records, err := readRecords(file)
	if err != nil {
		return EvidenceRecord{}, err
	}
	state, err := validateChain(records, recorder.binding, false)
	if err != nil {
		return EvidenceRecord{}, err
	}
	if len(records) >= MaximumRecords {
		return EvidenceRecord{}, ErrInvalidChain
	}

	recordID, err := metadata.NewUUID(nil)
	if err != nil {
		return EvidenceRecord{}, err
	}
	record := EvidenceRecord{
		Schema: RecordSchema, RecordID: recordID,
		Sequence: uint64(len(records) + 1), PreviousSHA256: state.lastDigest,
		Kind: kind, Binding: recorder.binding, BootID: observation.BootID,
		Sources:    append([]SourceReference(nil), observation.Sources...),
		ObservedAt: observation.ObservedAt, SourceMonotonicNS: observation.SourceMonotonicNS,
		Result: observation.Result, Reason: observation.Reason,
	}
	setPayload(&record)
	record.RecordSHA256, err = record.digest()
	if err != nil || record.Validate() != nil {
		return EvidenceRecord{}, ErrInvalidRecord
	}
	if err := validateNextRecord(state, record); err != nil {
		return EvidenceRecord{}, err
	}

	_, encoded, err := policy.CanonicalSHA256(record)
	if err != nil || len(encoded)+1 > MaximumRecordBytes {
		return EvidenceRecord{}, ErrInvalidRecord
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return EvidenceRecord{}, err
	}
	encoded = append(encoded, '\n')
	written, err := file.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return EvidenceRecord{}, fmt.Errorf("write qualification evidence: %w", err)
	}
	if err := file.Sync(); err != nil {
		return EvidenceRecord{}, err
	}
	if err := closeLockedChain(file); err != nil {
		file = nil
		return EvidenceRecord{}, err
	}
	file = nil
	return record, nil
}

func ensurePrivateRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return ErrInvalidChain
	}
	return nil
}

func ensureChainFile(path string) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err == nil {
		file := os.NewFile(uintptr(fd), path)
		return false, validateChainFile(file)
	}
	if !errors.Is(err, unix.ENOENT) {
		return false, err
	}
	fd, err = unix.Open(
		path,
		unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, err
	}
	return true, validateChainFile(file)
}

func validateChainFile(file *os.File) error {
	if file == nil {
		return ErrInvalidChain
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return ErrInvalidChain
	}
	return nil
}

func openLockedChain(path string, lock int) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, ErrInvalidChain
	}
	if err := unix.Flock(int(file.Fd()), lock); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = closeLockedChain(file)
		return nil, err
	}
	return file, nil
}

func closeLockedChain(file *os.File) error {
	if file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
