package telemetry

import (
	"errors"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

// Store is everything an upload needs from the records waiting to be sent.
//
// It is an interface because the thing that uploads must not hold the store.
// Opening a spool is a recovery operation — it completes or discards every
// unfinished write — which is correct for the only writer and destructive for a
// second one, and the runtime that owns the store appends to it every sixty
// seconds. So the owner serves this and the uploader asks, across a process
// boundary or not.
//
// Both ways of writing are a fixed shape. There is no Append here on purpose:
// an uploader that could put an arbitrary record into the store would be a
// path from whatever answers it into the record of what this host observed.
type Store interface {
	// Batch is the oldest records waiting, at most limit of them, and only
	// those are read.
	Batch(limit int) ([]spool.Entry, error)
	// Range is the retained records in these sequence ranges, and whether
	// every record in them was still there.
	Range(ranges []SequenceRange, maxEntries int) ([]spool.Entry, bool, error)
	// Accept removes the records whose event identities were durably taken,
	// and reports how many went. An identity that was never handed out removes
	// nothing: that is the property, not a message.
	Accept(accepted []metadata.UUID) (int, error)
	// RecordGapUnrecoverable writes the one bounded record saying a requested
	// range is no longer here.
	RecordGapUnrecoverable() error
}

// ErrStoreUnavailable is the store refusing to answer at all.
var ErrStoreUnavailable = errors.New("telemetry store unavailable")

// SpoolStore serves a spool the calling process owns.
//
// It remembers the batch it last handed out, because an acknowledgement names
// event identities and a spool removes by sequence. The caller that read those
// records is the one holding both numbers, and this is that caller. The memory
// is bounded by the batch and is also what makes it impossible to remove a
// record that was never handed over.
type SpoolStore struct {
	mu      sync.Mutex
	journal *spool.Spool
	handed  map[metadata.UUID]uint64
}

func NewSpoolStore(journal *spool.Spool) (*SpoolStore, error) {
	if journal == nil {
		return nil, ErrStoreUnavailable
	}
	return &SpoolStore{journal: journal, handed: map[metadata.UUID]uint64{}}, nil
}

// Batch reads the listing, which opens no record, and then the records it is
// handing back, one open each.
//
// Oldest first, because the sequence is what the server's cursor advances
// along: handing back a later record before an earlier one opens a gap that the
// repair path then has to close, for nothing.
//
// A sequence that has gone since the listing is skipped rather than failed, for
// the same reason reading one is an ordinary answer: records leave by eviction
// as well as by acknowledgement, and a listing is a moment old by the time it
// is used.
func (store *SpoolStore) Batch(limit int) ([]spool.Entry, error) {
	if store == nil || store.journal == nil || limit < 1 {
		return nil, ErrStoreUnavailable
	}
	sequences, err := store.journal.Sequences()
	if err != nil {
		return nil, err
	}
	if len(sequences) > limit {
		sequences = sequences[:limit]
	}
	entries := make([]spool.Entry, 0, len(sequences))
	for _, sequence := range sequences {
		entry, held, err := store.journal.Entry(sequence)
		if err != nil {
			return nil, err
		}
		if !held {
			continue
		}
		entries = append(entries, entry)
	}
	store.remember(entries)
	return entries, nil
}

func (store *SpoolStore) Range(
	ranges []SequenceRange,
	maxEntries int,
) ([]spool.Entry, bool, error) {
	if store == nil || store.journal == nil {
		return nil, false, ErrStoreUnavailable
	}
	if err := validateMissingRanges(ranges, ^uint64(0)); err != nil {
		return nil, false, err
	}
	spoolRanges := make([]spool.SequenceRange, 0, len(ranges))
	for _, item := range ranges {
		spoolRanges = append(spoolRanges, spool.SequenceRange{
			First: item.First,
			Last:  item.Last,
		})
	}
	entries, complete, err := store.journal.EntriesBySequenceRanges(
		spoolRanges,
		maxEntries,
	)
	if err != nil {
		return nil, false, ErrGapRepairRejected
	}
	store.remember(entries)
	return entries, complete, nil
}

// Accept removes what was taken, translating identities to sequences from what
// this store handed out. An identity it never handed out is not there to
// translate and removes nothing.
func (store *SpoolStore) Accept(accepted []metadata.UUID) (int, error) {
	if store == nil || store.journal == nil {
		return 0, ErrStoreUnavailable
	}
	store.mu.Lock()
	sequences := make([]uint64, 0, len(accepted))
	for _, eventID := range accepted {
		sequence, handed := store.handed[eventID]
		if !handed {
			continue
		}
		delete(store.handed, eventID)
		sequences = append(sequences, sequence)
	}
	store.mu.Unlock()
	if len(sequences) == 0 {
		return 0, nil
	}
	return store.journal.Acknowledge(sequences)
}

func (store *SpoolStore) RecordGapUnrecoverable() error {
	if store == nil || store.journal == nil {
		return ErrStoreUnavailable
	}
	encoded, err := event.Encode(event.SchemaDiagnostic, event.Diagnostic{
		Component: control.ComponentRuntime,
		Code:      event.DiagnosticTelemetryGapUnrecoverable,
		Count:     1,
	})
	if err != nil {
		return err
	}
	_, err = store.journal.Append(encoded)
	return err
}

// remember keeps the identities of what was just handed out, and only those.
// A batch replaces the one before it: the pass that asked for it is the pass
// that will acknowledge it, and a record handed out two passes ago is one the
// server never took.
func (store *SpoolStore) remember(entries []spool.Entry) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.handed = make(map[metadata.UUID]uint64, len(entries))
	for _, entry := range entries {
		store.handed[entry.Metadata.EventID] = entry.Sequence
	}
}
