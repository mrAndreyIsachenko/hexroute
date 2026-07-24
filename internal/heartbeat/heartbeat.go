package heartbeat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

const (
	Schema      = "hexroute.control-heartbeat.v1"
	FileName    = "control-loop.heartbeat.json"
	MaxFileSize = 4 * 1024
)

type Record struct {
	Schema        string       `json:"schema"`
	Sequence      uint64       `json:"sequence"`
	PID           int          `json:"pid"`
	MonotonicTick control.Tick `json:"monotonic_tick"`
}

type Publisher struct {
	mu     sync.Mutex
	path   string
	record Record
}

var (
	ErrInvalidHeartbeat = errors.New("invalid control-loop heartbeat")
	ErrHeartbeatMissing = errors.New("control-loop heartbeat not found")
)

func OpenPublisher(path string, pid int) (*Publisher, error) {
	if pid <= 0 || validatePath(path) != nil {
		return nil, ErrInvalidHeartbeat
	}
	record, err := Load(path)
	if errors.Is(err, ErrHeartbeatMissing) {
		record = Record{
			Schema: Schema,
			PID:    pid,
		}
	} else if err != nil {
		return nil, err
	}
	record.PID = pid
	return &Publisher{
		path:   path,
		record: record,
	}, nil
}

func (publisher *Publisher) BaseTick() control.Tick {
	if publisher == nil {
		return 0
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.record.MonotonicTick
}

func (publisher *Publisher) Publish(at control.Tick) error {
	if publisher == nil || at < 0 {
		return ErrInvalidHeartbeat
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if at < publisher.record.MonotonicTick {
		return control.ErrNonMonotonicTick
	}
	if publisher.record.Sequence == ^uint64(0) {
		return ErrInvalidHeartbeat
	}
	next := publisher.record
	next.Sequence++
	next.MonotonicTick = at
	if err := save(publisher.path, next); err != nil {
		return err
	}
	publisher.record = next
	return nil
}

func Load(path string) (Record, error) {
	if !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Base(path) != FileName {
		return Record{}, ErrInvalidHeartbeat
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrHeartbeatMissing
	}
	if err != nil {
		return Record{}, fmt.Errorf("open heartbeat: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil || len(content) == 0 || len(content) > MaxFileSize {
		return Record{}, ErrInvalidHeartbeat
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, ErrInvalidHeartbeat
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Record{}, ErrInvalidHeartbeat
	}
	if validateRecord(record) != nil {
		return Record{}, ErrInvalidHeartbeat
	}
	return record, nil
}

func save(path string, record Record) error {
	if validateRecord(record) != nil || validatePath(path) != nil {
		return ErrInvalidHeartbeat
	}
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".hexroute-heartbeat-*")
	if err != nil {
		return fmt.Errorf("create heartbeat temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("protect heartbeat temp file: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		temp.Close()
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync heartbeat: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close heartbeat: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("activate heartbeat: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open heartbeat directory: %w", err)
	}
	defer directoryFile.Close()
	if err := directoryFile.Sync(); err != nil {
		return fmt.Errorf("sync heartbeat directory: %w", err)
	}
	return nil
}

func validateRecord(record Record) error {
	if record.Schema != Schema ||
		record.Sequence == 0 ||
		record.PID <= 0 ||
		record.MonotonicTick < 0 {
		return ErrInvalidHeartbeat
	}
	return nil
}

func validatePath(path string) error {
	if !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Base(path) != FileName {
		return ErrInvalidHeartbeat
	}
	if err := validatePrivateOwner(filepath.Dir(path), true); err != nil {
		return err
	}
	if err := validatePrivateOwner(path, false); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validatePrivateOwner(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 ||
		(directory && !info.IsDir()) ||
		(!directory && !info.Mode().IsRegular()) {
		return ErrInvalidHeartbeat
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return ErrInvalidHeartbeat
	}
	return nil
}
