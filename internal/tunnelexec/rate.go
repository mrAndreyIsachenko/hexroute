package tunnelexec

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Window is how many rebuilds may happen in a stretch of time.
type Window struct {
	Span  time.Duration
	Limit int
}

// Bounds are what this runtime keeps on itself: two rebuilds in five minutes,
// six in an hour, fifteen in a day.
//
// They are not about what a healthy machine needs — a machine that needs six
// rebuilds in an hour is not being repaired by the seventh. They are the point
// at which this runtime stops and hands the tunnel to something else.
var Bounds = []Window{
	{Span: 5 * time.Minute, Limit: 2},
	{Span: time.Hour, Limit: 6},
	{Span: 24 * time.Hour, Limit: 15},
}

// Rate counts rebuilds where a restart of this runtime does not clear them.
//
// The case the bound exists for is a runtime that rebuilds, dies, comes back and
// rebuilds again — a crash loop wearing the shape of supervision. Counters in
// memory would be cleared by exactly the fault they are meant to catch, so they
// are on disk, and a count that cannot be read is treated as a bound reached
// rather than as permission.
type Rate struct {
	Path string
	Now  func() time.Time
	// Windows are the bounds. Empty means Bounds.
	Windows []Window
}

var ErrInvalidRate = errors.New("invalid rebuild rate store")

func (rate Rate) windows() []Window {
	if len(rate.Windows) == 0 {
		return Bounds
	}
	return rate.Windows
}

func (rate Rate) valid() bool { return rate.Path != "" && rate.Now != nil }

// Allows answers whether another rebuild is inside every bound.
func (rate Rate) Allows() (bool, error) {
	if !rate.valid() {
		return false, ErrInvalidRate
	}
	performed, err := rate.read()
	if err != nil {
		return false, err
	}
	now := rate.Now()
	for _, window := range rate.windows() {
		if countSince(performed, now.Add(-window.Span)) >= window.Limit {
			return false, nil
		}
	}
	return true, nil
}

// Record keeps a rebuild that happened, and forgets what no bound can still see.
func (rate Rate) Record(at time.Time) error {
	if !rate.valid() {
		return ErrInvalidRate
	}
	performed, err := rate.read()
	if err != nil {
		return err
	}
	performed = append(performed, at.UTC())
	return rate.write(performed)
}

// Clear forgets every rebuild. It is what `resume` does, and nothing else does
// it: a bound that a restart or a reinstall cleared would not be a bound.
func (rate Rate) Clear() error {
	if !rate.valid() {
		return ErrInvalidRate
	}
	return rate.write(nil)
}

// Performed is what the store holds, newest last. It is for a runtime that
// reports on itself, not for deciding.
func (rate Rate) Performed() ([]time.Time, error) {
	if !rate.valid() {
		return nil, ErrInvalidRate
	}
	return rate.read()
}

func countSince(performed []time.Time, from time.Time) int {
	count := 0
	for _, at := range performed {
		if at.After(from) {
			count++
		}
	}
	return count
}

func (rate Rate) longest() time.Duration {
	longest := time.Duration(0)
	for _, window := range rate.windows() {
		if window.Span > longest {
			longest = window.Span
		}
	}
	return longest
}

func (rate Rate) read() ([]time.Time, error) {
	content, err := os.ReadFile(rate.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stamps []string
	if err := json.Unmarshal(content, &stamps); err != nil {
		return nil, err
	}
	performed := make([]time.Time, 0, len(stamps))
	for _, stamp := range stamps {
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return nil, err
		}
		performed = append(performed, at.UTC())
	}
	sort.Slice(performed, func(i, j int) bool { return performed[i].Before(performed[j]) })
	return performed, nil
}

func (rate Rate) write(performed []time.Time) error {
	horizon := rate.Now().Add(-rate.longest())
	stamps := make([]string, 0, len(performed))
	for _, at := range performed {
		if at.After(horizon) {
			stamps = append(stamps, at.UTC().Format(time.RFC3339Nano))
		}
	}
	content, err := json.Marshal(stamps)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rate.Path), 0o700); err != nil {
		return err
	}
	temporary := rate.Path + ".writing"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, rate.Path)
}
