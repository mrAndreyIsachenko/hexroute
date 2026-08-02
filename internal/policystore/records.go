package policystore

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"golang.org/x/sys/unix"
)

const (
	PrepareReceiptSchema = "hexroute.policy-prepare-receipt.v1"
	CommitIntentSchema   = "hexroute.policy-commit-intent.v1"
	ActivePointerSchema  = "hexroute.policy-active-pointer.v1"
	RecordFileMode       = os.FileMode(0o400)
	MaxRecordSize        = 64 * 1024

	activePointerFilename = "active.json"
	auditIndexFilename    = "audit.json"
)

type PrepareReceipt struct {
	Schema           string        `json:"schema"`
	TransactionID    metadata.UUID `json:"transaction_id"`
	Domain           policy.Domain `json:"domain"`
	BundleGeneration uint64        `json:"bundle_generation"`
	PolicyGeneration uint64        `json:"policy_generation"`
	ManifestSHA256   string        `json:"manifest_sha256"`
	PayloadSHA256    string        `json:"payload_sha256"`
	ApprovalSHA256   string        `json:"approval_sha256"`
	PreparedAt       string        `json:"prepared_at"`
}

// CommitIntent carries the one user-presence approval that authorizes the
// referenced bundle. Transaction identity and timestamps are local correlation
// metadata and cannot expand the candidate covered by the embedded signature.
type CommitIntent struct {
	Schema               string                        `json:"schema"`
	TransactionID        metadata.UUID                 `json:"transaction_id"`
	BundleGeneration     uint64                        `json:"bundle_generation"`
	RootPolicyGeneration uint64                        `json:"root_policy_generation"`
	UserPolicyGeneration uint64                        `json:"user_policy_generation"`
	ManifestSHA256       string                        `json:"manifest_sha256"`
	RootPayloadSHA256    string                        `json:"root_payload_sha256"`
	UserPayloadSHA256    string                        `json:"user_payload_sha256"`
	ApprovalSHA256       string                        `json:"approval_sha256"`
	Approval             policyapproval.SignedApproval `json:"approval"`
	CreatedAt            string                        `json:"created_at"`
}

// ActivePointer carries the embedded approval and the immutable commit-intent
// digest. Full signature and candidate verification is deliberately performed
// by the daemon verification layer, not by storage persistence.
type ActivePointer struct {
	Schema             string                        `json:"schema"`
	TransactionID      metadata.UUID                 `json:"transaction_id"`
	Domain             policy.Domain                 `json:"domain"`
	BundleGeneration   uint64                        `json:"bundle_generation"`
	PolicyGeneration   uint64                        `json:"policy_generation"`
	ManifestSHA256     string                        `json:"manifest_sha256"`
	PayloadSHA256      string                        `json:"payload_sha256"`
	ApprovalSHA256     string                        `json:"approval_sha256"`
	CommitIntentSHA256 string                        `json:"commit_intent_sha256"`
	Approval           policyapproval.SignedApproval `json:"approval"`
	ActivatedAt        string                        `json:"activated_at"`
	ConfirmedAt        string                        `json:"confirmed_at,omitempty"`
}

type recordOperation string

const (
	recordPrepare    recordOperation = "prepare"
	recordCommit     recordOperation = "commit"
	recordActive     recordOperation = "active"
	recordResolution recordOperation = "resolution"
	recordAudit      recordOperation = "audit"
)

type persistenceBoundary string

const (
	boundaryBeforeFileSync      persistenceBoundary = "before_file_fsync"
	boundaryAfterFileSync       persistenceBoundary = "after_file_fsync"
	boundaryBeforeRename        persistenceBoundary = "before_rename"
	boundaryAfterRename         persistenceBoundary = "after_rename"
	boundaryBeforeDirectorySync persistenceBoundary = "before_directory_fsync"
	boundaryAfterDirectorySync  persistenceBoundary = "after_directory_fsync"
)

type persistenceFault func(recordOperation, persistenceBoundary) error

var (
	ErrInvalidRecord          = errors.New("invalid policy state record")
	ErrInsecureRecord         = errors.New("policy state record ownership or mode is invalid")
	ErrRecordNotFound         = errors.New("policy state record not found")
	ErrRecordConflict         = errors.New("policy state record conflicts with immutable evidence")
	ErrStaleActivePointer     = errors.New("policy active pointer would not advance")
	ErrPersistenceInterrupted = errors.New("policy state persistence interrupted")
)

