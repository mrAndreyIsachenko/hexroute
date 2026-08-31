// Package connectivityjournal stores accepted connectivity facts in the
// existing crash-safe priority journal.
//
// Each privilege domain keeps its own journal, so a user fact never lands in
// root's store and vice versa. Records carry the durable host acceptance
// sequence, which is what makes replay an ordering problem rather than a
// reconstruction problem.
package connectivityjournal

import (
	"errors"
	"fmt"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
)

var (
	ErrDomainMismatch = errors.New("fact does not belong to this journal")
	ErrCorruptRecord  = errors.New("journalled connectivity record is unusable")
	ErrSequenceReused = errors.New("host acceptance sequence was reused")
)

// Options configures a journal.
type Options struct {
	MaxBytes int64
	NodeID   metadata.UUID
	Clock    metadata.Clock
}

// Journal is one domain's connectivity fact store.
type Journal struct {
	spool  *spool.Spool
	domain policy.Domain
}

// Record is one journalled fact with its acceptance metadata.
type Record struct {
	// HostSequence is the accepted order, and is zero for an event that never
	// entered it.
	HostSequence uint64
	// FoldPosition is the order every folded event has, accepted or not. It
	// is what replay reads the stream back in.
	FoldPosition uint64
	Outcome      string
	Role         safety.SourceRole
	Digest       string
	Fact         connectivity.Fact
}

// Open prepares the journal for one privilege domain.
func Open(path string, domain policy.Domain, options Options) (*Journal, error) {
	owner := spool.OwnerRoot
	switch domain {
	case policy.DomainRoot:
	case policy.DomainUser:
		owner = spool.OwnerUser
	default:
		return nil, fmt.Errorf("%w: domain %q", ErrDomainMismatch, domain)
	}
	store, err := spool.Open(path, owner, spool.Options{
		MaxBytes: options.MaxBytes,
		NodeID:   options.NodeID,
		Clock:    options.Clock,
	})
	if err != nil {
		return nil, err
	}
	return &Journal{spool: store, domain: domain}, nil
}

// Domain reports which privilege domain this journal belongs to.
func (journal *Journal) Domain() policy.Domain { return journal.domain }

// Append writes one accepted fact.
//
// The fact is validated against the compiled ownership envelope again here.
// The acceptor has already done so, but a journal that trusted its caller
// would be a way to write a fact nobody ever accepted.
// Append records one folded event.
//
// Everything a reduction was given is recorded, not only what it accepted. A
// duplicate, a conflict and a late arrival all change what the reduction
// concludes — a conflict is kept in the aggregate state and a restatement is
// owed after one — so a journal holding only the accepted facts cannot
// reproduce the conclusion, and the lineage reports the difference as the
// conclusion contradicting its own evidence.
func (journal *Journal) Append(
	fact connectivity.Fact,
	hostSequence uint64,
	foldPosition uint64,
	outcome string,
	role safety.SourceRole,
) error {
	if fact.Domain != journal.domain {
		return fmt.Errorf("%w: %q in the %q journal",
			ErrDomainMismatch, fact.Domain, journal.domain)
	}
	if _, err := safety.ClassifyConnectivityFact(fact, journal.domain); err != nil {
		return err
	}
	if foldPosition == 0 || outcome == "" {
		return fmt.Errorf("%w: fold position", ErrCorruptRecord)
	}
	schema, record, err := event.CanonicalConnectivityRecord(
		fact, hostSequence, foldPosition, outcome, string(role))
	if err != nil {
		return err
	}
	encoded, err := event.Encode(schema, record)
	if err != nil {
		return err
	}
	_, err = journal.spool.Append(encoded)
	return err
}

// Records returns every retained event in the order it was folded.
//
// A record that cannot be decoded is an error rather than a skip: silently
// dropping one would turn a corrupt journal into a shorter healthy-looking
// one.
func (journal *Journal) Records() ([]Record, error) {
	entries, err := journal.spool.Entries()
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	seen := make(map[uint64]string, len(entries))
	for _, entry := range entries {
		decoded, err := event.Decode(entry.Event)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
		}
		payload, ok := decoded.Payload.(*event.ConnectivityFact)
		if !ok {
			// The journal is shared with other event classes; anything that
			// is not a connectivity record simply is not ours.
			continue
		}
		fact, err := event.DecodeConnectivityFact(*payload)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
		}
		// A reused fold position is two different events claiming one place
		// in the order, which is the same corruption a reused host sequence
		// was, one layer out.
		if previous, clash := seen[payload.FoldPosition]; clash && previous != payload.Digest {
			return nil, fmt.Errorf("%w: fold position %d",
				ErrSequenceReused, payload.FoldPosition)
		}
		seen[payload.FoldPosition] = payload.Digest
		records = append(records, Record{
			HostSequence: payload.HostSequence,
			FoldPosition: payload.FoldPosition,
			Outcome:      payload.Outcome,
			Role:         safety.SourceRole(payload.Role),
			Digest:       payload.Digest,
			Fact:         fact,
		})
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].FoldPosition < records[j].FoldPosition
	})
	return records, nil
}

// RecordsAfter returns the retained events folded after a watermark, and
// whether that range is continuous.
//
// Replay needs both answers. A continuous range can be folded forward; a
// broken one means the journal cannot prove what happened, which is a reason
// to publish uncertainty rather than to fold what remains.
func (journal *Journal) RecordsAfter(watermark uint64) ([]Record, bool, error) {
	records, err := journal.Records()
	if err != nil {
		return nil, false, err
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if record.FoldPosition > watermark {
			out = append(out, record)
		}
	}
	expected := watermark + 1
	for _, record := range out {
		if record.FoldPosition != expected {
			return out, false, nil
		}
		expected++
	}
	return out, true, nil
}

// LatestBaselines returns the newest retained complete restatement for every
// component that has one.
func (journal *Journal) LatestBaselines() (map[connectivity.Component]Record, error) {
	records, err := journal.Records()
	if err != nil {
		return nil, err
	}
	latest := make(map[connectivity.Component]Record)
	for _, record := range records {
		if !record.Fact.Baseline {
			continue
		}
		if existing, seen := latest[record.Fact.Component]; seen &&
			existing.HostSequence > record.HostSequence {
			continue
		}
		latest[record.Fact.Component] = record
	}
	return latest, nil
}
