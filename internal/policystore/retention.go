package policystore

import (
	"bytes"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"golang.org/x/sys/unix"
)

const (
	ResolutionSchema         = "hexroute.policy-resolution.v1"
	AuditIndexSchema         = "hexroute.policy-audit-index.v1"
	RetainedValidGenerations = 16
	MaxAuditEntries          = 128
	AuditRetention           = 90 * 24 * time.Hour
)

type ResolutionRecord struct {
	Schema           string              `json:"schema"`
	TransactionID    metadata.UUID       `json:"transaction_id"`
	Domain           policy.Domain       `json:"domain"`
	State            policy.PolicyState  `json:"state"`
	BundleGeneration uint64              `json:"bundle_generation"`
	PolicyGeneration uint64              `json:"policy_generation"`
	ManifestSHA256   string              `json:"manifest_sha256"`
	PayloadSHA256    string              `json:"payload_sha256"`
	ResolvedAt       string              `json:"resolved_at"`
	Reason           policy.PolicyReason `json:"reason"`
}

type AuditEntry struct {
	Domain           policy.Domain       `json:"domain"`
	State            policy.PolicyState  `json:"state"`
	BundleGeneration uint64              `json:"bundle_generation"`
	PolicyGeneration uint64              `json:"policy_generation"`
	ManifestSHA256   string              `json:"manifest_sha256"`
	PayloadSHA256    string              `json:"payload_sha256"`
	RecordedAt       string              `json:"recorded_at"`
	Reason           policy.PolicyReason `json:"reason"`
}

type AuditIndex struct {
	Schema  string       `json:"schema"`
	Entries []AuditEntry `json:"entries"`
}

type RetentionResult struct {
	RetainedValidGenerations int
	UnresolvedPrepares       int
	AuditEntries             int
	RemovedGenerationFiles   int
	RemovedStateRecords      int
	RemovedTransientFiles    int
}

func (record ResolutionRecord) Validate() error {
	if record.Schema != ResolutionSchema || !validTransactionID(record.TransactionID) ||
		validateResolutionFields(
			record.Domain,
			record.State,
			record.BundleGeneration,
			record.PolicyGeneration,
			record.ManifestSHA256,
			record.PayloadSHA256,
			record.ResolvedAt,
			record.Reason,
		) != nil {
		return ErrInvalidRecord
	}
	return nil
}