func (receipt PrepareReceipt) Validate() error {
	if receipt.Schema != PrepareReceiptSchema || !validTransactionID(receipt.TransactionID) ||
		!receipt.Domain.Valid() || receipt.BundleGeneration == 0 || receipt.PolicyGeneration == 0 ||
		!validDigest(receipt.ManifestSHA256) || !validDigest(receipt.PayloadSHA256) ||
		!validDigest(receipt.ApprovalSHA256) || !validCanonicalUTC(receipt.PreparedAt) {
		return ErrInvalidRecord
	}
	return nil
}

func (intent CommitIntent) Validate() error {
	if intent.Schema != CommitIntentSchema || !validTransactionID(intent.TransactionID) ||
		intent.BundleGeneration == 0 || intent.RootPolicyGeneration == 0 ||
		intent.UserPolicyGeneration == 0 || !validDigest(intent.ManifestSHA256) ||
		!validDigest(intent.RootPayloadSHA256) || !validDigest(intent.UserPayloadSHA256) ||
		!validDigest(intent.ApprovalSHA256) || !validCanonicalUTC(intent.CreatedAt) ||
		validateApprovalStructure(intent.Approval) != nil {
		return ErrInvalidRecord
	}
	statement := intent.Approval.Statement
	if statement.ManifestSHA256 != intent.ManifestSHA256 ||
		statement.RootSHA256 != intent.RootPayloadSHA256 ||
		statement.UserSHA256 != intent.UserPayloadSHA256 {
		return ErrInvalidRecord
	}
	digest, err := approvalSHA256(intent.Approval)
	if err != nil || digest != intent.ApprovalSHA256 {
		return ErrInvalidRecord
	}
	return nil
}

func (pointer ActivePointer) Validate() error {
	if pointer.Schema != ActivePointerSchema || !validTransactionID(pointer.TransactionID) ||
		!pointer.Domain.Valid() || pointer.BundleGeneration == 0 || pointer.PolicyGeneration == 0 ||
		!validDigest(pointer.ManifestSHA256) || !validDigest(pointer.PayloadSHA256) ||
		!validDigest(pointer.ApprovalSHA256) || !validDigest(pointer.CommitIntentSHA256) ||
		!validCanonicalUTC(pointer.ActivatedAt) || validateApprovalStructure(pointer.Approval) != nil {
		return ErrInvalidRecord
	}
	if pointer.ConfirmedAt != "" {
		activatedAt, activatedErr := time.Parse(time.RFC3339Nano, pointer.ActivatedAt)
		confirmedAt, confirmedErr := time.Parse(time.RFC3339Nano, pointer.ConfirmedAt)
		if activatedErr != nil || confirmedErr != nil ||
			confirmedAt.UTC().Format(time.RFC3339Nano) != pointer.ConfirmedAt ||
			confirmedAt.Before(activatedAt) {
			return ErrInvalidRecord
		}
	}
	statement := pointer.Approval.Statement
	wantPayload := statement.RootSHA256
	if pointer.Domain == policy.DomainUser {
		wantPayload = statement.UserSHA256
	}
	if statement.ManifestSHA256 != pointer.ManifestSHA256 || wantPayload != pointer.PayloadSHA256 {
		return ErrInvalidRecord
	}
	digest, err := approvalSHA256(pointer.Approval)
	if err != nil || digest != pointer.ApprovalSHA256 {
		return ErrInvalidRecord
	}
	return nil
}

func CommitIntentSHA256(intent CommitIntent) (string, error) {
	if intent.Validate() != nil {
		return "", ErrInvalidRecord
	}
	digest, _, err := policy.CanonicalSHA256(intent)
	if err != nil {
		return "", ErrInvalidRecord
	}
	return digest, nil
}

func (store *Store) PersistPrepareReceipt(receipt PrepareReceipt) error {
	if receipt.Validate() != nil || storeDomain(store) != receipt.Domain {
		return ErrInvalidRecord
	}
	encoded, err := marshalRecord(receipt)
	if err != nil {
		return err
	}
	name, err := transactionRecordFilename(recordPrepare, receipt.TransactionID)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	return store.persistImmutableRecordLocked(recordPrepare, name, encoded)
}

