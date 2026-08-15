package reconciler

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type AttemptBinding struct {
	ActionID               metadata.UUID `json:"action_id"`
	AttemptID              metadata.UUID `json:"attempt_id"`
	Nonce                  metadata.UUID `json:"nonce"`
	Domain                 policy.Domain `json:"domain"`
	Target                 string        `json:"target"`
	BootID                 metadata.UUID `json:"boot_id"`
	BundleGeneration       uint64        `json:"bundle_generation"`
	DomainPolicyGeneration uint64        `json:"domain_policy_generation"`
	ControlGeneration      uint64        `json:"control_generation"`
	SnapshotGeneration     uint64        `json:"snapshot_generation"`
	PlanSHA256             string        `json:"plan_sha256"`
}

type AttemptJournalEntry struct {
	Sequence    uint64         `json:"sequence"`
	Previous    AttemptState   `json:"previous"`
	Attempt     AttemptRecord  `json:"attempt"`
	Binding     AttemptBinding `json:"binding"`
	Reason      Reason         `json:"reason"`
	EntrySHA256 string         `json:"entry_sha256"`
}

type attemptJournalDigestInput struct {
	Sequence uint64         `json:"sequence"`
	Previous AttemptState   `json:"previous"`
	Attempt  AttemptRecord  `json:"attempt"`
	Binding  AttemptBinding `json:"binding"`
	Reason   Reason         `json:"reason"`
}

type MemoryAttemptJournal struct {
	mu      sync.Mutex
	records map[metadata.UUID][]AttemptJournalEntry
}

type FileAttemptJournal struct {
	mu   sync.Mutex
	path string
}

type TargetRecoveryState string

const (
	TargetUntouched           TargetRecoveryState = "untouched"
	TargetExactlyOwnedApplied TargetRecoveryState = "exactly_owned_applied"
	TargetUncertain           TargetRecoveryState = "uncertain"
)

type StartupRecoveryClass string

const (
	RecoveryTerminal    StartupRecoveryClass = "terminal"
	RecoveryUntouched   StartupRecoveryClass = "untouched"
	RecoveryVerifyOwned StartupRecoveryClass = "verify_owned"
	RecoverySafeMode    StartupRecoveryClass = "safe_mode"
	RecoveryBlocked     StartupRecoveryClass = "blocked"
)

type StartupObservation struct {
	BootID      metadata.UUID       `json:"boot_id"`
	AttemptID   metadata.UUID       `json:"attempt_id"`
	TargetState TargetRecoveryState `json:"target_state"`
}

type StartupRecovery struct {
	Class      StartupRecoveryClass `json:"class"`
	Reason     Reason               `json:"reason"`
	CanAutoRun bool                 `json:"can_auto_run"`
}

var (
	ErrAttemptJournal       = errors.New("invalid attempt journal")
	ErrAttemptCAS           = errors.New("attempt transition compare-and-swap failed")
	ErrAttemptTransition    = errors.New("invalid attempt transition")
	ErrAttemptImmutable     = errors.New("attempt immutable binding mismatch")
	ErrAttemptJournalTamper = errors.New("attempt journal tamper")
)

func NewMemoryAttemptJournal() *MemoryAttemptJournal {
	return &MemoryAttemptJournal{records: make(map[metadata.UUID][]AttemptJournalEntry)}
}