func validateResolutionFields(
	domain policy.Domain,
	state policy.PolicyState,
	bundleGeneration uint64,
	policyGeneration uint64,
	manifestSHA256 string,
	payloadSHA256 string,
	recordedAt string,
	reason policy.PolicyReason,
) error {
	if !domain.Valid() || bundleGeneration == 0 || policyGeneration == 0 ||
		!validDigest(manifestSHA256) || !validDigest(payloadSHA256) ||
		!validCanonicalUTC(recordedAt) || !reason.Valid() {
		return ErrInvalidRecord
	}
	switch state {
	case policy.PolicyActive:
		if reason != policy.ReasonNone {
			return ErrInvalidRecord
		}
	case policy.PolicyRejected:
		if reason == policy.ReasonNone {
			return ErrInvalidRecord
		}
	case policy.PolicyRestartRequired:
		if reason != policy.ReasonStaticMismatch {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

func (entry AuditEntry) Validate() error {
	return validateResolutionFields(
		entry.Domain,
		entry.State,
		entry.BundleGeneration,
		entry.PolicyGeneration,
		entry.ManifestSHA256,
		entry.PayloadSHA256,
		entry.RecordedAt,
		entry.Reason,
	)
}

func (index AuditIndex) Validate() error {
	if index.Schema != AuditIndexSchema || index.Entries == nil || len(index.Entries) > MaxAuditEntries {
		return ErrInvalidRecord
	}
	type auditGeneration struct {
		domain policy.Domain
		Generation
	}
	seen := make(map[auditGeneration]struct{}, len(index.Entries))
	for position, entry := range index.Entries {
		if entry.Validate() != nil {
			return ErrInvalidRecord
		}
		generation := auditGeneration{
			domain:     entry.Domain,
			Generation: Generation{Bundle: entry.BundleGeneration, Policy: entry.PolicyGeneration},
		}
		if _, exists := seen[generation]; exists {
			return ErrInvalidRecord
		}
		seen[generation] = struct{}{}
		if position > 0 && !auditEntryLess(index.Entries[position-1], entry) {
			return ErrInvalidRecord
		}
	}
	return nil
}

func (store *Store) ResolveGeneration(
	record ResolutionRecord,
	now time.Time,
) (RetentionResult, error) {
	if record.Validate() != nil || storeDomain(store) != record.Domain {
		return RetentionResult{}, ErrInvalidRecord
	}
	now, err := retentionNow(now)
	if err != nil {
		return RetentionResult{}, err
	}
	resolvedAt, _ := time.Parse(time.RFC3339Nano, record.ResolvedAt)
	if resolvedAt.After(now) {
		return RetentionResult{}, ErrInvalidRecord
	}
	encoded, err := marshalRecord(record)
	if err != nil {
		return RetentionResult{}, err
	}
	name, _ := transactionRecordFilename(recordResolution, record.TransactionID)

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return RetentionResult{}, err
	}
	if err := store.validateResolutionTransitionLocked(record); err != nil {
		return RetentionResult{}, err
	}
	if err := store.persistImmutableRecordLocked(recordResolution, name, encoded); err != nil {
		return RetentionResult{}, err
	}
	return store.applyRetentionLocked(now)
}

func (store *Store) ApplyRetention(now time.Time) (RetentionResult, error) {
	now, err := retentionNow(now)
	if err != nil {
		return RetentionResult{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return RetentionResult{}, err
	}
	return store.applyRetentionLocked(now)
}

func (store *Store) ReadAuditIndex() (AuditIndex, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return AuditIndex{}, err
	}
	index, _, err := store.loadAuditIndexLocked()
	return index, err
}

func (store *Store) validateResolutionTransitionLocked(record ResolutionRecord) error {
	pointerEncoded, err := store.readRecordLocked(activePointerFilename)
	if errors.Is(err, ErrRecordNotFound) {
		if record.State == policy.PolicyActive {
			return ErrRecordConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	pointer, err := decodeRecord[ActivePointer](pointerEncoded)
	if err != nil || pointer.Validate() != nil || pointer.Domain != store.domain {
		return ErrInvalidRecord
	}
	matches := resolutionMatchesPointer(record, pointer)
	if record.State == policy.PolicyActive && !matches {
		return ErrRecordConflict
	}
	if record.State != policy.PolicyActive && matches {
		return ErrRecordConflict
	}
	return nil
}

func (store *Store) applyRetentionLocked(now time.Time) (RetentionResult, error) {
	state, err := store.scanRetentionStateLocked()
	if err != nil {
		return RetentionResult{}, err
	}
	index, existingAudit, err := store.loadAuditIndexLocked()
	if err != nil {
		return RetentionResult{}, err
	}
	index, err = mergeAndPruneAudit(index, state.resolutions, now)
	if err != nil {
		return RetentionResult{}, err
	}
	encodedAudit, err := marshalRecord(index)
	if err != nil {
		return RetentionResult{}, err
	}
	if !bytes.Equal(existingAudit, encodedAudit) {
		if err := store.persistRecordLocked(recordAudit, auditIndexFilename, encodedAudit, true); err != nil {
			return RetentionResult{}, err
		}
	}

	keep, validCount, unresolvedCount, err := store.retainedGenerationsLocked(state)
	if err != nil {
		return RetentionResult{}, err
	}
	artifacts, err := store.scanGenerationArtifactsLocked()
	if err != nil {
		return RetentionResult{}, err
	}
	result := RetentionResult{
		RetainedValidGenerations: validCount,
		UnresolvedPrepares:       unresolvedCount,
		AuditEntries:             len(index.Entries),
	}

	generationDirectoryChanged := false
	for generation, names := range artifacts {
		if _, retained := keep[generation]; retained {
			continue
		}
		for _, name := range names {
			if err := store.removeGenerationArtifactLocked(name); err != nil {
				return RetentionResult{}, err
			}
			result.RemovedGenerationFiles++
			generationDirectoryChanged = true
		}
	}
	if generationDirectoryChanged && unix.Fsync(store.generationsFD) != nil {
		return RetentionResult{}, ErrStoreUnavailable
	}

	stateDirectoryChanged := false
	for transactionID, resolution := range state.resolutions {
		generation := generationFromResolution(resolution)
		_, retained := keep[generation]
		if resolution.State == policy.PolicyActive && retained {
			continue
		}
		for _, operation := range []recordOperation{recordResolution, recordPrepare, recordCommit} {
			name, _ := transactionRecordFilename(operation, transactionID)
			removed, removeErr := store.removeStateRecordIfPresentLocked(name)
			if removeErr != nil {
				return RetentionResult{}, removeErr
			}
			if removed {
				result.RemovedStateRecords++
				stateDirectoryChanged = true
			}
		}
	}
	for _, name := range state.transientNames {
		if err := store.removeTransientRecordLocked(name); err != nil {
			return RetentionResult{}, err
		}
		result.RemovedTransientFiles++
		stateDirectoryChanged = true
	}
	if stateDirectoryChanged && unix.Fsync(store.stateFD) != nil {
		return RetentionResult{}, ErrStoreUnavailable
	}
	if err := store.validatePathBindingLocked(); err != nil {
		return RetentionResult{}, err
	}
	return result, nil
}

type retentionState struct {
	receipts       map[metadata.UUID]PrepareReceipt
	commits        map[metadata.UUID]CommitIntent
	resolutions    map[metadata.UUID]ResolutionRecord
	actionLeases   map[metadata.UUID]policy.ActionLease
	actionNonces   map[metadata.UUID]actionNonceClaim
	transientNames []string
}

func (store *Store) scanRetentionStateLocked() (retentionState, error) {
	names, err := directoryEntryNames(store.stateFD)
	if err != nil {
		return retentionState{}, err
	}
	state := retentionState{
		receipts:     make(map[metadata.UUID]PrepareReceipt),
		commits:      make(map[metadata.UUID]CommitIntent),
		resolutions:  make(map[metadata.UUID]ResolutionRecord),
		actionLeases: make(map[metadata.UUID]policy.ActionLease),
		actionNonces: make(map[metadata.UUID]actionNonceClaim),
	}
	for _, name := range names {
		switch {
		case name == activePointerFilename || name == auditIndexFilename:
			continue
		case isTransientRecordName(name):
			state.transientNames = append(state.transientNames, name)
			continue
		}
		operation, transactionID, ok := parseTransactionRecordFilename(name)
		if !ok {
			return retentionState{}, ErrInvalidRecord
		}
		encoded, err := store.readRecordLocked(name)
		if err != nil {
			return retentionState{}, err
		}
		switch operation {
		case recordPrepare:
			receipt, decodeErr := decodeRecord[PrepareReceipt](encoded)
			if decodeErr != nil || receipt.Validate() != nil || receipt.Domain != store.domain ||
				receipt.TransactionID != transactionID {
				return retentionState{}, ErrInvalidRecord
			}
			state.receipts[transactionID] = receipt
		case recordCommit:
			intent, decodeErr := decodeRecord[CommitIntent](encoded)
			if decodeErr != nil || intent.Validate() != nil || intent.TransactionID != transactionID {
				return retentionState{}, ErrInvalidRecord
			}
			state.commits[transactionID] = intent
		case recordResolution:
			resolution, decodeErr := decodeRecord[ResolutionRecord](encoded)
			if decodeErr != nil || resolution.Validate() != nil || resolution.Domain != store.domain ||
				resolution.TransactionID != transactionID {
				return retentionState{}, ErrInvalidRecord
			}
			state.resolutions[transactionID] = resolution
		case recordActionLease:
			lease, decodeErr := decodeRecord[policy.ActionLease](encoded)
			if decodeErr != nil || lease.Validate() != nil || lease.Status != policy.LeasePending ||
				lease.Domain != store.domain || lease.ActionID != transactionID {
				return retentionState{}, ErrInvalidRecord
			}
			state.actionLeases[transactionID] = lease
		case recordActionNonce:
			claim, decodeErr := decodeRecord[actionNonceClaim](encoded)
			if decodeErr != nil || claim.Validate() != nil || claim.Domain != store.domain ||
				claim.Nonce != transactionID {
				return retentionState{}, ErrInvalidRecord
			}
			state.actionNonces[transactionID] = claim
		default:
			return retentionState{}, ErrInvalidRecord
		}
	}
	for _, lease := range state.actionLeases {
		claim, exists := state.actionNonces[lease.Nonce]
		if !exists || claim.ActionID != lease.ActionID || claim.Domain != lease.Domain ||
			claim.ClaimedAt != lease.IssuedAt {
			return retentionState{}, ErrRecordConflict
		}
	}
	return state, nil
}

func (store *Store) retainedGenerationsLocked(
	state retentionState,
) (map[Generation]struct{}, int, int, error) {
	owners := make(map[Generation]metadata.UUID)
	for transactionID, receipt := range state.receipts {
		generation := Generation{Bundle: receipt.BundleGeneration, Policy: receipt.PolicyGeneration}
		if owner, exists := owners[generation]; exists && owner != transactionID {
			return nil, 0, 0, ErrRecordConflict
		}
		owners[generation] = transactionID
	}
	valid := make([]ResolutionRecord, 0, len(state.resolutions))
	for transactionID, resolution := range state.resolutions {
		generation := generationFromResolution(resolution)
		if owner, exists := owners[generation]; exists && owner != transactionID {
			return nil, 0, 0, ErrRecordConflict
		}
		owners[generation] = transactionID
		if resolution.State != policy.PolicyActive {
			continue
		}
		receipt, receiptOK := state.receipts[transactionID]
		intent, intentOK := state.commits[transactionID]
		if !receiptOK || !intentOK || !receiptMatchesIntent(receipt, intent, store.domain) ||
			!resolutionMatchesReceipt(resolution, receipt) {
			return nil, 0, 0, ErrRecordConflict
		}
		valid = append(valid, resolution)
	}
	for transactionID := range state.commits {
		if _, receiptOK := state.receipts[transactionID]; receiptOK {
			continue
		}
		resolution, resolved := state.resolutions[transactionID]
		if !resolved || resolution.State == policy.PolicyActive {
			return nil, 0, 0, ErrRecordConflict
		}
	}
	sort.Slice(valid, func(left, right int) bool {
		if valid[left].BundleGeneration != valid[right].BundleGeneration {
			return valid[left].BundleGeneration > valid[right].BundleGeneration
		}
		return valid[left].PolicyGeneration > valid[right].PolicyGeneration
	})
	keep := make(map[Generation]struct{})
	validCount := len(valid)
	if validCount > RetainedValidGenerations {
		validCount = RetainedValidGenerations
	}
	for index := 0; index < validCount; index++ {
		keep[generationFromResolution(valid[index])] = struct{}{}
	}
	unresolvedCount := 0
	for transactionID, receipt := range state.receipts {
		if _, resolved := state.resolutions[transactionID]; resolved {
			continue
		}
		keep[Generation{Bundle: receipt.BundleGeneration, Policy: receipt.PolicyGeneration}] = struct{}{}
		unresolvedCount++
	}
	pointerEncoded, err := store.readRecordLocked(activePointerFilename)
	if err == nil {
		pointer, decodeErr := decodeRecord[ActivePointer](pointerEncoded)
		if decodeErr != nil || pointer.Validate() != nil || pointer.Domain != store.domain {
			return nil, 0, 0, ErrInvalidRecord
		}
		keep[Generation{Bundle: pointer.BundleGeneration, Policy: pointer.PolicyGeneration}] = struct{}{}
	} else if !errors.Is(err, ErrRecordNotFound) {
		return nil, 0, 0, err
	}
	return keep, validCount, unresolvedCount, nil
}

func (store *Store) loadAuditIndexLocked() (AuditIndex, []byte, error) {
	encoded, err := store.readRecordLocked(auditIndexFilename)
	if errors.Is(err, ErrRecordNotFound) {
		return AuditIndex{Schema: AuditIndexSchema, Entries: []AuditEntry{}}, nil, nil
	}
	if err != nil {
		return AuditIndex{}, nil, err
	}
	index, err := decodeRecord[AuditIndex](encoded)
	if err != nil || index.Validate() != nil {
		return AuditIndex{}, nil, ErrInvalidRecord
	}
	for _, entry := range index.Entries {
		if entry.Domain != store.domain {
			return AuditIndex{}, nil, ErrInvalidRecord
		}
	}
	return index, encoded, nil
}

func mergeAndPruneAudit(
	index AuditIndex,
	resolutions map[metadata.UUID]ResolutionRecord,
	now time.Time,
) (AuditIndex, error) {
	cutoff := now.Add(-AuditRetention)
	byGeneration := make(map[Generation]AuditEntry)
	for _, entry := range index.Entries {
		recordedAt, _ := time.Parse(time.RFC3339Nano, entry.RecordedAt)
		if recordedAt.After(now) {
			return AuditIndex{}, ErrInvalidRecord
		}
		if recordedAt.Before(cutoff) {
			continue
		}
		byGeneration[Generation{Bundle: entry.BundleGeneration, Policy: entry.PolicyGeneration}] = entry
	}
	for _, resolution := range resolutions {
		resolvedAt, _ := time.Parse(time.RFC3339Nano, resolution.ResolvedAt)
		if resolvedAt.After(now) {
			return AuditIndex{}, ErrInvalidRecord
		}
		if resolvedAt.Before(cutoff) {
			continue
		}
		entry := auditEntryFromResolution(resolution)
		generation := generationFromResolution(resolution)
		if current, exists := byGeneration[generation]; exists && current != entry {
			return AuditIndex{}, ErrRecordConflict
		}
		byGeneration[generation] = entry
	}
	entries := make([]AuditEntry, 0, len(byGeneration))
	for _, entry := range byGeneration {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool { return auditEntryLess(entries[left], entries[right]) })
	if len(entries) > MaxAuditEntries {
		entries = append([]AuditEntry(nil), entries[len(entries)-MaxAuditEntries:]...)
	}
	result := AuditIndex{Schema: AuditIndexSchema, Entries: entries}
	if result.Validate() != nil {
		return AuditIndex{}, ErrInvalidRecord
	}
	return result, nil
}

func (store *Store) scanGenerationArtifactsLocked() (map[Generation][]string, error) {
	names, err := directoryEntryNames(store.generationsFD)
	if err != nil {
		return nil, err
	}
	artifacts := make(map[Generation][]string)
	for _, name := range names {
		generation, _, ok := parseGenerationFilename(store.domain, name)
		if !ok {
			return nil, ErrInvalidArtifact
		}
		artifacts[generation] = append(artifacts[generation], name)
	}
	return artifacts, nil
}

func (store *Store) removeGenerationArtifactLocked(name string) error {
	if err := store.classifyExistingLocked(name); !errors.Is(err, ErrGenerationExists) {
		return err
	}
	if unix.Unlinkat(store.generationsFD, name, 0) != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func (store *Store) removeStateRecordIfPresentLocked(name string) (bool, error) {
	if _, err := store.readRecordLocked(name); errors.Is(err, ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if unix.Unlinkat(store.stateFD, name, 0) != nil {
		return false, ErrStoreUnavailable
	}
	return true, nil
}

func (store *Store) removeTransientRecordLocked(name string) error {
	if !isTransientRecordName(name) {
		return ErrInvalidRecord
	}
	fd, err := unix.Openat(
		store.stateFD, name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return ErrInsecureRecord
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != store.expectedUID || stat.Gid != store.expectedGID || stat.Nlink != 1 ||
		(os.FileMode(stat.Mode).Perm() != 0o600 && os.FileMode(stat.Mode).Perm() != RecordFileMode) ||
		stat.Size < 0 || stat.Size > MaxRecordSize {
		return ErrInsecureRecord
	}
	if unix.Unlinkat(store.stateFD, name, 0) != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func directoryEntryNames(directoryFD int) ([]string, error) {
	fd, err := unix.Openat(
		directoryFD, ".",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, ErrStoreUnavailable
	}
	directory := os.NewFile(uintptr(fd), "policy-store-directory")
	if directory == nil {
		_ = unix.Close(fd)
		return nil, ErrStoreUnavailable
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, ErrStoreUnavailable
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func parseTransactionRecordFilename(name string) (recordOperation, metadata.UUID, bool) {
	for _, operation := range []recordOperation{
		recordPrepare, recordCommit, recordResolution, recordActionLease, recordActionNonce,
	} {
		prefix := string(operation) + "-"
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
		transactionID, err := metadata.ParseUUID(value)
		if err != nil {
			return "", "", false
		}
		expected, _ := transactionRecordFilename(operation, transactionID)
		return operation, transactionID, expected == name
	}
	return "", "", false
}

func parseGenerationFilename(
	domain policy.Domain,
	name string,
) (Generation, ArtifactKind, bool) {
	parts := strings.Split(name, "-")
	if len(parts) != 5 || parts[0] != "bundle" || parts[2] != string(domain) ||
		len(parts[1]) != 20 || len(parts[3]) != 20 || !strings.HasSuffix(parts[4], ".json") {
		return Generation{}, "", false
	}
	bundle, bundleErr := strconv.ParseUint(parts[1], 10, 64)
	policyGeneration, policyErr := strconv.ParseUint(parts[3], 10, 64)
	kind := ArtifactKind(strings.TrimSuffix(parts[4], ".json"))
	if bundleErr != nil || policyErr != nil || bundle == 0 || policyGeneration == 0 {
		return Generation{}, "", false
	}
	generation := Generation{Bundle: bundle, Policy: policyGeneration}
	expected, err := generationFilename(domain, generation, kind)
	return generation, kind, err == nil && expected == name
}

func isTransientRecordName(name string) bool {
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".tmp") {
		return false
	}
	withoutSuffix := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".tmp")
	separator := strings.LastIndexByte(withoutSuffix, '.')
	if separator <= 0 {
		return false
	}
	target := withoutSuffix[:separator]
	if _, err := metadata.ParseUUID(withoutSuffix[separator+1:]); err != nil {
		return false
	}
	if target == activePointerFilename || target == auditIndexFilename {
		return true
	}
	_, _, ok := parseTransactionRecordFilename(target)
	return ok
}

func auditEntryFromResolution(record ResolutionRecord) AuditEntry {
	return AuditEntry{
		Domain: record.Domain, State: record.State,
		BundleGeneration: record.BundleGeneration, PolicyGeneration: record.PolicyGeneration,
		ManifestSHA256: record.ManifestSHA256, PayloadSHA256: record.PayloadSHA256,
		RecordedAt: record.ResolvedAt, Reason: record.Reason,
	}
}

func auditEntryLess(left, right AuditEntry) bool {
	if left.RecordedAt != right.RecordedAt {
		return left.RecordedAt < right.RecordedAt
	}
	if left.Domain != right.Domain {
		return left.Domain < right.Domain
	}
	if left.BundleGeneration != right.BundleGeneration {
		return left.BundleGeneration < right.BundleGeneration
	}
	return left.PolicyGeneration < right.PolicyGeneration
}

func generationFromResolution(record ResolutionRecord) Generation {
	return Generation{Bundle: record.BundleGeneration, Policy: record.PolicyGeneration}
}

func resolutionMatchesReceipt(record ResolutionRecord, receipt PrepareReceipt) bool {
	return record.TransactionID == receipt.TransactionID && record.Domain == receipt.Domain &&
		record.BundleGeneration == receipt.BundleGeneration &&
		record.PolicyGeneration == receipt.PolicyGeneration &&
		record.ManifestSHA256 == receipt.ManifestSHA256 &&
		record.PayloadSHA256 == receipt.PayloadSHA256
}

func resolutionMatchesPointer(record ResolutionRecord, pointer ActivePointer) bool {
	return record.TransactionID == pointer.TransactionID && record.Domain == pointer.Domain &&
		record.BundleGeneration == pointer.BundleGeneration &&
		record.PolicyGeneration == pointer.PolicyGeneration &&
		record.ManifestSHA256 == pointer.ManifestSHA256 &&
		record.PayloadSHA256 == pointer.PayloadSHA256
}

func retentionNow(now time.Time) (time.Time, error) {
	if now.IsZero() {
		return time.Time{}, ErrInvalidRecord
	}
	return now.UTC(), nil
}