func (store *Store) ReadPrepareReceipt(transactionID metadata.UUID) (PrepareReceipt, error) {
	name, err := transactionRecordFilename(recordPrepare, transactionID)
	if err != nil {
		return PrepareReceipt{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return PrepareReceipt{}, err
	}
	encoded, err := store.readRecordLocked(name)
	if err != nil {
		return PrepareReceipt{}, err
	}
	receipt, err := decodeRecord[PrepareReceipt](encoded)
	if err != nil || receipt.Validate() != nil || receipt.Domain != store.domain ||
		receipt.TransactionID != transactionID {
		return PrepareReceipt{}, ErrInvalidRecord
	}
	return receipt, nil
}

func (store *Store) PersistCommitIntent(intent CommitIntent) error {
	if intent.Validate() != nil {
		return ErrInvalidRecord
	}
	encoded, err := marshalRecord(intent)
	if err != nil {
		return err
	}
	name, err := transactionRecordFilename(recordCommit, intent.TransactionID)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	receiptName, _ := transactionRecordFilename(recordPrepare, intent.TransactionID)
	receiptEncoded, err := store.readRecordLocked(receiptName)
	if err != nil {
		return err
	}
	receipt, err := decodeRecord[PrepareReceipt](receiptEncoded)
	if err != nil || receipt.Validate() != nil || !receiptMatchesIntent(receipt, intent, store.domain) {
		return ErrRecordConflict
	}
	return store.persistImmutableRecordLocked(recordCommit, name, encoded)
}

func (store *Store) ReadCommitIntent(transactionID metadata.UUID) (CommitIntent, error) {
	name, err := transactionRecordFilename(recordCommit, transactionID)
	if err != nil {
		return CommitIntent{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return CommitIntent{}, err
	}
	encoded, err := store.readRecordLocked(name)
	if err != nil {
		return CommitIntent{}, err
	}
	intent, err := decodeRecord[CommitIntent](encoded)
	if err != nil || intent.Validate() != nil || intent.TransactionID != transactionID {
		return CommitIntent{}, ErrInvalidRecord
	}
	return intent, nil
}

func (store *Store) PersistActivePointer(pointer ActivePointer) error {
	if pointer.Validate() != nil || storeDomain(store) != pointer.Domain {
		return ErrInvalidRecord
	}
	encoded, err := marshalRecord(pointer)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return err
	}
	intentName, _ := transactionRecordFilename(recordCommit, pointer.TransactionID)
	intentEncoded, err := store.readRecordLocked(intentName)
	if err != nil {
		return err
	}
	intent, err := decodeRecord[CommitIntent](intentEncoded)
	if err != nil || intent.Validate() != nil || !pointerMatchesIntent(pointer, intent) {
		return ErrRecordConflict
	}
	existing, err := store.readRecordLocked(activePointerFilename)
	switch {
	case err == nil:
		if bytes.Equal(existing, encoded) {
			return nil
		}
		current, decodeErr := decodeRecord[ActivePointer](existing)
		if decodeErr != nil || current.Validate() != nil || current.Domain != store.domain {
			return ErrInvalidRecord
		}
		if pointer.BundleGeneration == current.BundleGeneration &&
			activePointerConfirmationAdvance(current, pointer) {
			break
		}
		if pointer.BundleGeneration <= current.BundleGeneration {
			return ErrStaleActivePointer
		}
	case errors.Is(err, ErrRecordNotFound):
	case err != nil:
		return err
	}
	return store.persistRecordLocked(recordActive, activePointerFilename, encoded, true)
}

func activePointerConfirmationAdvance(current, next ActivePointer) bool {
	currentConfirmed := current.ConfirmedAt
	nextConfirmed := next.ConfirmedAt
	current.ConfirmedAt = ""
	next.ConfirmedAt = ""
	return current == next && currentConfirmed == "" && nextConfirmed != ""
}

func (store *Store) ReadActivePointer() (ActivePointer, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validateOpenLocked(); err != nil {
		return ActivePointer{}, err
	}
	encoded, err := store.readRecordLocked(activePointerFilename)
	if err != nil {
		return ActivePointer{}, err
	}
	pointer, err := decodeRecord[ActivePointer](encoded)
	if err != nil || pointer.Validate() != nil || pointer.Domain != store.domain {
		return ActivePointer{}, ErrInvalidRecord
	}
	return pointer, nil
}

func (store *Store) persistImmutableRecordLocked(
	operation recordOperation,
	name string,
	encoded []byte,
) error {
	existing, err := store.readRecordLocked(name)
	switch {
	case err == nil:
		if bytes.Equal(existing, encoded) {
			return nil
		}
		return ErrRecordConflict
	case errors.Is(err, ErrRecordNotFound):
	case err != nil:
		return err
	}
	err = store.persistRecordLocked(operation, name, encoded, false)
	if !errors.Is(err, unix.EEXIST) {
		return err
	}
	existing, readErr := store.readRecordLocked(name)
	if readErr != nil {
		return readErr
	}
	if bytes.Equal(existing, encoded) {
		return nil
	}
	return ErrRecordConflict
}

func (store *Store) persistRecordLocked(
	operation recordOperation,
	name string,
	encoded []byte,
	replace bool,
) error {
	if len(encoded) == 0 || len(encoded) > MaxRecordSize {
		return ErrInvalidRecord
	}
	temporaryID, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		return ErrStoreUnavailable
	}
	temporaryName := "." + name + "." + string(temporaryID) + ".tmp"
	fd, err := unix.Openat(
		store.stateFD,
		temporaryName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0o600,
	)
	if err != nil {
		return ErrStoreUnavailable
	}
	preserveTemporary := false
	defer func() {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if temporaryName != "" && !preserveTemporary {
			_ = unix.Unlinkat(store.stateFD, temporaryName, 0)
		}
	}()

	if writeAll(fd, encoded) != nil ||
		unix.Fchmod(fd, uint32(RecordFileMode.Perm())) != nil ||
		validateRecordFD(fd, store.expectedUID, store.expectedGID) != nil {
		return ErrStoreUnavailable
	}
	if store.injectPersistenceFault(operation, boundaryBeforeFileSync) {
		preserveTemporary = true
		return ErrPersistenceInterrupted
	}
	if unix.Fsync(fd) != nil {
		return ErrStoreUnavailable
	}
	if store.injectPersistenceFault(operation, boundaryAfterFileSync) {
		preserveTemporary = true
		return ErrPersistenceInterrupted
	}
	if unix.Close(fd) != nil {
		fd = -1
		return ErrStoreUnavailable
	}
	fd = -1
	if store.injectPersistenceFault(operation, boundaryBeforeRename) {
		preserveTemporary = true
		return ErrPersistenceInterrupted
	}
	if replace {
		err = unix.Renameat(store.stateFD, temporaryName, store.stateFD, name)
	} else {
		err = renameNoReplaceAt(store.stateFD, temporaryName, store.stateFD, name)
	}
	if err != nil {
		if !replace && errors.Is(err, unix.EEXIST) {
			return unix.EEXIST
		}
		return ErrStoreUnavailable
	}
	temporaryName = ""
	if store.injectPersistenceFault(operation, boundaryAfterRename) {
		return ErrPersistenceInterrupted
	}
	final, err := store.readRecordLocked(name)
	if err != nil || !bytes.Equal(final, encoded) {
		return ErrStoreUnavailable
	}
	if store.injectPersistenceFault(operation, boundaryBeforeDirectorySync) {
		return ErrPersistenceInterrupted
	}
	if unix.Fsync(store.stateFD) != nil {
		return ErrStoreUnavailable
	}
	if store.injectPersistenceFault(operation, boundaryAfterDirectorySync) {
		return ErrPersistenceInterrupted
	}
	return store.validatePathBindingLocked()
}

