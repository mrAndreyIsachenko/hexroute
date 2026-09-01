package eventarchive

import (
	"time"
)

// MaxReadRecords bounds one read. A review that asked for everything and got
// everything would be a way to run the host out of memory from a report.
const MaxReadRecords = 100_000

// Query is a request for a window of the archive.
//
// An empty From or To means the archive's own edge, so a caller that does not
// know what the archive holds can still ask for all of it without guessing.
type Query struct {
	From time.Time
	To   time.Time
	// Limit bounds the records returned. Zero means MaxReadRecords.
	Limit int
}

// Reading is the answer, together with what it is an answer about.
//
// Requested and Covered are separate because they disagree in the case that
// matters: an archive whose oldest record is newer than the requested start
// answers about a shorter period than was asked for, and a caller that cannot
// see the difference reads an eviction as a quiet week.
type Reading struct {
	Records   []Record
	Requested Window
	Covered   Window
	// Truncated says the limit stopped the read before the window ended, so
	// Covered describes fewer records than the window holds.
	Truncated bool
}

// Read returns the records inside a window and says what window it answered
// about.
func (archive *Archive) Read(query Query) (Reading, error) {
	limit := query.Limit
	if limit <= 0 || limit > MaxReadRecords {
		limit = MaxReadRecords
	}

	archive.mu.Lock()
	defer archive.mu.Unlock()

	held, err := archive.scan()
	if err != nil {
		return Reading{}, err
	}

	from, to := query.From.UTC(), query.To.UTC()
	requested := Window{Oldest: from, Newest: to}
	if from.IsZero() && to.IsZero() {
		requested.Empty = true
	}

	selected := make([]Record, 0, len(held))
	for _, record := range held {
		at := record.Metadata.WallClock.UTC()
		if !from.IsZero() && at.Before(from) {
			continue
		}
		if !to.IsZero() && at.After(to) {
			continue
		}
		selected = append(selected, record)
	}

	truncated := false
	if len(selected) > limit {
		selected = selected[:limit]
		truncated = true
	}

	reading := Reading{
		Records: selected, Requested: requested, Truncated: truncated,
	}
	if len(selected) == 0 {
		// An empty answer says so. A zeroed window would print as a period in
		// which nothing happened, which is the one reading an empty archive
		// must never be mistaken for.
		reading.Covered = Window{Empty: true}
		return reading, nil
	}
	reading.Covered = Window{
		Records: uint32(len(selected)),
		First:   selected[0].Sequence,
		Last:    selected[len(selected)-1].Sequence,
		Oldest:  selected[0].Metadata.WallClock.UTC(),
		Newest:  selected[len(selected)-1].Metadata.WallClock.UTC(),
	}
	return reading, nil
}

// Shortened reports that the archive answered about less than was asked for.
//
// It is a question worth being able to ask directly, because the two ways it
// happens — the archive starts later than the request, or ends earlier — both
// look like a period with less in it than expected.
func (reading Reading) Shortened() bool {
	if reading.Truncated {
		return true
	}
	if reading.Covered.Empty {
		return !reading.Requested.Empty
	}
	if !reading.Requested.Oldest.IsZero() &&
		reading.Covered.Oldest.After(reading.Requested.Oldest) {
		return true
	}
	if !reading.Requested.Newest.IsZero() &&
		reading.Covered.Newest.Before(reading.Requested.Newest) {
		return true
	}
	return false
}
