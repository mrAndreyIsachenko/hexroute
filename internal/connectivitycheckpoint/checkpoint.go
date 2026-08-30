// Package connectivitycheckpoint persists the aggregate connectivity read
// model so it can be resumed, and proves that what it resumed is what was
// written.
//
// A checkpoint is not a cache of the latest snapshot. It is a link in a chain:
// it names its parent, the snapshot it started from, the exact range of
// accepted facts it consumed, the policy and reducer that produced it, and the
// digests of everything it produced. That is what lets startup tell a
// checkpoint it can trust from one that merely looks healthy.
package connectivitycheckpoint

import (
	"errors"
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	// CheckpointSchema names the wire contract for one checkpoint record.
	CheckpointSchema = "hexroute.connectivity-checkpoint.v1"
	// CheckpointSchemaVersion is bumped only for an incompatible change.
	CheckpointSchemaVersion uint16 = 1

	// IndexSchema names one append-only lineage entry.
	IndexSchema = "hexroute.connectivity-checkpoint-index.v1"
	// PointerSchema names the mutable latest pointer.
	PointerSchema = "hexroute.connectivity-checkpoint-pointer.v1"

	// MaxCheckpointBytes bounds one persisted checkpoint record.
	MaxCheckpointBytes = 64 * 1024
	// MaxIndexEntries bounds the retained lineage. Beyond it the oldest
	// entries are evicted and the loss is recorded rather than hidden.
	MaxIndexEntries = 64
	// DefaultMaxRecoveryDepth bounds how far startup may walk backwards
	// looking for a checkpoint it can prove.
	DefaultMaxRecoveryDepth = 8
)

var (
	ErrInvalidCheckpoint = errors.New("connectivity checkpoint is invalid")
	ErrLineageBroken     = errors.New("connectivity checkpoint lineage cannot be proven")
	ErrGenerationGuard   = errors.New("connectivity checkpoint generation guard refused the write")
	ErrStoreUnavailable  = errors.New("connectivity checkpoint store is unavailable")
	ErrNotFound          = errors.New("connectivity checkpoint not found")
	ErrInjectedFault     = errors.New("connectivity checkpoint write was interrupted")
)

// Checkpoint is one durable link in the read-model lineage.
type Checkpoint struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	// ID is immutable and addresses this checkpoint in the lineage.
	ID string `json:"id"`
	// Parent is the previous checkpoint, empty only at genesis.
	Parent string `json:"parent,omitempty"`
	// ParentDigest binds this record to that exact parent content, so a
	// substituted ancestor breaks the chain instead of extending it.
	ParentDigest string `json:"parent_digest,omitempty"`

	// PriorSnapshotDigest is the input the reduction started from.
	PriorSnapshotDigest string `json:"prior_snapshot_digest,omitempty"`
	// ConsumedFrom and ConsumedTo bound the accepted facts folded in. The
	// range is what makes replay from a journal an ordering problem.
	ConsumedFrom uint64 `json:"consumed_from"`
	ConsumedTo   uint64 `json:"consumed_to"`

	SourceWatermarks []connectivityreduce.SourceWatermark `json:"source_watermarks"`

	Policy         connectivityreduce.PolicyDescriptor `json:"policy"`
	ReducerID      string                              `json:"reducer_id"`
	ReducerVersion uint16                              `json:"reducer_version"`

	SnapshotGeneration uint64 `json:"snapshot_generation"`
	SnapshotDigest     string `json:"snapshot_digest"`
	// Snapshot is the read model itself. Resuming needs the state, not only
	// its name, and SnapshotDigest is what binds the two together.
	Snapshot        connectivityreduce.Snapshot `json:"snapshot"`
	DiffDigest      string                      `json:"diff_digest"`
	ProposalsDigest string                      `json:"proposals_digest"`

	// Digest addresses this record. It covers every other field.
	Digest string `json:"digest"`
}

func checkpointBody(checkpoint Checkpoint) Checkpoint {
	checkpoint.Digest = ""
	return checkpoint
}

