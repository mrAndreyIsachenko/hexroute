package connectivitycheckpoint

import (
	"errors"
	"fmt"
)

// ResumeStatus is how startup ended up.
type ResumeStatus string

const (
	// ResumeGenesis means nothing has been checkpointed yet.
	ResumeGenesis ResumeStatus = "genesis"
	// ResumeLatest means the newest checkpoint proved itself.
	ResumeLatest ResumeStatus = "latest"
	// ResumeAncestor means the newest checkpoint did not, and an older one
	// that did was used instead.
	ResumeAncestor ResumeStatus = "recovered_ancestor"
	// ResumeUnrecoverable means nothing within the bound could be proven.
	// It is a publishable state, not a crash: unknown is an answer.
	ResumeUnrecoverable ResumeStatus = "unrecoverable"
)

// ResumeReason is the bounded explanation for the status.
type ResumeReason string

const (
	ResumeReasonNone           ResumeReason = "none"
	ResumeReasonNoPointer      ResumeReason = "no_pointer"
	ResumeReasonPointerInvalid ResumeReason = "pointer_invalid"
	ResumeReasonRecordMissing  ResumeReason = "record_missing"
	ResumeReasonRecordInvalid  ResumeReason = "record_invalid"
	ResumeReasonParentBroken   ResumeReason = "parent_link_broken"
	ResumeReasonDigestMismatch ResumeReason = "digest_mismatch"
	ResumeReasonDepthExhausted ResumeReason = "recovery_depth_exhausted"
	ResumeReasonLineageEvicted ResumeReason = "lineage_evicted"
)

// Resume is what startup concluded about the stored lineage.
type Resume struct {
	Status     ResumeStatus
	Reason     ResumeReason
	Checkpoint *Checkpoint
	// Depth is how many candidates back from the pointer were tried.
	Depth int
	// LineageOverflow reports that older lineage was evicted, so an ancestor
	// that cannot be found may have existed rather than never have been.
	LineageOverflow bool
}

// Usable reports whether a checkpoint may be resumed from.
func (resume Resume) Usable() bool {
	return resume.Checkpoint != nil &&
		(resume.Status == ResumeLatest || resume.Status == ResumeAncestor)
}

// Resume validates the stored lineage and returns the newest checkpoint it can
// prove.
//
// It never returns a checkpoint it could not verify. When nothing within the
// configured bound can be proven the answer is unrecoverable, which the caller
// publishes as unknown state — a plausible healthy snapshot is exactly what
// must not be selected here.
func (store *Store) Resume() (Resume, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	pointer, err := store.pointerLocked()
	switch {
	case errors.Is(err, ErrNotFound):
		// A store with lineage but no pointer is a crash between the index
		// entry and the pointer write. The lineage is what survives.
		entries, indexErr := store.indexLocked()
		if indexErr != nil {
			return Resume{}, indexErr
		}
		if len(entries) == 0 {
			return Resume{Status: ResumeGenesis, Reason: ResumeReasonNone}, nil
		}
		return store.searchLocked(entries, len(entries)-1, false, ResumeReasonNoPointer)
	case err != nil:
		entries, indexErr := store.indexLocked()
		if indexErr != nil {
			return Resume{}, indexErr
		}
		if len(entries) == 0 {
			return Resume{
				Status: ResumeUnrecoverable, Reason: ResumeReasonPointerInvalid,
			}, nil
		}
		return store.searchLocked(entries, len(entries)-1, false, ResumeReasonPointerInvalid)
	}

	entries, err := store.indexLocked()
	if err != nil {
		return Resume{}, err
	}
	start := len(entries) - 1
	for index := range entries {
		if entries[index].ID == pointer.ID {
			start = index
		}
	}
	resume, err := store.searchLocked(entries, start, pointer.Overflow, ResumeReasonNone)
	if err != nil {
		return Resume{}, err
	}
	if resume.Status == ResumeAncestor && resume.Depth == 0 {
		resume.Status = ResumeLatest
	}
	return resume, nil
}