func (store *Store) readRecordLocked(name string) ([]byte, error) {
	fd, err := unix.Openat(
		store.stateFD,
		name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, ErrInsecureRecord
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrStoreUnavailable
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || validateRecordStat(stat, store.expectedUID, store.expectedGID) != nil {
		return nil, ErrInsecureRecord
	}
	encoded, err := io.ReadAll(io.LimitReader(file, MaxRecordSize+1))
	if err != nil || len(encoded) == 0 || len(encoded) > MaxRecordSize || int64(len(encoded)) != stat.Size {
		return nil, ErrInvalidRecord
	}
	if err := store.validatePathBindingLocked(); err != nil {
		return nil, err
	}
	return encoded, nil
}

func transactionRecordFilename(operation recordOperation, transactionID metadata.UUID) (string, error) {
	if !validTransactionID(transactionID) {
		return "", ErrInvalidRecord
	}
	switch operation {
	case recordPrepare, recordCommit, recordResolution:
		return string(operation) + "-" + string(transactionID) + ".json", nil
	default:
		return "", ErrInvalidRecord
	}
}

func marshalRecord(value any) ([]byte, error) {
	encoded, err := policy.MarshalCanonical(value)
	if err != nil || len(encoded) == 0 || len(encoded) > MaxRecordSize {
		return nil, ErrInvalidRecord
	}
	return encoded, nil
}

func decodeRecord[T any](encoded []byte) (T, error) {
	var value T
	if len(encoded) == 0 || len(encoded) > MaxRecordSize {
		return value, ErrInvalidRecord
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, ErrInvalidRecord
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, ErrInvalidRecord
	}
	canonical, err := policy.MarshalCanonical(value)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return value, ErrInvalidRecord
	}
	return value, nil
}

