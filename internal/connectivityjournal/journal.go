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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	// Mirror receives a copy of every record the journal writes.
	//
	// It is here rather than at the call site so that "everything journaled is
	// also retained" is true by construction. Two places agreeing to encode
	// the same fact the same way is an agreement that eventually stops
	// holding, and the copy would then silently describe something else.
	Mirror Sink
}

// Sink receives records the journal wrote, for durable local retention.
//
// It is not evidence. The journal is what the lineage is replayed from, and a
// sink that fails costs a copy rather than a write — so a failure here is
// counted and reported, never returned.
type Sink interface {
	Append(encoded []byte) (uint64, error)
}

// Journal is one domain's connectivity fact store.
type Journal struct {
	spool  *spool.Spool
	domain policy.Domain
	// superseded records that this journal began because the one on disk was
	// written in a format this build does not speak.
	superseded bool

	mirror         Sink
	mirrorMu       sync.Mutex
	mirrorFailures uint64
}

// MirrorFailures reports how many records the journal wrote and the sink did
// not take. It is exposed because a mirror that quietly stopped keeping up
// would leave a retention store that looks complete and is not.
func (journal *Journal) MirrorFailures() uint64 {
	if journal == nil {
		return 0
	}
	journal.mirrorMu.Lock()
	defer journal.mirrorMu.Unlock()
	return journal.mirrorFailures
}

// Superseded reports whether opening this journal set an unreadable one aside.
// The caller decides what to say about it; the journal only refuses to pretend
// it did not happen.
func (journal *Journal) Superseded() bool { return journal.superseded }

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
// RecordFormat names the shape of the records this build writes.
//
// It exists because a journal has to tell a format it does not speak from a
// journal that is damaged, and until it could, it treated both as damage.
// That is not a small difference: records from an older build made the read
// model refuse to start, every ten seconds, for as long as they were on disk,
// and no amount of restarting could clear it.
const RecordFormat = "hexroute.connectivity-journal.folded.v1"

// The journal's directory holds the spool and the journal's own marker beside
// it. The spool owns everything in its directory and refuses a file it does
// not recognise — deliberately, so a stray one is noticed rather than ignored
// — and writing the marker as a sibling of the journal would put it outside
// the store the runtime was handed, which an architectural test refuses. So
// the spool moves down one level and the journal keeps a directory of its own.
const (
	formatFilename = ".record-format"
	spoolDirectory = "spool"
)

// Open prepares the journal for one privilege domain.
//
// A journal written in a format this build does not speak is set aside rather
// than read or deleted. Reading it is impossible, refusing to start leaves a
// host with no read model at all, and deleting it would throw away evidence
// nobody asked to lose — so it keeps its records under a name that says they
// were superseded, and a new journal begins.
func Open(path string, domain policy.Domain, options Options) (*Journal, error) {
	owner := spool.OwnerRoot
	switch domain {
	case policy.DomainRoot:
	case policy.DomainUser:
		owner = spool.OwnerUser
	default:
		return nil, fmt.Errorf("%w: domain %q", ErrDomainMismatch, domain)
	}
	superseded, err := supersedeForeignFormat(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	store, err := spool.Open(filepath.Join(path, spoolDirectory), owner, spool.Options{
		MaxBytes: options.MaxBytes,
		NodeID:   options.NodeID,
		Clock:    options.Clock,
	})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(path, formatFilename),
		[]byte(RecordFormat+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	return &Journal{spool: store, domain: domain, superseded: superseded,
		mirror: options.Mirror}, nil
}

// supersedeForeignFormat moves aside a journal whose records this build cannot
// read, and reports whether it did.
//
// A journal with no marker and no records is simply new. One with no marker
// and records in it was written before the marker existed, which is the same
// answer as a marker that does not match: not ours.
func supersedeForeignFormat(path string) (bool, error) {
	marker, err := os.ReadFile(filepath.Join(path, formatFilename))
	switch {
	case err == nil && strings.TrimSpace(string(marker)) == RecordFormat:
		return false, nil
	case err != nil && !os.IsNotExist(err):
		return false, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	holds := false
	for _, entry := range entries {
		if entry.Name() != formatFilename {
			holds = true
			break
		}
	}
	if !holds {
		return false, nil
	}
	// Named for what it is, not numbered: a second supersession would find
	// the name taken, and losing the first one is exactly what must not
	// happen quietly.
	aside := path + ".superseded"
	if _, err := os.Stat(aside); err == nil {
		return false, fmt.Errorf(
			"%w: a superseded journal is already set aside at %s", ErrCorruptRecord, aside)
	}
	if err := os.Rename(path, aside); err != nil {
		return false, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	return true, nil
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
	if _, err := journal.spool.Append(encoded); err != nil {
		return err
	}
	journal.mirrorRecord(encoded)
	return nil
}

// mirrorRecord copies a written record to the sink. The journal has already
// succeeded by this point, so nothing here may fail the append.
func (journal *Journal) mirrorRecord(encoded []byte) {
	if journal.mirror == nil {
		return
	}
	if _, err := journal.mirror.Append(encoded); err != nil {
		journal.mirrorMu.Lock()
		journal.mirrorFailures++
		journal.mirrorMu.Unlock()
	}
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
