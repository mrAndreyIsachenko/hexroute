package connectivityhost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// A shadow soak is 72 hours long. A comparison held in memory answers nothing
// afterwards, so the recorder writes and the file is what gets studied.
//
// It records a comparison only when the correlation changed. A soak that
// appends an identical line every minute produces a file whose size is a
// measure of elapsed time — the same mistake the daemon logs made, and the
// reason a 33 MB log said one thing 163,367 times.

const (
	comparisonFile = "shadow-comparisons.jsonl"
	// MaxComparisonBytes bounds the recorded file. Beyond it the recorder
	// stops appending and says so rather than growing without limit or
	// silently dropping the beginning of a soak.
	MaxComparisonBytes = 8 * 1024 * 1024
)

// Recorder appends comparisons that differ from the last one written.
type Recorder struct {
	mu      sync.Mutex
	path    string
	last    string
	full    bool
	written uint64
}

// OpenRecorder prepares the durable comparison log under a store root.
func OpenRecorder(root string) (*Recorder, error) {
	if root == "" {
		return nil, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: comparison root: %v", ErrStore, err)
	}
	return &Recorder{path: filepath.Join(root, comparisonFile)}, nil
}

// Full reports that the recorder stopped appending because it reached its
// bound. It is exposed so the condition can be seen rather than inferred from
// a file that quietly stopped changing.
func (recorder *Recorder) Full() bool {
	if recorder == nil {
		return false
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.full
}

// Written reports how many comparisons were appended.
func (recorder *Recorder) Written() uint64 {
	if recorder == nil {
		return 0
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.written
}

// Record appends the comparison when it differs from the last one written.
//
// It reports whether it wrote. A repeated correlation is not news; the first
// one after a change is.
func (recorder *Recorder) Record(comparison Comparison) (bool, error) {
	if recorder == nil {
		return false, nil
	}
	encoded, err := json.Marshal(comparison)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStore, err)
	}
	// The generation moves whenever the read model does, which is not the same
	// as the correlation changing. Comparing without it keeps the file a record
	// of disagreements rather than of reductions.
	stable := comparison
	stable.SnapshotGeneration = 0
	key, err := json.Marshal(stable)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStore, err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.full || string(key) == recorder.last {
		return false, nil
	}
	info, statErr := os.Stat(recorder.path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return false, fmt.Errorf("%w: %v", ErrStore, statErr)
	}
	if statErr == nil && info.Size()+int64(len(encoded))+1 > MaxComparisonBytes {
		recorder.full = true
		return false, nil
	}
	file, err := os.OpenFile(recorder.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrStore, err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return false, fmt.Errorf("%w: %v", ErrStore, err)
	}
	if err := file.Sync(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrStore, err)
	}
	recorder.last = string(key)
	recorder.written++
	return true, nil
}
