package spool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

// stableRecord is what the spool can learn about a stored record without
// opening it: which sequence it holds, and how many bytes it occupies.
//
// Both come from the directory itself. The sequence is the filename, and the
// size is what the filesystem reports — the same number readEntry recomputes by
// re-marshalling the record and then compares against this one.
type stableRecord struct {
	Sequence uint64
	Size     int64
	// Priority is the class eviction would place this record in, and is empty
	// until something needs to evict.
	//
	// It is the one thing eviction needs that the directory cannot report, and
	// a spool below its bound never asks for it. Holding it here rather than in
	// a store of its own means it is dropped exactly when the listing is
	// dropped, so nothing has to reason about a remembered class belonging to a
	// record that has gone.
	Priority event.Priority
}

// scanIndex lists the stable records without reading any of them.
//
// It exists because most of what the spool does never looks inside a record.
// Appending needs the highest sequence and the total size; reporting size needs
// the total; opening needs the highest. Decoding sixty thousand payloads to
// learn two numbers is the whole of the defect this replaces.
//
// What it still refuses is anything that makes the directory itself
// untrustworthy: a name that is not a record, a symlink, a subdirectory. Those
// are statements about the spool rather than about one record in it, and a
// spool whose shape is wrong cannot be reasoned about at all.
// index answers what the spool holds, listing the directory only when it does
// not already know.
//
// Listing is O(records stored) and an append needs the listing, so taking it
// per append made the cost O(stored x appended). A runtime appends eleven
// records per cycle into a spool that had reached six figures, and the listing
// is where a profile of the live process found it standing.
//
// The spool keeps what it learned and amends it as it publishes and evicts.
// Anything else that touches the stable records drops it instead: acknowledging
// an upload and quarantining a record are rare beside appending, and paying a
// listing there is cheaper than reasoning about whether the amendment is right.
func (spool *Spool) index() ([]stableRecord, error) {
	if spool.indexed != nil {
		return spool.indexed, nil
	}
	records, err := spool.scanIndex()
	if err != nil {
		return nil, err
	}
	spool.indexed = records
	return records, nil
}

// forgetIndex drops what the spool believed about its directory.
func (spool *Spool) forgetIndex() {
	spool.indexed = nil
}

// noteCommitted amends the listing with what was just published and without
// what was just evicted.
func (spool *Spool) noteCommitted(staged []Entry, evictions []stableRecord) {
	if spool.indexed == nil {
		return
	}
	gone := make(map[uint64]struct{}, len(evictions))
	for _, record := range evictions {
		gone[record.Sequence] = struct{}{}
	}
	kept := spool.indexed[:0]
	for _, record := range spool.indexed {
		if _, removed := gone[record.Sequence]; removed {
			continue
		}
		kept = append(kept, record)
	}
	for _, entry := range staged {
		info, err := os.Lstat(spool.stablePath(entry.Sequence))
		if err != nil {
			// It is published and its size is not known. Rather than guess,
			// the spool stops claiming to know its directory.
			spool.indexed = nil
			return
		}
		// A record the spool published carries its class from the append that
		// wrote it, so eviction never opens what this process stored.
		kept = append(kept, stableRecord{
			Sequence: entry.Sequence,
			Size:     info.Size(),
			Priority: entry.Priority,
		})
	}
	sort.Slice(kept, func(one, other int) bool {
		return kept[one].Sequence < kept[other].Sequence
	})
	spool.indexed = kept
}

// evictable answers what eviction may choose among: the listing, plus the class
// of every record in it.
//
// The class cannot come from the directory, so a record already on disk when the
// listing was taken has to be opened. It is opened once. The listing is kept
// across appends and amended as records are published and evicted, so the
// reading is paid per record rather than per append.
//
// That distinction is the whole of this change. A spool nothing drains reaches
// its byte bound and stays there, and the path this replaces decoded every
// stored record on every append. On 2026-09-12 the live root runtime met it: the
// user journal's spool stood at 104,858,244 bytes against a bound of
// 104,857,600, holding 84,067 records, and the daemon spent four hours in
// canonicalisation without completing one append or recording that it had
// stopped.
func (spool *Spool) evictable() ([]stableRecord, error) {
	// Each pass sets aside at least one record and adds none, so the directory
	// strictly shrinks and this terminates.
	for {
		records, err := spool.index()
		if err != nil {
			return nil, err
		}
		unclassified := make([]uint64, 0)
		for position := range records {
			if records[position].Priority != "" {
				continue
			}
			priority, err := spool.readPriority(records[position].Sequence)
			if err != nil {
				unclassified = append(unclassified, records[position].Sequence)
				continue
			}
			records[position].Priority = priority
		}
		if len(unclassified) == 0 {
			return records, nil
		}
		// Eviction cannot place a record it cannot classify, and refusing the
		// append would lose a new observation to an old damaged one.
		for _, sequence := range unclassified {
			if err := spool.quarantineLocked(sequence); err != nil {
				return nil, err
			}
		}
		spool.forgetIndex()
	}
}

// readPriority answers which class a stored record belongs to, and reads
// nothing else.
//
// It decodes the envelope and stops: no canonicalisation, no re-marshalling,
// and no look at the event the record carries. Eviction does not use the event,
// and proving it here is the payment that wedged the runtime.
//
// The listing already refused anything that is not a plain, tightly permissioned
// file, so this does not restate that. What it does check is that the record
// agrees with the name it is filed under, because a record answering for a
// sequence that is not its own would be evicted in the wrong order.
func (spool *Spool) readPriority(sequence uint64) (event.Priority, error) {
	spool.opens++
	data, err := os.ReadFile(spool.stablePath(sequence))
	if err != nil {
		return "", err
	}
	var envelope struct {
		Schema   string         `json:"schema"`
		Sequence uint64         `json:"sequence"`
		Priority event.Priority `json:"priority"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", ErrCorruptSpool
	}
	if envelope.Schema != entrySchema || envelope.Sequence != sequence {
		return "", ErrCorruptSpool
	}
	switch envelope.Priority {
	case event.PriorityCritical,
		event.PriorityOperational,
		event.PriorityDiagnostic:
		return envelope.Priority, nil
	}
	return "", ErrCorruptSpool
}

func (spool *Spool) scanIndex() ([]stableRecord, error) {
	spool.scans++
	directoryEntries, err := os.ReadDir(spool.path)
	if err != nil {
		return nil, fmt.Errorf("read spool directory: %w", err)
	}

	records := make([]stableRecord, 0, len(directoryEntries))
	seen := make(map[uint64]struct{}, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		name := directoryEntry.Name()
		if isReservedName(name) {
			continue
		}
		sequence, ok := parseStableName(name)
		if !ok || directoryEntry.Type()&os.ModeSymlink != 0 || directoryEntry.IsDir() {
			return nil, ErrCorruptSpool
		}
		if _, duplicate := seen[sequence]; duplicate {
			return nil, ErrCorruptSpool
		}
		info, err := os.Lstat(filepath.Join(spool.path, name))
		if err != nil {
			return nil, fmt.Errorf("inspect spool record: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, ErrCorruptSpool
		}
		seen[sequence] = struct{}{}
		records = append(records, stableRecord{Sequence: sequence, Size: info.Size()})
	}
	sort.Slice(records, func(left, right int) bool {
		return records[left].Sequence < records[right].Sequence
	})
	return records, nil
}

func indexLastSequence(records []stableRecord) uint64 {
	if len(records) == 0 {
		return 0
	}
	return records[len(records)-1].Sequence
}

func indexTotalSize(records []stableRecord) int64 {
	var total int64
	for _, record := range records {
		total += record.Size
	}
	return total
}
