package control

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotStoreRoundTripAndGenerationGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := NewSnapshot(StateHealthy)

	if err := SaveSnapshot(path, 0, initial); err != nil {
		t.Fatalf("SaveSnapshot(initial) error: %v", err)
	}

	mode, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if mode.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %#o, want 0600", mode.Mode().Perm())
	}

	next := initial
	next.Generation = 1
	next.ConsecutiveFailures = 1
	if err := SaveSnapshot(path, 0, next); err != nil {
		t.Fatalf("SaveSnapshot(next) error: %v", err)
	}

	stale := next
	stale.Generation = 2
	if err := SaveSnapshot(path, 0, stale); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale SaveSnapshot() error = %v, want %v", err, ErrStaleGeneration)
	}

	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error: %v", err)
	}
	if loaded != next {
		t.Fatalf("LoadSnapshot() = %+v, want %+v", loaded, next)
	}
}

func TestSnapshotStoreRejectsInvalidSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	invalid := NewSnapshot(State("INVALID"))

	if err := SaveSnapshot(path, 0, invalid); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("SaveSnapshot() error = %v, want %v", err, ErrInvalidSnapshot)
	}
}

func TestLoadSnapshotRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	content := []byte(`{"schema_version":1,"generation":0,"state":"HEALTHY","unknown":true}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	if _, err := LoadSnapshot(path); err == nil {
		t.Fatal("LoadSnapshot() accepted an unknown field")
	}
}