// Seal computes and attaches the checkpoint's own address.
func Seal(checkpoint Checkpoint) (Checkpoint, error) {
	checkpoint.Schema = CheckpointSchema
	checkpoint.Version = CheckpointSchemaVersion
	digest, _, err := policy.CanonicalSHA256(checkpointBody(checkpoint))
	if err != nil {
		return Checkpoint{}, fmt.Errorf("%w: encoding", ErrInvalidCheckpoint)
	}
	checkpoint.Digest = digest
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// Validate rejects a record that is internally inconsistent or whose address
// does not match its content.
func (checkpoint Checkpoint) Validate() error {
	if checkpoint.Schema != CheckpointSchema ||
		checkpoint.Version != CheckpointSchemaVersion {
		return fmt.Errorf("%w: schema", ErrInvalidCheckpoint)
	}
	if !validIdentifier(checkpoint.ID) {
		return fmt.Errorf("%w: id", ErrInvalidCheckpoint)
	}
	if checkpoint.Parent == "" {
		if checkpoint.ParentDigest != "" {
			return fmt.Errorf("%w: genesis carries a parent digest", ErrInvalidCheckpoint)
		}
	} else {
		if !validIdentifier(checkpoint.Parent) || len(checkpoint.ParentDigest) != 64 {
			return fmt.Errorf("%w: parent binding", ErrInvalidCheckpoint)
		}
		if checkpoint.Parent == checkpoint.ID {
			return fmt.Errorf("%w: checkpoint is its own parent", ErrInvalidCheckpoint)
		}
	}
	if checkpoint.SnapshotGeneration == 0 || len(checkpoint.SnapshotDigest) != 64 ||
		len(checkpoint.DiffDigest) != 64 || len(checkpoint.ProposalsDigest) != 64 {
		return fmt.Errorf("%w: output digests", ErrInvalidCheckpoint)
	}
	if checkpoint.ReducerID != connectivityreduce.ReducerID ||
		checkpoint.ReducerVersion != connectivityreduce.ReducerVersion {
		return fmt.Errorf("%w: reducer identity", ErrInvalidCheckpoint)
	}
	// A reduction can be effective without consuming anything: a component
	// passing its freshness deadline changes the read model on its own. Such
	// a checkpoint folded no facts, and says so with an absent range rather
	// than an inverted one.
	if checkpoint.ConsumedFrom != 0 && checkpoint.ConsumedTo < checkpoint.ConsumedFrom {
		return fmt.Errorf("%w: consumed range", ErrInvalidCheckpoint)
	}
	if err := checkpoint.Snapshot.Validate(); err != nil {
		return fmt.Errorf("%w: snapshot", ErrInvalidCheckpoint)
	}
	// The carried snapshot must be the one this checkpoint names, or the
	// digest would attest to something the record does not hold.
	snapshotDigest, err := checkpoint.Snapshot.Digest()
	if err != nil {
		return fmt.Errorf("%w: snapshot digest", ErrInvalidCheckpoint)
	}
	if snapshotDigest != checkpoint.SnapshotDigest {
		return fmt.Errorf("%w: snapshot does not match its digest", ErrInvalidCheckpoint)
	}
	if checkpoint.Snapshot.Generation != checkpoint.SnapshotGeneration {
		return fmt.Errorf("%w: snapshot generation", ErrInvalidCheckpoint)
	}
	if checkpoint.Snapshot.ConsumedHostSequence != checkpoint.ConsumedTo {
		return fmt.Errorf("%w: consumed watermark", ErrInvalidCheckpoint)
	}
	// The top-level watermarks exist so a reader can see integrity without
	// decoding the snapshot. They may not say anything different from it.
	if len(checkpoint.SourceWatermarks) != len(checkpoint.Snapshot.Sources) {
		return fmt.Errorf("%w: watermark count", ErrInvalidCheckpoint)
	}
	for index, watermark := range checkpoint.SourceWatermarks {
		carried := checkpoint.Snapshot.Sources[index]
		if watermark.Source != carried.Source || watermark.BootID != carried.BootID ||
			watermark.LastSequence != carried.LastSequence ||
			watermark.Conflicts != carried.Conflicts ||
			watermark.AwaitingBaseline() != carried.AwaitingBaseline() ||
			len(watermark.PendingBaseline) != len(carried.PendingBaseline) ||
			len(watermark.Gaps) != len(carried.Gaps) {
			return fmt.Errorf("%w: watermark %q disagrees with the snapshot",
				ErrInvalidCheckpoint, watermark.Source)
		}
	}
	expected, _, err := policy.CanonicalSHA256(checkpointBody(checkpoint))
	if err != nil {
		return fmt.Errorf("%w: encoding", ErrInvalidCheckpoint)
	}
	if expected != checkpoint.Digest {
		return fmt.Errorf("%w: digest", ErrInvalidCheckpoint)
	}
	return nil
}

// IndexEntry is one bounded append-only lineage record. It exists so the
// lineage survives independently of the mutable latest pointer.
type IndexEntry struct {
	Schema     string `json:"schema"`
	Sequence   uint64 `json:"sequence"`
	ID         string `json:"id"`
	Parent     string `json:"parent,omitempty"`
	Digest     string `json:"digest"`
	Generation uint64 `json:"snapshot_generation"`
	ConsumedTo uint64 `json:"consumed_to"`
}

// Pointer names the newest checkpoint the store believes in.
type Pointer struct {
	Schema     string `json:"schema"`
	ID         string `json:"id"`
	Digest     string `json:"digest"`
	Generation uint64 `json:"snapshot_generation"`
	Sequence   uint64 `json:"index_sequence"`
	// Overflow records that older lineage was evicted, so a walk that runs
	// out of ancestors can say whether they never existed or were dropped.
	Overflow bool `json:"lineage_overflow"`
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}
