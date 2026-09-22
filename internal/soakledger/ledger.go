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
	// LongestSilence is the longest stretch inside the window with no record at
	// all. The runtime writes several records every cycle, so a silence longer
	// than a few cycles is a runtime that was not running — and a decision it
	// did not make there is invisible to a comparison that only looks at where
	// windows start and meet. Absent on collections made before it was kept,
	// and absent is not zero: nobody measured those windows' silences.
	LongestSilence *time.Duration `json:"longest_silence,omitempty"`
	// Silences are where the stretches longer than MaxLead fell, including one
	// from the requested start to the first record. A machine asleep writes
	// nothing, so a silence is a hole only if nothing after it says the machine
	// woke. Absent on collections made before they were kept.
	Silences []Span `json:"silences,omitempty"`
	// Dozing are the stretches where this runtime's cycles did not finish: a
	// machine dozing on battery wakes for seconds, and a cycle that stops at
	// the probes records an incomplete decision. Both runtimes decide in such
	// stretches, at moments neither shares, so nothing in them is compared.
	// Absent on collections made before they were kept.
	Dozing []Span `json:"dozing,omitempty"`
}

// Inside says whether a moment falls in any of the spans.
func Inside(spans []Span, at time.Time) bool {
	for _, span := range spans {
		if !at.Before(span.From) && !at.After(span.To) {
			return true
		}
	}
	return false
}

// Within says whether a stretch falls entirely in any of the spans.
// Superseded drops what an older collection recorded about a stretch a later one
// read again. Collections are appended, so a stretch read twice is described
// twice, and the older description outlives the judgement it was made under: a
// stretch read as dozing on 2026-09-22 stayed dozing after the rule that read it
// was narrowed and the ledger collected again from the soak's start. A later
// reading of the same stretch is the one that counts; what an older collection
// saw outside it is kept, because the archive may no longer hold those records.
func Superseded(windows []Coverage) []Coverage {
	read := func(window Coverage) Span { return Span{From: window.Oldest, To: window.Newest} }
	fresh := make([]Coverage, 0, len(windows))
	for _, window := range windows {
		later := make([]Span, 0, len(windows))
		for _, other := range windows {
			if other.CollectedAt.After(window.CollectedAt) {
				later = append(later, read(other))
			}
		}
		window.Silences = outside(later, window.Silences)
		window.Dozing = outside(later, window.Dozing)
		fresh = append(fresh, window)
	}
	return fresh
}

