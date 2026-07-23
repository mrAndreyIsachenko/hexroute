package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrSnapshotNotFound = errors.New("state snapshot not found")

func LoadSnapshot(path string) (Snapshot, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("open snapshot: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()

	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func SaveSnapshot(path string, expectedGeneration uint64, snapshot Snapshot) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}

	current, err := LoadSnapshot(path)
	switch {
	case err == nil:
		if current.Generation != expectedGeneration {
			return ErrStaleGeneration
		}
	case errors.Is(err, ErrSnapshotNotFound):
		if expectedGeneration != 0 {
			return ErrStaleGeneration
		}
	default:
		return err
	}

	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".hexroute-snapshot-*")
	if err != nil {
		return fmt.Errorf("create snapshot temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("protect snapshot temp file: %w", err)
	}

	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		temp.Close()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("activate snapshot: %w", err)
	}

	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open snapshot directory: %w", err)
	}
	defer directoryFile.Close()
	if err := directoryFile.Sync(); err != nil {
		return fmt.Errorf("sync snapshot directory: %w", err)
	}
	return nil
}
