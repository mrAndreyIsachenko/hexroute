package heartbeat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

func privateDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	return directory
}

func TestPublisherPersistsMonotonicAtomicSequence(t *testing.T) {
	path := filepath.Join(privateDirectory(t), FileName)
	publisher, err := OpenPublisher(path, 123)
	if err != nil {
		t.Fatalf("OpenPublisher() error: %v", err)
	}
	if publisher.BaseTick() != 0 {
		t.Fatalf("BaseTick() = %d", publisher.BaseTick())
	}
	if err := publisher.Publish(10); err != nil {
		t.Fatalf("first Publish() error: %v", err)
	}
	if err := publisher.Publish(15); err != nil {
		t.Fatalf("second Publish() error: %v", err)
	}

	record, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if record.Sequence != 2 ||
		record.PID != 123 ||
		record.MonotonicTick != 15 {
		t.Fatalf("Load() = %+v", record)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("heartbeat mode = %o", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".hexroute-heartbeat-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary heartbeat files = %v, error=%v", matches, err)
	}
}

func TestPublisherContinuesSequenceAcrossProcessRestart(t *testing.T) {
	path := filepath.Join(privateDirectory(t), FileName)
	first, _ := OpenPublisher(path, 123)
	if err := first.Publish(20); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	second, err := OpenPublisher(path, 456)
	if err != nil {
		t.Fatalf("second OpenPublisher() error: %v", err)
	}
	if second.BaseTick() != 20 {
		t.Fatalf("BaseTick() = %d, want 20", second.BaseTick())
	}
	if err := second.Publish(20); err != nil {
		t.Fatalf("second Publish() error: %v", err)
	}
	record, _ := Load(path)
	if record.Sequence != 2 || record.PID != 456 || record.MonotonicTick != 20 {
		t.Fatalf("Load() = %+v", record)
	}
}

func TestPublisherRejectsNonMonotonicTickWithoutChangingFile(t *testing.T) {
	path := filepath.Join(privateDirectory(t), FileName)
	publisher, _ := OpenPublisher(path, 123)
	if err := publisher.Publish(20); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	if err := publisher.Publish(19); !errors.Is(err, control.ErrNonMonotonicTick) {
		t.Fatalf("Publish() error = %v, want %v", err, control.ErrNonMonotonicTick)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("rejected publish changed heartbeat file")
	}
}

func TestLoadRejectsUnknownFieldsAndSymlinks(t *testing.T) {
	directory := privateDirectory(t)
	path := filepath.Join(directory, FileName)
	record := map[string]any{
		"schema":         Schema,
		"sequence":       1,
		"pid":            123,
		"monotonic_tick": 10,
		"command":        "restart",
	}
	content, _ := json.Marshal(record)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if _, err := Load(path); !errors.Is(err, ErrInvalidHeartbeat) {
		t.Fatalf("Load() error = %v, want %v", err, ErrInvalidHeartbeat)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink() error: %v", err)
	}
	if _, err := OpenPublisher(path, 123); err == nil {
		t.Fatal("OpenPublisher() accepted a symlink")
	}
}

func TestLoadRejectsOversizedHeartbeat(t *testing.T) {
	path := filepath.Join(privateDirectory(t), FileName)
	content := make([]byte, MaxFileSize+1)
	for index := range content {
		content[index] = ' '
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if _, err := Load(path); !errors.Is(err, ErrInvalidHeartbeat) {
		t.Fatalf("Load() error = %v, want %v", err, ErrInvalidHeartbeat)
	}
}

func TestPublisherRequiresPrivateOwnerDirectory(t *testing.T) {
	directory := privateDirectory(t)
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	path := filepath.Join(directory, FileName)
	if _, err := OpenPublisher(path, 123); err == nil {
		t.Fatal("OpenPublisher() accepted a non-private directory")
	}
}
