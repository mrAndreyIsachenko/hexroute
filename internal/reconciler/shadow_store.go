package reconciler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	RootShadowStorePath         = "/Library/Application Support/Hexroute/reconciler-root"
	UserShadowStoreRelativePath = "Library/Application Support/Hexroute/reconciler-user"

	shadowAttemptJournalFile = "action-attempts.jsonl"
)

type ShadowStoreConfig struct {
	Domain      policy.Domain
	RootPath    string
	ExpectedUID uint32
	ExpectedGID uint32
	PeerUID     uint32
	Registry    Registry
}

type ShadowStore struct {
	domain      policy.Domain
	rootPath    string
	storeSHA256 string
	peerUID     uint32
	registry    Registry
	attempts    *FileAttemptJournal
}

var ErrShadowStore = errors.New("invalid reconciler shadow store")

func RootShadowStoreConfig(peerUID uint32) ShadowStoreConfig {
	return ShadowStoreConfig{
		Domain:      policy.DomainRoot,
		RootPath:    RootShadowStorePath,
		ExpectedUID: 0,
		ExpectedGID: 0,
		PeerUID:     peerUID,
		Registry:    DefaultSyntheticRegistry(),
	}
}

func UserShadowStorePath(home string) (string, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "", ErrShadowStore
	}
	return filepath.Join(home, UserShadowStoreRelativePath), nil
}

func OpenShadowStore(config ShadowStoreConfig) (*ShadowStore, error) {
	if config.validate() != nil {
		return nil, ErrShadowStore
	}
	if err := ensureShadowDirectory(config.RootPath, config.ExpectedUID, config.ExpectedGID); err != nil {
		return nil, err
	}
	journalPath := filepath.Join(config.RootPath, shadowAttemptJournalFile)
	journal, err := OpenFileAttemptJournal(journalPath)
	if err != nil {
		return nil, err
	}
	if err := validateShadowFile(journalPath, config.ExpectedUID, config.ExpectedGID); err != nil {
		return nil, err
	}
	return &ShadowStore{
		domain:      config.Domain,
		rootPath:    filepath.Clean(config.RootPath),
		storeSHA256: policy.SHA256Hex([]byte(filepath.Clean(config.RootPath))),
		peerUID:     config.PeerUID,
		registry:    config.Registry,
		attempts:    journal,
	}, nil
}

func ValidateDisjointShadowStores(root, user ShadowStoreConfig) error {
	if root.Domain != policy.DomainRoot ||
		user.Domain != policy.DomainUser ||
		invalidPath(root.RootPath) ||
		invalidPath(user.RootPath) ||
		pathsOverlap(root.RootPath, user.RootPath) {
		return ErrShadowStore
	}
	return nil
}

func (store *ShadowStore) Domain() policy.Domain {
	if store == nil {
		return ""
	}
	return store.domain
}

func (store *ShadowStore) RootPath() string {
	if store == nil {
		return ""
	}
	return store.rootPath
}

func (store *ShadowStore) PeerUID() uint32 {
	if store == nil {
		return 0
	}
	return store.peerUID
}

func (store *ShadowStore) AppendPending(
	binding AttemptBinding,
	reason Reason,
) (AttemptJournalEntry, error) {
	if store == nil || store.attempts == nil || binding.Domain != store.domain {
		return AttemptJournalEntry{}, ErrShadowStore
	}
	return store.attempts.AppendPending(binding, reason)
}

func (store *ShadowStore) CompareAndSwap(
	binding AttemptBinding,
	from AttemptState,
	to AttemptState,
	reason Reason,
) (AttemptJournalEntry, error) {
	if store == nil || store.attempts == nil || binding.Domain != store.domain {
		return AttemptJournalEntry{}, ErrShadowStore
	}
	return store.attempts.CompareAndSwap(binding, from, to, reason)
}

func (store *ShadowStore) Latest(
	actionID metadata.UUID,
) (AttemptJournalEntry, bool, error) {
	if store == nil || store.attempts == nil {
		return AttemptJournalEntry{}, false, ErrShadowStore
	}
	entry, exists, err := store.attempts.Latest(actionID)
	if err != nil || !exists {
		return entry, exists, err
	}
	if entry.Binding.Domain != store.domain {
		return AttemptJournalEntry{}, false, ErrShadowStore
	}
	return entry, true, nil
}

func (store *ShadowStore) HandleIPC(_ context.Context, request ipc.Request) ipc.Response {
	response := ipc.Response{Version: ipc.ProtocolVersion, RequestID: request.RequestID}
	if store == nil ||
		request.Action != ipc.ActionReconcilerShadowStatus ||
		request.ReconcilerShadowStatus == nil ||
		request.Validate() != nil {
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
	status, err := store.ipcStatus()
	if err != nil {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	response.OK = true
	response.ReconcilerShadowStatus = &status
	return response
}

func (store *ShadowStore) ipcStatus() (ipc.ReconcilerShadowStatusResult, error) {
	if store == nil || !store.domain.Valid() || !store.registry.SyntheticOnly() {
		return ipc.ReconcilerShadowStatusResult{}, ErrShadowStore
	}
	ids := store.registry.IDs()
	capabilityIDs := make([]string, len(ids))
	for index, id := range ids {
		capabilityIDs[index] = string(id)
	}
	role := ipc.RoleRoot
	if store.domain == policy.DomainUser {
		role = ipc.RoleUser
	}
	result := ipc.ReconcilerShadowStatusResult{
		Domain:              store.domain,
		Role:                role,
		StoreSHA256:         store.storeSHA256,
		ShadowIPC:           true,
		SyntheticOnly:       true,
		ProposalTranslation: false,
		ExecutionIPC:        false,
		CapabilityIDs:       capabilityIDs,
	}
	if result.Validate() != nil {
		return ipc.ReconcilerShadowStatusResult{}, ErrShadowStore
	}
	return result, nil
}

func (config ShadowStoreConfig) validate() error {
	if !config.Domain.Valid() ||
		invalidPath(config.RootPath) ||
		!config.Registry.SyntheticOnly() {
		return ErrShadowStore
	}
	return nil
}

func ensureShadowDirectory(path string, uid, gid uint32) error {
	if invalidPath(path) {
		return ErrShadowStore
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 {
		return ErrShadowStore
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid || stat.Gid != gid {
		return ErrShadowStore
	}
	if info.Mode().Perm() != 0o700 {
		return os.Chmod(path, 0o700)
	}
	return nil
}

func validateShadowFile(path string, uid, gid uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 {
		return ErrShadowStore
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid || stat.Gid != gid {
		return ErrShadowStore
	}
	return nil
}

func invalidPath(path string) bool {
	return path == "" ||
		!filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		path == string(filepath.Separator)
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	if parent == child {
		return true
	}
	relative, err := filepath.Rel(parent, child)
	return err == nil &&
		relative != "." &&
		relative != "" &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		relative != ".."
}
