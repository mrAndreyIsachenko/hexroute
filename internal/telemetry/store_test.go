package telemetry

import (
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// Nothing can be removed that was not handed over. This is the property the
// socket rests on: the thing that uploads names identities, and the store can
// only translate the ones it served.
func TestAcceptRemovesNothingItDidNotHandOut(t *testing.T) {
	journal := newJournal(t)
	for index := 0; index < 3; index++ {
		appendObservation(t, journal)
	}
	store := newStore(t, journal)

	// An identity shaped like one of ours and belonging to nothing.
	removed, err := store.Accept([]metadata.UUID{
		metadata.UUID("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if removed != 0 {
		t.Fatalf("an identity that was never handed out removed %d records", removed)
	}
	held, err := journal.Sequences()
	if err != nil {
		t.Fatalf("sequences: %v", err)
	}
	if len(held) != 3 {
		t.Fatalf("the store holds %d records, held 3 before the acknowledgement",
			len(held))
	}
}

// A record handed out and then acknowledged goes, and acknowledging it twice
// does not go looking for it again.
func TestAcceptRemovesWhatItHandedOutOnce(t *testing.T) {
	journal := newJournal(t)
	for index := 0; index < 2; index++ {
		appendObservation(t, journal)
	}
	store := newStore(t, journal)
	entries, err := store.Batch(MaxBatchEvents)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the batch holds %d records, want 2", len(entries))
	}

	first := []metadata.UUID{entries[0].Metadata.EventID}
	removed, err := store.Accept(first)
	if err != nil || removed != 1 {
		t.Fatalf("Accept = %d, %v; want 1, nil", removed, err)
	}
	settled := journal.Opens()
	removed, err = store.Accept(first)
	if err != nil || removed != 0 {
		t.Fatalf("a second acknowledgement = %d, %v; want 0, nil", removed, err)
	}
	// And it costs nothing to say no: the identity is gone from what the store
	// handed out, so there is nothing to look for.
	if opened := journal.Opens() - settled; opened != 0 {
		t.Fatalf("a repeated acknowledgement opened %d records", opened)
	}
}

// What the store remembers is bounded by one batch.
//
// It has to remember something — an acknowledgement names identities and the
// spool removes by sequence — and a memory that grew with every pass would be
// an unbounded map in a process that runs for weeks. A batch replaces the one
// before it rather than adding to it.
//
// The first draft of this test asserted that an identity from the previous
// batch is forgotten, which is not true and could not be: records leave only by
// acknowledgement, so two successive batches begin at the same record. The
// property is the bound, not the forgetting.
func TestWhatTheStoreRemembersIsBoundedByOneBatch(t *testing.T) {
	journal := newJournal(t)
	const stored = 12
	for index := 0; index < stored; index++ {
		appendObservation(t, journal)
	}
	store := newStore(t, journal)

	for pass := 0; pass < 3; pass++ {
		entries, err := store.Batch(4)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if len(entries) != 4 {
			t.Fatalf("pass %d handed out %d records, want 4", pass, len(entries))
		}
		store.mu.Lock()
		remembered := len(store.handed)
		store.mu.Unlock()
		if remembered != 4 {
			t.Fatalf("after pass %d the store remembers %d identities, want 4",
				pass, remembered)
		}
	}
}

// There is no way to put an arbitrary record into the store through this. The
// only write besides removal is the one fixed diagnostic, and that it is a
// method rather than an argument is the point.
func TestTheOnlyWriteIsTheFixedDiagnostic(t *testing.T) {
	journal := newJournal(t)
	store := newStore(t, journal)
	if err := store.RecordGapUnrecoverable(); err != nil {
		t.Fatalf("RecordGapUnrecoverable: %v", err)
	}
	entries, err := journal.Entries()
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the store holds %d records, want the one diagnostic", len(entries))
	}
	var store2 Store = store
	if _, ok := store2.(interface {
		Append([]byte) (uint64, error)
	}); ok {
		t.Fatal("the store exposes a way to append an arbitrary record")
	}
}

// A store with no spool answers rather than panicking, because a misconfigured
// agent is a thing that happens and a crash says less than a refusal.
func TestAStoreWithNoSpoolRefuses(t *testing.T) {
	if _, err := NewSpoolStore(nil); err == nil {
		t.Fatal("a store was built over no spool")
	}
	var store *SpoolStore
	if _, err := store.Batch(MaxBatchEvents); err == nil {
		t.Fatal("a store with no spool handed back a batch")
	}
	if _, err := store.Accept(nil); err == nil {
		t.Fatal("a store with no spool accepted an acknowledgement")
	}
	if err := store.RecordGapUnrecoverable(); err == nil {
		t.Fatal("a store with no spool recorded a diagnostic")
	}
}