// outside are the spans no later reading covers.
func outside(later []Span, spans []Span) []Span {
	if len(later) == 0 || len(spans) == 0 {
		return spans
	}
	kept := make([]Span, 0, len(spans))
	for _, span := range spans {
		if !Within(later, span) {
			kept = append(kept, span)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func Within(spans []Span, stretch Span) bool {
	for _, span := range spans {
		if !stretch.From.Before(span.From) && !stretch.To.After(span.To) {
			return true
		}
	}
	return false
}

// Span is a stretch of time.
type Span struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Silences are the stretches longer than MaxLead with no moment in them, from
// the start of what was asked for, in any order of moments.
func Silences(from time.Time, moments []time.Time) []Span {
	if len(moments) == 0 {
		return nil
	}
	sorted := append([]time.Time(nil), moments...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	var silences []Span
	previous := from
	for _, moment := range sorted {
		if moment.Sub(previous) > MaxLead {
			silences = append(silences, Span{From: previous, To: moment})
		}
		previous = moment
	}
	return silences
}

// woke says a wake gap was decided within MaxLead of a silence ending.
// woke says a wake gap the runtime decided accounts for a silence.
//
// Either the wake was decided within three cycles of the silence ending, or the
// gap it was decided on covers the silence: a cycle saying eleven minutes passed
// since the last one has accounted for every silence in those eleven minutes.
// A machine on battery wakes for seconds at a time and sleeps again, so the
// cycle that finishes and decides can be several sleeps later — measured
// 2026-09-20, a silence of 46 minutes whose wake was decided 42 minutes after
// it ended, on a gap that spanned both.
func woke(cycles []Cycle, silence Span) bool {
	for _, cycle := range cycles {
		// A cycle the runtime ran inside the silence: its record was lost, not
		// its observation. Measured 2026-09-20, a cycle decided at 04:26:33Z
		// inside a silence and wrote nothing, because the machine slept again
		// before the rest of that cycle; the next cycle's gap named it.
		// A decision with no gap recorded names no cycle before it, and says
		// nothing about a silence it did not measure.
		if cycle.TickGap > 0 {
			if previous := cycle.Previous(); previous.After(silence.From) && previous.Before(silence.To) {
				return true
			}
		}
		if !cycle.Wake || cycle.At.Before(silence.To) {
			continue
		}
		if cycle.At.Sub(silence.To) <= MaxLead {
			return true
		}
		if !cycle.Previous().After(silence.From.Add(MaxLead)) {
			return true
		}
	}
	return false
}

// LongestSilence is the longest gap between consecutive moments, in any order.
func LongestSilence(moments []time.Time) time.Duration {
	sorted := append([]time.Time(nil), moments...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	var longest time.Duration
	for index := 1; index < len(sorted); index++ {
		if gap := sorted[index].Sub(sorted[index-1]); gap > longest {
			longest = gap
		}
	}
	return longest
}

// Entry is one of this runtime's decisions to rebuild.
type Entry struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	Causes   []string  `json:"causes"`
	// TickGap is the wall time the deciding cycle was after the one before it.
	// A decision says how long its own gap was, so a silence inside that gap is
	// a stretch the runtime accounted for rather than one nobody observed.
	TickGap time.Duration `json:"tick_gap,omitempty"`
}

// Cycle is one decision this runtime recorded: when it was reached, the gap
// since the cycle before it, and whether it named a wake.
//
// The gap is what makes a decision say more than its own moment: it names the
// cycle before it, whose record may be missing. A machine that sleeps inside
// the rest of a cycle loses what that cycle had not yet written.
type Cycle struct {
	At      time.Time
	TickGap time.Duration
	Wake    bool
}

// Previous is when the cycle before this one ran, as this one measured it.
func (cycle Cycle) Previous() time.Time { return cycle.At.Add(-cycle.TickGap) }

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
	// A decision already collected is not collected again — collections overlap
	// by a cycle or two, and the same decision read twice is one decision. But a
	// later collection can carry what an earlier one did not: a collection made
	// before the gap was kept holds none, and dropping the richer line would
	// leave the judgement unable to see which silences the runtime accounted
	// for, with no way to repair it short of discarding the ledger.
	gaps := make(map[uint64]time.Duration, len(held))
	seen := make(map[uint64]bool, len(held))
	for _, entry := range held {
		seen[entry.Sequence] = true
		if entry.TickGap > gaps[entry.Sequence] {
			gaps[entry.Sequence] = entry.TickGap
		}
	}
	fresh := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !seen[entry.Sequence] {
			seen[entry.Sequence] = true
			gaps[entry.Sequence] = entry.TickGap
			fresh = append(fresh, entry)
			continue
		}
		if entry.TickGap > gaps[entry.Sequence] {
			gaps[entry.Sequence] = entry.TickGap
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
	var order []uint64
	held := map[uint64]Entry{}
	err := readLines(ledger.path("decisions.jsonl"), func(line []byte) error {
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		previous, repeated := held[entry.Sequence]
		if !repeated {
			order = append(order, entry.Sequence)
			held[entry.Sequence] = entry
			return nil
		}
		// One decision, written twice because a later collection carried the
		// gap the first did not. The one that says more is the one kept.
		if entry.TickGap > previous.TickGap {
			held[entry.Sequence] = entry
		}
		return nil
	})
	entries := make([]Entry, 0, len(order))
	for _, sequence := range order {
		entries = append(entries, held[sequence])
	}
	return entries, err
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
func Continuous(windows []Coverage, cycles []Cycle, from, until time.Time) error {
	if len(windows) == 0 {
		return fmt.Errorf("%w: nothing was collected", ErrNotContinuous)
	}
	// A window whose silences were not kept is not evidence that nothing was
	// silent in it. It is set aside, and a collection made again from the soak's
	// start covers what it covered.
	measured := make([]Coverage, 0, len(windows))
	for _, window := range windows {
		if window.LongestSilence != nil {
			measured = append(measured, window)
		}
	}
	if len(measured) == 0 {
		return fmt.Errorf("%w: no collection kept its silences; collect again with --from the soak's start", ErrNotContinuous)
	}
	windows = measured
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
		if *window.LongestSilence > MaxLead && len(window.Silences) == 0 {
			return fmt.Errorf("%w: the collection at %s holds a silence of %s it did not locate; collect again with --from the soak's start",
				ErrNotContinuous, window.CollectedAt.Format(time.RFC3339), window.LongestSilence.Round(time.Second))
		}
		for _, silence := range window.Silences {
			// A stretch the runtime spent dozing is not judged at all, so a
			// silence inside one is not a hole in what was judged.
			if Within(window.Dozing, silence) {
				continue
			}
			if !woke(cycles, silence) {
				return fmt.Errorf("%w: the runtime wrote nothing from %s to %s and decided no wake after it",
					ErrNotContinuous, silence.From.Format(time.RFC3339), silence.To.Format(time.RFC3339))
			}
		}
		if lead := window.Oldest.Sub(window.Requested); lead > MaxLead &&
			!woke(cycles, Span{From: window.Requested, To: window.Oldest}) {
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
