package ingressagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
)

const (
	appliedName   = "applied.json"
	retainedName  = "retained.json"
	resultName    = "last-result.json"
	appliedAtName = "applied-at.json"
	// filePermissions keeps the state readable only by the agent's own user.
	// None of it is secret, but a configuration a host runs is not something
	// any local account should be able to rewrite.
	filePermissions = 0o600
	dirPermissions  = 0o700
)

// Store is the agent's own state on disk: what is applied, what is retained,
// and what the last synchronisation did.
//
// The retained version is kept as the artifact rather than as the
// configuration it contains, so that returning to it verifies a signature
// rather than trusting a file.
type Store struct {
	directory string
}

// NewStore opens the state directory, creating it if it is not there.
func NewStore(directory string) (*Store, error) {
	if directory == "" || !filepath.IsAbs(directory) ||
		filepath.Clean(directory) != directory ||
		strings.ContainsAny(directory, "\x00\r\n") {
		return nil, fmt.Errorf("%w: state directory", ErrAgent)
	}
	if err := os.MkdirAll(directory, dirPermissions); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	return &Store{directory: directory}, nil
}

// Applied is the version currently in service.
func (store *Store) Applied() (configversion.Artifact, []byte, bool, error) {
	return store.read(appliedName)
}

// Retained is the version to return to.
func (store *Store) Retained() (configversion.Artifact, []byte, bool, error) {
	return store.read(retainedName)
}

// SetApplied records a version as the one in service, and when it entered it.
//
// The time is kept because a window has to start somewhere, and the file's own
// timestamp is not it: a state directory that was copied, restored or touched
// would move the moment a version is judged by.
func (store *Store) SetApplied(encoded []byte, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("%w: no application time", ErrAgent)
	}
	if err := store.write(appliedName, encoded); err != nil {
		return err
	}
	return store.writeFile(appliedAtName,
		[]byte(strconv.Quote(at.UTC().Format(time.RFC3339Nano))))
}

// AppliedAt is when the applied version entered service.
func (store *Store) AppliedAt() (time.Time, bool, error) {
	content, ok, err := store.readFile(appliedAtName)
	if err != nil || !ok {
		return time.Time{}, ok, err
	}
	raw, err := strconv.Unquote(string(content))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%w: application time", ErrAgent)
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%w: application time", ErrAgent)
	}
	return at.UTC(), true, nil
}

// Retain keeps a version so that returning to it needs no network.
func (store *Store) Retain(encoded []byte) error {
	return store.write(retainedName, encoded)
}

// PromoteRetained makes the retained version the applied one and leaves
// nothing retained. A return that kept the version it returned from would
// allow a second return straight back into the fault.
func (store *Store) PromoteRetained(at time.Time) error {
	_, encoded, ok, err := store.Retained()
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoRetainedVersion
	}
	if err := store.SetApplied(encoded, at); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(store.directory, retainedName)); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	return nil
}

// RecordResult writes what the last synchronisation did, including which check
// a refused version failed.
func (store *Store) RecordResult(result Result) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	return store.writeFile(resultName, encoded)
}

// LastResult reads what the last synchronisation did.
func (store *Store) LastResult() (Result, bool, error) {
	content, ok, err := store.readFile(resultName)
	if err != nil || !ok {
		return Result{}, ok, err
	}
	var result Result
	if err := json.Unmarshal(content, &result); err != nil {
		return Result{}, false, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	return result, true, nil
}

func (store *Store) read(name string) (configversion.Artifact, []byte, bool, error) {
	content, ok, err := store.readFile(name)
	if err != nil || !ok {
		return configversion.Artifact{}, nil, ok, err
	}
	artifact, err := configversion.Decode(content)
	if err != nil {
		return configversion.Artifact{}, nil, false, err
	}
	return artifact, content, true, nil
}

func (store *Store) write(name string, encoded []byte) error {
	// Only an artifact this host would accept is stored. A state file that
	// could hold something unverifiable would be a way to make a host apply
	// it later without ever having verified it.
	if _, err := configversion.Decode(encoded); err != nil {
		return err
	}
	return store.writeFile(name, encoded)
}

func (store *Store) readFile(name string) ([]byte, bool, error) {
	content, err := os.ReadFile(filepath.Join(store.directory, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if len(content) == 0 || len(content) > configversion.MaxArtifactBytes {
		return nil, false, fmt.Errorf("%w: %s is %d bytes", ErrAgent, name, len(content))
	}
	return content, true, nil
}

// writeFile replaces a state file atomically, so that a host interrupted mid
// write does not come back holding half of one.
func (store *Store) writeFile(name string, content []byte) error {
	temporary, err := os.CreateTemp(store.directory, "."+name+".*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	path := temporary.Name()
	defer func() {
		temporary.Close()
		_ = os.Remove(path)
	}()
	if err := temporary.Chmod(filePermissions); err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	if err := os.Rename(path, filepath.Join(store.directory, name)); err != nil {
		return fmt.Errorf("%w: %w", ErrAgent, err)
	}
	return nil
}
