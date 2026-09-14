// Package soakledger keeps what a soak is judged from, for longer than the
// archive it is collected from keeps it.
//
// The root runtime's archive answers for seven days at most and evicts by age
// regardless of priority, so a soak that lasts seven days cannot be read out of
// it at the end: its first hours are gone by then. Collected as it goes, and
// with each collection's window recorded, the soak can be judged whole — and a
// window nobody collected, or one the archive had already lost, is a hole the
// judgement refuses rather than a quiet stretch it counts as agreement.
package soakledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/soakcompare"
)

// MaxLead is how far after the requested start a collection's oldest record may
// be before the window counts as not covered. The runtime writes several records
// every cycle, so three cycles with nothing is an eviction or a runtime that was
// not running — either way, a stretch nobody observed.
const MaxLead = 3 * time.Minute

var ErrNotContinuous = errors.New("the soak has a stretch nobody observed")

// Coverage is what one collection read from the archive.
type Coverage struct {
	Requested   time.Time `json:"requested"`
	Oldest      time.Time `json:"oldest"`
	Newest      time.Time `json:"newest"`
	Records     uint32    `json:"records"`
	CollectedAt time.Time `json:"collected_at"`
}

// Entry is one of this runtime's decisions to rebuild.
type Entry struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	Causes   []string  `json:"causes"`
}

type induction struct {
	At    time.Time `json:"at"`
	Cause string    `json:"cause"`
}

// Ledger is a directory of append-only files.
type Ledger struct{ dir string }

func Open(dir string) (*Ledger, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("the ledger directory must be absolute: %s", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Ledger{dir: dir}, nil
}

func (ledger *Ledger) path(name string) string { return filepath.Join(ledger.dir, name) }

// Collect records one collection: the decisions it found, without repeating any
// already held, and the window it read.
func (ledger *Ledger) Collect(entries []Entry, coverage Coverage) error {
	held, err := ledger.Decisions()
	if err != nil {
		return err
	}
	seen := make(map[uint64]bool, len(held))
	for _, entry := range held {
		seen[entry.Sequence] = true
	}
	fresh := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !seen[entry.Sequence] {
			seen[entry.Sequence] = true
			fresh = append(fresh, entry)
		}
	}
	if err := appendLines(ledger.path("decisions.jsonl"), fresh); err != nil {
		return err
	}
	return appendLines(ledger.path("coverage.jsonl"), []any{coverage})
}

// Note records that the operator induced a cause, at the moment they did.
func (ledger *Ledger) Note(at time.Time, cause string) error {
	switch cause {
	case soakcompare.ProcessGone, soakcompare.WakeGap, soakcompare.CarrierChanged:
	default:
		return fmt.Errorf("not a cause the soak judges: %q", cause)
	}
	return appendLines(ledger.path("inductions.jsonl"), []any{induction{At: at.UTC(), Cause: cause}})
}

func (ledger *Ledger) Decisions() ([]Entry, error) {
	var entries []Entry
	return entries, readLines(ledger.path("decisions.jsonl"), func(line []byte) error {
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
}

func (ledger *Ledger) Coverage() ([]Coverage, error) {
	var windows []Coverage
	return windows, readLines(ledger.path("coverage.jsonl"), func(line []byte) error {
		var window Coverage
		if err := json.Unmarshal(line, &window); err != nil {
			return err
		}
		windows = append(windows, window)
		return nil
	})
}

func (ledger *Ledger) Inductions() ([]soakcompare.Induction, error) {
	var inductions []soakcompare.Induction
	return inductions, readLines(ledger.path("inductions.jsonl"), func(line []byte) error {
		var noted induction
		if err := json.Unmarshal(line, &noted); err != nil {
			return err
		}
		inductions = append(inductions, soakcompare.Induction{At: noted.At, Cause: noted.Cause})
		return nil
	})
}

// Next is where the next collection starts: the newest record already covered.
func (ledger *Ledger) Next() (time.Time, bool, error) {
	windows, err := ledger.Coverage()
	if err != nil || len(windows) == 0 {
		return time.Time{}, false, err
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Newest.Before(windows[j].Newest) })
	return windows[len(windows)-1].Newest, true, nil
}

// Continuous answers whether the collections cover the soak without a hole.
//
// Each collection must start within MaxLead of where it asked to, the first must
// ask from no later than the soak's start, each must ask from no later than the
// previous one's newest record, and the last must reach the soak's end.
func Continuous(windows []Coverage, from, until time.Time) error {
	if len(windows) == 0 {
		return fmt.Errorf("%w: nothing was collected", ErrNotContinuous)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Requested.Before(windows[j].Requested) })
	if windows[0].Requested.After(from) {
		return fmt.Errorf("%w: collection starts at %s, after the soak's start %s",
			ErrNotContinuous, windows[0].Requested.Format(time.RFC3339), from.Format(time.RFC3339))
	}
	var reached time.Time
	for index, window := range windows {
		if window.Records == 0 {
			return fmt.Errorf("%w: the collection at %s read nothing", ErrNotContinuous, window.CollectedAt.Format(time.RFC3339))
		}
		if lead := window.Oldest.Sub(window.Requested); lead > MaxLead {
			return fmt.Errorf("%w: from %s the archive held nothing for %s",
				ErrNotContinuous, window.Requested.Format(time.RFC3339), lead.Round(time.Second))
		}
		if index > 0 && window.Requested.After(reached) {
			return fmt.Errorf("%w: nothing was collected between %s and %s",
				ErrNotContinuous, reached.Format(time.RFC3339), window.Requested.Format(time.RFC3339))
		}
		if window.Newest.After(reached) {
			reached = window.Newest
		}
	}
	if until.Sub(reached) > MaxLead {
		return fmt.Errorf("%w: the last collection reaches %s, short of %s",
			ErrNotContinuous, reached.Format(time.RFC3339), until.Format(time.RFC3339))
	}
	return nil
}

func appendLines(path string, values []any) error {
	if len(values) == 0 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			_ = file.Close()
			return err
		}
		if _, err := file.Write(append(encoded, '\n')); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readLines(path string, each func([]byte) error) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		if err := each(scanner.Bytes()); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	}
	return scanner.Err()
}
