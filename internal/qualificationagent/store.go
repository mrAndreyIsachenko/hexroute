package qualificationagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
	"golang.org/x/sys/unix"
)

type store struct {
	root     string
	stateDir string
	sessions string
}

type sourceStore struct {
	root string
}

func openStore(root string) (*store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrInvalidConfig
	}
	result := &store{
		root: root, stateDir: filepath.Join(root, "state"),
		sessions: filepath.Join(root, "sessions"),
	}
	for _, directory := range []string{result.root, result.stateDir, result.sessions} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (store *store) lock() (*os.File, error) {
	path := filepath.Join(store.stateDir, lockFilename)
	file, err := openPrivateFile(path, unix.O_RDWR|unix.O_CREAT, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func unlock(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func (store *store) readState() (State, error) {
	path := filepath.Join(store.stateDir, stateFilename)
	file, err := openPrivateFile(path, unix.O_RDONLY, 0)
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	limited := io.LimitReader(file, maximumStateBytes+1)
	content, err := io.ReadAll(limited)
	if err != nil || len(content) == 0 || len(content) > maximumStateBytes {
		return State{}, ErrInvalidState
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var state State
	if decoder.Decode(&state) != nil {
		return State{}, ErrInvalidState
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) || state.Validate() != nil {
		return State{}, ErrInvalidState
	}
	_, canonical, err := policy.CanonicalSHA256(state)
	if err != nil || !bytes.Equal(content, append(canonical, '\n')) {
		return State{}, ErrInvalidState
	}
	return state, nil
}

func (store *store) writeState(state State) error {
	if state.Validate() != nil {
		return ErrInvalidState
	}
	_, content, err := policy.CanonicalSHA256(state)
	if err != nil {
		return ErrInvalidState
	}
	return atomicWrite(filepath.Join(store.stateDir, stateFilename), append(content, '\n'))
}

func (store *store) sessionRoot(binding policyqualification.Binding) string {
	return filepath.Join(store.sessions, "session-"+string(binding.SessionID))
}

func (store *store) sources(binding policyqualification.Binding) (*sourceStore, error) {
	root := filepath.Join(store.sessionRoot(binding), "sources")
	if err := ensurePrivateDirectory(store.sessionRoot(binding)); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	return &sourceStore{root: root}, nil
}

func (sources *sourceStore) put(id metadata.UUID, content []byte) (policyqualification.SourceReference, error) {
	if metadataUUID(id) != nil || len(content) == 0 || len(content) > policyqualification.MaximumSourceBytes {
		return policyqualification.SourceReference{}, ErrInvalidState
	}
	path := filepath.Join(sources.root, string(id)+".json")
	if err := atomicCreate(path, content); err != nil {
		return policyqualification.SourceReference{}, err
	}
	return policyqualification.SourceReference{EventID: id, SHA256: policy.SHA256Hex(content)}, nil
}

func (sources *sourceStore) LoadQualificationSource(id metadata.UUID) ([]byte, error) {
	if metadataUUID(id) != nil {
		return nil, ErrInvalidState
	}
	file, err := openPrivateFile(filepath.Join(sources.root, string(id)+".json"), unix.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, policyqualification.MaximumSourceBytes+1))
	if err != nil || len(content) == 0 || len(content) > policyqualification.MaximumSourceBytes {
		return nil, ErrInvalidState
	}
	return content, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return ErrInvalidState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return ErrInvalidState
	}
	return nil
}

func openPrivateFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode))
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, ErrInvalidState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		_ = file.Close()
		return nil, ErrInvalidState
	}
	return file, nil
}

func atomicCreate(path string, content []byte) error {
	file, err := openPrivateFile(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func atomicWrite(path string, content []byte) error {
	id, err := metadata.NewUUID(nil)
	if err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+string(id)+".tmp")
	if err := atomicCreate(temporary, content); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
