package connectivityhost

import (
	"bufio"
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
//
// It reads back what is already there. A recorder that opened blind would
// count from zero over a file that plainly holds records, and would write its
// first comparison again because it could not remember writing it — so every
// restart of a 72-hour soak would leave a duplicate line and an under-reported
// total in the one file the soak is studied from.
func OpenRecorder(root string) (*Recorder, error) {
	if root == "" {
		return nil, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: comparison root: %v", ErrStore, err)
	}
	recorder := &Recorder{path: filepath.Join(root, comparisonFile)}
	if err := recorder.resume(); err != nil {
		return nil, err
	}
	return recorder, nil
}

// resume restores the position the previous process left.
func (recorder *Recorder) resume() error {
	file, err := os.Open(recorder.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStore, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxComparisonBytes)
	var last []byte
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		last = append(last[:0], line...)
		recorder.written++
	}
	if err := scanner.Err(); err != nil {
		// A log this build cannot read back is not a reason to refuse to
		// record. Appending to it keeps the soak observable; what is lost is
		// only the suppression of one repeated line.
		recorder.written = 0
		return nil
	}
	if last == nil {
		return nil
	}
	var previous Comparison
	if json.Unmarshal(last, &previous) != nil {
		return nil
	}
	previous.SnapshotGeneration = 0
	key, err := json.Marshal(previous)
	if err != nil {
		return nil
	}
	recorder.last = string(key)
	return nil
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