func validateRecordFD(fd int, uid, gid uint32) error {
	var stat unix.Stat_t
	if fd < 0 || unix.Fstat(fd, &stat) != nil {
		return ErrInsecureRecord
	}
	return validateRecordStat(stat, uid, gid)
}

func validateRecordStat(stat unix.Stat_t, uid, gid uint32) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || os.FileMode(stat.Mode).Perm() != RecordFileMode ||
		stat.Uid != uid || stat.Gid != gid || stat.Nlink != 1 ||
		stat.Size <= 0 || stat.Size > MaxRecordSize {
		return ErrInsecureRecord
	}
	return nil
}

func validateApprovalStructure(approval policyapproval.SignedApproval) error {
	statement := approval.Statement
	if statement.Schema != policyapproval.ApprovalSchema ||
		!validDigest(statement.ManifestSHA256) || !validDigest(statement.RootSHA256) ||
		!validDigest(statement.UserSHA256) || !validDigest(statement.ReviewSHA256) ||
		!validDigest(statement.SignerFingerprint) || !validCanonicalUTC(statement.NotBefore) ||
		!validCanonicalUTC(statement.ExpiresAt) {
		return ErrInvalidRecord
	}
	notBefore, _ := time.Parse(time.RFC3339Nano, statement.NotBefore)
	expiresAt, _ := time.Parse(time.RFC3339Nano, statement.ExpiresAt)
	if !expiresAt.After(notBefore) {
		return ErrInvalidRecord
	}
	signature, err := base64.RawURLEncoding.DecodeString(approval.Signature)
	if err != nil || len(signature) != 64 {
		return ErrInvalidRecord
	}
	return nil
}

func approvalSHA256(approval policyapproval.SignedApproval) (string, error) {
	digest, err := policyapproval.ApprovalSHA256(approval)
	if err != nil {
		return "", ErrInvalidRecord
	}
	return digest, nil
}

func receiptMatchesIntent(receipt PrepareReceipt, intent CommitIntent, domain policy.Domain) bool {
	if receipt.TransactionID != intent.TransactionID || receipt.Domain != domain ||
		receipt.BundleGeneration != intent.BundleGeneration ||
		receipt.ManifestSHA256 != intent.ManifestSHA256 ||
		receipt.ApprovalSHA256 != intent.ApprovalSHA256 {
		return false
	}
	if domain == policy.DomainRoot {
		return receipt.PolicyGeneration == intent.RootPolicyGeneration &&
			receipt.PayloadSHA256 == intent.RootPayloadSHA256
	}
	return receipt.PolicyGeneration == intent.UserPolicyGeneration &&
		receipt.PayloadSHA256 == intent.UserPayloadSHA256
}

func pointerMatchesIntent(pointer ActivePointer, intent CommitIntent) bool {
	if pointer.TransactionID != intent.TransactionID ||
		pointer.BundleGeneration != intent.BundleGeneration ||
		pointer.ManifestSHA256 != intent.ManifestSHA256 ||
		pointer.ApprovalSHA256 != intent.ApprovalSHA256 {
		return false
	}
	intentDigest, err := CommitIntentSHA256(intent)
	if err != nil || pointer.CommitIntentSHA256 != intentDigest {
		return false
	}
	if pointer.Domain == policy.DomainRoot {
		return pointer.PolicyGeneration == intent.RootPolicyGeneration &&
			pointer.PayloadSHA256 == intent.RootPayloadSHA256
	}
	return pointer.PolicyGeneration == intent.UserPolicyGeneration &&
		pointer.PayloadSHA256 == intent.UserPayloadSHA256
}

func validTransactionID(transactionID metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(transactionID))
	return err == nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == string(bytes.ToLower([]byte(value)))
}

func validCanonicalUTC(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func (store *Store) injectPersistenceFault(operation recordOperation, boundary persistenceBoundary) bool {
	return store.persistenceFault != nil && store.persistenceFault(operation, boundary) != nil
}