// searchLocked walks backwards from a starting lineage entry looking for the
// newest checkpoint whose record and retained ancestry it can prove.
func (store *Store) searchLocked(
	entries []IndexEntry,
	start int,
	overflow bool,
	initial ResumeReason,
) (Resume, error) {
	reason := initial
	// firstFailure is why the search had to move at all. Reporting an
	// ancestor without it leaves an operator told only that the newest
	// checkpoint was not used, when a corrupt record, a missing ancestor and
	// a substituted parent call for three different responses.
	firstFailure := ResumeReasonNone
	for depth := 0; depth <= store.depth; depth++ {
		position := start - depth
		if position < 0 {
			status := ResumeUnrecoverable
			if reason == ResumeReasonNone {
				reason = ResumeReasonRecordMissing
			}
			if overflow {
				reason = ResumeReasonLineageEvicted
			}
			return Resume{
				Status: status, Reason: reason, Depth: depth,
				LineageOverflow: overflow,
			}, nil
		}
		candidate, failure := store.proveLocked(entries[position], overflow)
		if failure == ResumeReasonNone {
			status := ResumeAncestor
			if depth == 0 && initial == ResumeReasonNone {
				status = ResumeLatest
			}
			reported := initial
			if reported == ResumeReasonNone {
				reported = firstFailure
			}
			return Resume{
				Status: status, Reason: reported, Checkpoint: candidate,
				Depth: depth, LineageOverflow: overflow,
			}, nil
		}
		if firstFailure == ResumeReasonNone {
			firstFailure = failure
		}
		if reason == ResumeReasonNone || depth > 0 {
			reason = failure
		}
	}
	return Resume{
		Status: ResumeUnrecoverable, Reason: ResumeReasonDepthExhausted,
		Depth: store.depth, LineageOverflow: overflow,
	}, nil
}

// proveLocked verifies one candidate and its retained ancestry.
func (store *Store) proveLocked(entry IndexEntry, overflow bool) (*Checkpoint, ResumeReason) {
	checkpoint, err := store.Load(entry.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, ResumeReasonRecordMissing
	case err != nil:
		return nil, ResumeReasonRecordInvalid
	}
	// The lineage entry and the record must agree, or the index is naming
	// something other than what is stored.
	if checkpoint.Digest != entry.Digest || checkpoint.Parent != entry.Parent ||
		checkpoint.SnapshotGeneration != entry.Generation ||
		checkpoint.ConsumedTo != entry.ConsumedTo {
		return nil, ResumeReasonDigestMismatch
	}

	current := checkpoint
	for steps := 0; current.Parent != ""; steps++ {
		if steps >= store.depth {
			// The retained ancestry is longer than we are allowed to walk.
			// The candidate is still the newest thing we could prove within
			// the bound, and the bound is a configured choice, not a defect.
			break
		}
		parent, err := store.Load(current.Parent)
		switch {
		case errors.Is(err, ErrNotFound):
			if overflow {
				// The ancestor was evicted rather than lost. The chain we
				// could retain is intact up to the horizon.
				return &checkpoint, ResumeReasonNone
			}
			return nil, ResumeReasonParentBroken
		case err != nil:
			return nil, ResumeReasonRecordInvalid
		}
		if parent.Digest != current.ParentDigest {
			return nil, ResumeReasonParentBroken
		}
		current = parent
	}
	return &checkpoint, ResumeReasonNone
}

// String renders a resume for a bounded diagnostic line.
func (resume Resume) String() string {
	id := "none"
	if resume.Checkpoint != nil {
		id = resume.Checkpoint.ID
	}
	return fmt.Sprintf("status=%s reason=%s checkpoint=%s depth=%d overflow=%t",
		resume.Status, resume.Reason, id, resume.Depth, resume.LineageOverflow)
}