func OpenFileAttemptJournal(path string) (*FileAttemptJournal, error) {
	if path == "" {
		return nil, ErrAttemptJournal
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	journal := &FileAttemptJournal{path: path}
	if _, err := journal.loadEntries(); err != nil {
		return nil, err
	}
	return journal, nil
}

func AttemptBindingFromLeaseClaim(
	lease policy.ActionLease,
	claim policy.ActionLeaseExecutionClaim,
	snapshotGeneration uint64,
) (AttemptBinding, error) {
	if lease.Validate() != nil || claim.Validate() != nil ||
		snapshotGeneration == 0 ||
		lease.ActionID != claim.ActionID ||
		lease.Domain != claim.Domain ||
		lease.Nonce != claim.Nonce ||
		lease.BootID != claim.BootID {
		return AttemptBinding{}, ErrAttemptJournal
	}
	binding := AttemptBinding{
		ActionID:               lease.ActionID,
		AttemptID:              claim.AttemptID,
		Nonce:                  lease.Nonce,
		Domain:                 lease.Domain,
		Target:                 lease.Target,
		BootID:                 lease.BootID,
		BundleGeneration:       lease.BundleGeneration,
		DomainPolicyGeneration: lease.DomainPolicyGeneration,
		ControlGeneration:      lease.ControlStateGeneration,
		SnapshotGeneration:     snapshotGeneration,
		PlanSHA256:             lease.PlanSHA256,
	}
	if binding.validate() != nil {
		return AttemptBinding{}, ErrAttemptJournal
	}
	return binding, nil
}

func (journal *MemoryAttemptJournal) AppendPending(binding AttemptBinding, reason Reason) (AttemptJournalEntry, error) {
	if journal == nil || binding.validate() != nil || !reason.Valid() {
		return AttemptJournalEntry{}, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if _, exists := journal.records[binding.ActionID]; exists {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	entry, err := newAttemptJournalEntry(1, "", AttemptPending, binding, reason)
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	journal.records[binding.ActionID] = []AttemptJournalEntry{entry}
	return entry, nil
}

func (journal *MemoryAttemptJournal) CompareAndSwap(
	binding AttemptBinding,
	from AttemptState,
	to AttemptState,
	reason Reason,
) (AttemptJournalEntry, error) {
	if journal == nil || binding.validate() != nil || !from.Valid() || !to.Valid() || !reason.Valid() {
		return AttemptJournalEntry{}, ErrAttemptJournal
	}
	if !allowedAttemptTransition(from, to) {
		return AttemptJournalEntry{}, ErrAttemptTransition
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	entries, exists := journal.records[binding.ActionID]
	if !exists || len(entries) == 0 {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	if err := verifyAttemptEntries(entries); err != nil {
		return AttemptJournalEntry{}, err
	}
	latest := entries[len(entries)-1]
	if latest.Attempt.State != from {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	if latest.Binding != binding {
		return AttemptJournalEntry{}, ErrAttemptImmutable
	}
	entry, err := newAttemptJournalEntry(latest.Sequence+1, from, to, binding, reason)
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	journal.records[binding.ActionID] = append(entries, entry)
	return entry, nil
}

func (journal *MemoryAttemptJournal) Latest(actionID metadata.UUID) (AttemptJournalEntry, bool, error) {
	if journal == nil || !validUUID(actionID) {
		return AttemptJournalEntry{}, false, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	entries, exists := journal.records[actionID]
	if !exists || len(entries) == 0 {
		return AttemptJournalEntry{}, false, nil
	}
	if err := verifyAttemptEntries(entries); err != nil {
		return AttemptJournalEntry{}, false, err
	}
	return entries[len(entries)-1], true, nil
}

func (journal *MemoryAttemptJournal) Entries(actionID metadata.UUID) ([]AttemptJournalEntry, error) {
	if journal == nil || !validUUID(actionID) {
		return nil, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	entries := append([]AttemptJournalEntry(nil), journal.records[actionID]...)
	if len(entries) > 0 {
		if err := verifyAttemptEntries(entries); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (journal *FileAttemptJournal) AppendPending(binding AttemptBinding, reason Reason) (AttemptJournalEntry, error) {
	if journal == nil || binding.validate() != nil || !reason.Valid() {
		return AttemptJournalEntry{}, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	records, err := journal.loadEntries()
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	if _, exists := records[binding.ActionID]; exists {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	entry, err := newAttemptJournalEntry(1, "", AttemptPending, binding, reason)
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	if err := journal.appendEntry(entry); err != nil {
		return AttemptJournalEntry{}, err
	}
	return entry, nil
}

func (journal *FileAttemptJournal) CompareAndSwap(
	binding AttemptBinding,
	from AttemptState,
	to AttemptState,
	reason Reason,
) (AttemptJournalEntry, error) {
	if journal == nil || binding.validate() != nil || !from.Valid() || !to.Valid() || !reason.Valid() {
		return AttemptJournalEntry{}, ErrAttemptJournal
	}
	if !allowedAttemptTransition(from, to) {
		return AttemptJournalEntry{}, ErrAttemptTransition
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	records, err := journal.loadEntries()
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	entries := records[binding.ActionID]
	if len(entries) == 0 {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	latest := entries[len(entries)-1]
	if latest.Attempt.State != from {
		return AttemptJournalEntry{}, ErrAttemptCAS
	}
	if latest.Binding != binding {
		return AttemptJournalEntry{}, ErrAttemptImmutable
	}
	entry, err := newAttemptJournalEntry(latest.Sequence+1, from, to, binding, reason)
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	if err := journal.appendEntry(entry); err != nil {
		return AttemptJournalEntry{}, err
	}
	return entry, nil
}

func (journal *FileAttemptJournal) Latest(actionID metadata.UUID) (AttemptJournalEntry, bool, error) {
	if journal == nil || !validUUID(actionID) {
		return AttemptJournalEntry{}, false, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	records, err := journal.loadEntries()
	if err != nil {
		return AttemptJournalEntry{}, false, err
	}
	entries := records[actionID]
	if len(entries) == 0 {
		return AttemptJournalEntry{}, false, nil
	}
	return entries[len(entries)-1], true, nil
}

func (journal *FileAttemptJournal) Entries(actionID metadata.UUID) ([]AttemptJournalEntry, error) {
	if journal == nil || !validUUID(actionID) {
		return nil, ErrAttemptJournal
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	records, err := journal.loadEntries()
	if err != nil {
		return nil, err
	}
	return append([]AttemptJournalEntry(nil), records[actionID]...), nil
}

func ClassifyStartupAttempt(entry AttemptJournalEntry, observation StartupObservation) (StartupRecovery, error) {
	if entry.validate() != nil || observation.validate() != nil {
		return StartupRecovery{}, ErrAttemptJournal
	}
	if terminalAttemptState(entry.Attempt.State) {
		return StartupRecovery{Class: RecoveryTerminal, Reason: entry.Reason}, nil
	}
	if observation.BootID != entry.Binding.BootID {
		return StartupRecovery{Class: RecoveryBlocked, Reason: ReasonGeneration}, nil
	}
	if observation.AttemptID != entry.Binding.AttemptID {
		return StartupRecovery{Class: RecoveryBlocked, Reason: ReasonOwnership}, nil
	}
	switch observation.TargetState {
	case TargetUntouched:
		return StartupRecovery{Class: RecoveryUntouched, Reason: ReasonAccepted}, nil
	case TargetExactlyOwnedApplied:
		return StartupRecovery{Class: RecoveryVerifyOwned, Reason: ReasonVerification}, nil
	case TargetUncertain:
		return StartupRecovery{Class: RecoverySafeMode, Reason: ReasonSafeMode}, nil
	default:
		return StartupRecovery{}, ErrAttemptJournal
	}
}

func (journal *FileAttemptJournal) loadEntries() (map[metadata.UUID][]AttemptJournalEntry, error) {
	encoded, err := os.ReadFile(journal.path)
	if err != nil {
		return nil, err
	}
	records := make(map[metadata.UUID][]AttemptJournalEntry)
	if len(encoded) == 0 {
		return records, nil
	}
	if !bytes.HasSuffix(encoded, []byte("\n")) {
		return nil, ErrAttemptJournalTamper
	}
	lines := bytes.Split(bytes.TrimSuffix(encoded, []byte("\n")), []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			return nil, ErrAttemptJournalTamper
		}
		canonical, err := policy.Canonicalize(line)
		if err != nil || !bytes.Equal(canonical, line) {
			return nil, ErrAttemptJournalTamper
		}
		var entry AttemptJournalEntry
		if err := decodeStrict(line, &entry); err != nil {
			return nil, ErrAttemptJournalTamper
		}
		if entry.validate() != nil {
			return nil, ErrAttemptJournalTamper
		}
		records[entry.Attempt.ActionID] = append(records[entry.Attempt.ActionID], entry)
	}
	for _, entries := range records {
		if err := verifyAttemptEntries(entries); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (journal *FileAttemptJournal) appendEntry(entry AttemptJournalEntry) error {
	encoded, err := policy.MarshalCanonical(entry)
	if err != nil {
		return ErrAttemptJournal
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(journal.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	written, err := file.Write(encoded)
	if err != nil {
		return err
	}
	if written != len(encoded) {
		return ErrAttemptJournal
	}
	return file.Sync()
}

func newAttemptJournalEntry(
	sequence uint64,
	previous AttemptState,
	state AttemptState,
	binding AttemptBinding,
	reason Reason,
) (AttemptJournalEntry, error) {
	entry := AttemptJournalEntry{
		Sequence: sequence,
		Previous: previous,
		Attempt: AttemptRecord{
			ActionID:   binding.ActionID,
			AttemptID:  binding.AttemptID,
			Nonce:      binding.Nonce,
			State:      state,
			PlanSHA256: binding.PlanSHA256,
		},
		Binding: binding,
		Reason:  reason,
	}
	digest, err := attemptJournalEntryDigest(entry)
	if err != nil {
		return AttemptJournalEntry{}, err
	}
	entry.EntrySHA256 = digest
	if entry.validate() != nil {
		return AttemptJournalEntry{}, ErrAttemptJournal
	}
	return entry, nil
}

func attemptJournalEntryDigest(entry AttemptJournalEntry) (string, error) {
	if entry.Sequence == 0 || !entry.Reason.Valid() || entry.Attempt.Validate() != nil || entry.Binding.validate() != nil {
		return "", ErrAttemptJournal
	}
	encoded, err := policy.MarshalCanonical(attemptJournalDigestInput{
		Sequence: entry.Sequence,
		Previous: entry.Previous,
		Attempt:  entry.Attempt,
		Binding:  entry.Binding,
		Reason:   entry.Reason,
	})
	if err != nil {
		return "", ErrAttemptJournal
	}
	return policy.SHA256Hex(encoded), nil
}

func verifyAttemptEntries(entries []AttemptJournalEntry) error {
	var previous *AttemptJournalEntry
	for index := range entries {
		entry := entries[index]
		if entry.validate() != nil {
			return ErrAttemptJournalTamper
		}
		if previous == nil {
			if entry.Sequence != 1 || entry.Previous != "" || entry.Attempt.State != AttemptPending {
				return ErrAttemptJournalTamper
			}
		} else if entry.Sequence != previous.Sequence+1 ||
			entry.Previous != previous.Attempt.State ||
			entry.Binding != previous.Binding ||
			entry.Attempt.ActionID != previous.Attempt.ActionID ||
			entry.Attempt.AttemptID != previous.Attempt.AttemptID ||
			entry.Attempt.Nonce != previous.Attempt.Nonce ||
			entry.Attempt.PlanSHA256 != previous.Attempt.PlanSHA256 {
			return ErrAttemptJournalTamper
		}
		previous = &entries[index]
	}
	return nil
}

func (entry AttemptJournalEntry) validate() error {
	if entry.Sequence == 0 ||
		entry.Attempt.Validate() != nil ||
		entry.Binding.validate() != nil ||
		!entry.Reason.Valid() ||
		!validDigest(entry.EntrySHA256) ||
		entry.Attempt.ActionID != entry.Binding.ActionID ||
		entry.Attempt.AttemptID != entry.Binding.AttemptID ||
		entry.Attempt.Nonce != entry.Binding.Nonce ||
		entry.Attempt.PlanSHA256 != entry.Binding.PlanSHA256 {
		return ErrAttemptJournal
	}
	if entry.Previous != "" && !entry.Previous.Valid() {
		return ErrAttemptJournal
	}
	digest, err := attemptJournalEntryDigest(entry)
	if err != nil || digest != entry.EntrySHA256 {
		return ErrAttemptJournal
	}
	return nil
}

func (binding AttemptBinding) validate() error {
	if !validUUID(binding.ActionID) ||
		!validUUID(binding.AttemptID) ||
		!validUUID(binding.Nonce) ||
		!binding.Domain.Valid() ||
		!validIdentifier(binding.Target) ||
		!validUUID(binding.BootID) ||
		binding.BundleGeneration == 0 ||
		binding.DomainPolicyGeneration == 0 ||
		binding.ControlGeneration == 0 ||
		binding.SnapshotGeneration == 0 ||
		!validDigest(binding.PlanSHA256) ||
		binding.ActionID == binding.AttemptID ||
		binding.ActionID == binding.Nonce ||
		binding.AttemptID == binding.Nonce {
		return ErrAttemptJournal
	}
	return nil
}

func (observation StartupObservation) validate() error {
	if !validUUID(observation.BootID) || !validUUID(observation.AttemptID) ||
		!observation.TargetState.Valid() {
		return ErrAttemptJournal
	}
	return nil
}

func (state TargetRecoveryState) Valid() bool {
	return state == TargetUntouched ||
		state == TargetExactlyOwnedApplied ||
		state == TargetUncertain
}

func allowedAttemptTransition(from, to AttemptState) bool {
	if terminalAttemptState(from) {
		return false
	}
	switch from {
	case AttemptPending:
		return to == AttemptClaimed || to == AttemptExpired ||
			to == AttemptDenied || to == AttemptCancelled
	case AttemptClaimed:
		return to == AttemptRunning || to == AttemptExpired ||
			to == AttemptDenied || to == AttemptCancelled ||
			to == AttemptSafeMode
	case AttemptRunning:
		return to == AttemptVerifying || to == AttemptCancelled ||
			to == AttemptRolledBack || to == AttemptFailed || to == AttemptSafeMode
	case AttemptVerifying:
		return to == AttemptCommitted || to == AttemptCancelled ||
			to == AttemptRolledBack || to == AttemptFailed ||
			to == AttemptSafeMode
	default:
		return false
	}
}

func terminalAttemptState(state AttemptState) bool {
	switch state {
	case AttemptCommitted, AttemptExpired, AttemptDenied, AttemptCancelled,
		AttemptRolledBack, AttemptFailed, AttemptSafeMode:
		return true
	default:
		return false
	}
}
