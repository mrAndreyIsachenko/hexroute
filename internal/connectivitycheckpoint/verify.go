package connectivitycheckpoint

import (
	"errors"
	"fmt"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
)

// A checkpoint says what it concluded. Verification asks whether the retained
// facts still say the same thing.
//
// This is the difference between a lineage that is well-formed and one that is
// true. Append already refuses a record whose parent link or internal digests
// do not hold; nothing until now re-derived the snapshot, diff and proposals
// from the journal and compared them to what was written. A record can be
// perfectly formed and describe a reduction that the same facts would not
// produce again — because the rules changed, because the journal lost
// something, or because somebody rewrote one of them.
//
// It runs offline against a stored lineage and changes nothing. It opens no
// socket, reduces under the policy descriptor each checkpoint recorded rather
// than a current one, and never moves the active pointer.

// VerifyStatus is what verification concluded about one link.
type VerifyStatus string

const (
	// VerifyReproduced means replaying the retained facts onto the parent
	// produced exactly the outputs the checkpoint recorded.
	VerifyReproduced VerifyStatus = "reproduced"
	// VerifyDiverged means it produced different outputs. This is the finding
	// the qualification gate exists to refuse: a published conclusion the
	// evidence no longer supports.
	VerifyDiverged VerifyStatus = "diverged"
	// VerifyUnreplayable means the retained facts cannot reconstruct the
	// link — the journal no longer holds its range. It is not a divergence:
	// nothing was contradicted, and nothing was confirmed either.
	VerifyUnreplayable VerifyStatus = "unreplayable"
	// VerifyGenesis means the link has no parent to replay from.
	VerifyGenesis VerifyStatus = "genesis"
)

// LinkResult is one checkpoint's verdict.
type LinkResult struct {
	ID       string       `json:"id"`
	Parent   string       `json:"parent,omitempty"`
	Sequence uint64       `json:"sequence"`
	Status   VerifyStatus `json:"status"`

	// Recorded and Reproduced are the three output digests as written and as
	// re-derived. They are reported on divergence so the disagreement can be
	// read rather than taken on trust.
	Recorded   OutputDigests `json:"recorded"`
	Reproduced OutputDigests `json:"reproduced,omitempty"`
}

// OutputDigests are the three canonical outputs of one reduction.
type OutputDigests struct {
	Snapshot  string `json:"snapshot"`
	Diff      string `json:"diff"`
	Proposals string `json:"proposals"`
}

// VerifyResult is the whole lineage's verdict.
type VerifyResult struct {
	Links []LinkResult `json:"links"`

	Reproduced   int `json:"reproduced"`
	Diverged     int `json:"diverged"`
	Unreplayable int `json:"unreplayable"`

	// LineageOverflow reports that older links were evicted, so a lineage
	// that verifies completely may still be shorter than the run it describes.
	LineageOverflow bool `json:"lineage_overflow"`
}

// Sound reports whether nothing in the lineage contradicted its evidence.
//
// Unreplayable links do not make a lineage unsound: they make it unverified,
// which the counts say. Only a divergence is a contradiction.
func (result VerifyResult) Sound() bool { return result.Diverged == 0 }

var ErrVerify = errors.New("lineage verification failed")

// Verify replays every retained link against the journals that fed it.
//
// Both domains are required because the host order spans them: root and user
// keep separate journals and the acceptor assigns one sequence across both, so
// a link's consumed range is not held by either alone.
func Verify(
	store *Store,
	root *connectivityjournal.Journal,
	user *connectivityjournal.Journal,
	policyComponents []connectivityreduce.ComponentPolicy,
) (VerifyResult, error) {
	if store == nil || root == nil || user == nil {
		return VerifyResult{}, ErrVerify
	}
	index, err := store.Index()
	if err != nil {
		return VerifyResult{}, err
	}
	pointer, err := store.Pointer()
	if err != nil {
		return VerifyResult{}, err
	}
	result := VerifyResult{LineageOverflow: pointer.Overflow}

	byID := make(map[string]Checkpoint, len(index))
	for _, entry := range index {
		checkpoint, loadErr := store.Load(entry.ID)
		if loadErr != nil {
			// A record the index names and the store cannot load is exactly
			// the missing-ancestor condition; it is reported per link rather
			// than aborting the whole verification.
			result.Links = append(result.Links, LinkResult{
				ID: entry.ID, Parent: entry.Parent, Sequence: entry.Sequence,
				Status: VerifyUnreplayable,
			})
			result.Unreplayable++
			continue
		}
		byID[entry.ID] = checkpoint
	}

	for _, entry := range index {
		checkpoint, held := byID[entry.ID]
		if !held {
			continue
		}
		link := LinkResult{
			ID: checkpoint.ID, Parent: checkpoint.Parent, Sequence: entry.Sequence,
			Recorded: OutputDigests{
				Snapshot:  checkpoint.SnapshotDigest,
				Diff:      checkpoint.DiffDigest,
				Proposals: checkpoint.ProposalsDigest,
			},
		}
		parent, hasParent := byID[checkpoint.Parent]
		if checkpoint.Parent == "" || !hasParent {
			// Genesis has nothing to replay from. A named parent the store no
			// longer holds cannot be replayed from either, and saying so is
			// different from claiming the link disagreed.
			link.Status = VerifyGenesis
			if checkpoint.Parent != "" {
				link.Status = VerifyUnreplayable
				result.Unreplayable++
			}
			result.Links = append(result.Links, link)
			continue
		}
		reproduced, status, err := reproduce(
			checkpoint, parent, root, user, policyComponents)
		if err != nil {
			return VerifyResult{}, err
		}
		link.Status = status
		link.Reproduced = reproduced
		switch status {
		case VerifyReproduced:
			result.Reproduced++
		case VerifyDiverged:
			result.Diverged++
		case VerifyUnreplayable:
			result.Unreplayable++
		}
		result.Links = append(result.Links, link)
	}
	return result, nil
}

// merged reads both domains forward from a watermark and returns the host
// order they shared, bounded to what the link consumed.
//
// Continuity is the two journals' answer taken together: a hole in either is a
// hole in the host order, because the sequence numbers one stream that both
// contributed to.
func merged(
	root *connectivityjournal.Journal,
	user *connectivityjournal.Journal,
	from uint64,
	consumedTo uint64,
) ([]connectivityjournal.Record, bool, error) {
	rootRecords, rootContinuous, err := root.RecordsAfter(from)
	if err != nil {
		return nil, false, err
	}
	userRecords, userContinuous, err := user.RecordsAfter(from)
	if err != nil {
		return nil, false, err
	}
	all := append(append(
		make([]connectivityjournal.Record, 0, len(rootRecords)+len(userRecords)),
		rootRecords...), userRecords...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].HostSequence < all[j].HostSequence
	})
	kept := make([]connectivityjournal.Record, 0, len(all))
	for _, record := range all {
		if record.HostSequence <= from || record.HostSequence > consumedTo {
			continue
		}
		kept = append(kept, record)
	}
	// Continuity is judged on the merged order, not on either journal's own
	// answer. A domain that contributed nothing to a stretch sees a hole in
	// its own numbering where the host order has none, so its flag would
	// condemn every link that the other domain filled in.
	_, _ = rootContinuous, userContinuous
	if uint64(len(kept)) != consumedTo-from {
		// The retained facts do not cover the range. Folding what is left
		// would produce a different snapshot and call the shortfall a
		// divergence, which would turn retention into a contradiction.
		return kept, false, nil
	}
	for index, record := range kept {
		if record.HostSequence != from+uint64(index)+1 {
			return kept, false, nil
		}
	}
	return kept, true, nil
}

// reproduce replays one link and compares what came out.
func reproduce(
	checkpoint Checkpoint,
	parent Checkpoint,
	root *connectivityjournal.Journal,
	user *connectivityjournal.Journal,
	policyComponents []connectivityreduce.ComponentPolicy,
) (OutputDigests, VerifyStatus, error) {
	// Each journal reads forward from a watermark. The parent consumed up to
	// its own ConsumedTo, so that is where this link's facts begin in both.
	records, continuous, err := merged(root, user, parent.ConsumedTo, checkpoint.ConsumedTo)
	if err != nil || !continuous {
		return OutputDigests{}, VerifyUnreplayable, nil
	}
	replayed, err := Replay(ReplayInput{
		Resume:     Resume{Status: ResumeLatest, Checkpoint: &parent},
		Records:    records,
		Continuous: continuous,
		// The policy the checkpoint recorded, not a current one: this asks
		// whether the conclusion followed from its own inputs, and swapping
		// the policy would ask a different question.
		Policy:           checkpoint.Policy,
		PolicyComponents: policyComponents,
		BootID:           checkpoint.Snapshot.BootID,
		EvaluationTick:   checkpoint.Snapshot.EvaluationTick,
	})
	if err != nil {
		return OutputDigests{}, VerifyUnreplayable, nil
	}
	if replayed.Status != ReplayComplete {
		return OutputDigests{}, VerifyUnreplayable, nil
	}

	snapshot, err := replayed.Output.Snapshot.Digest()
	if err != nil {
		return OutputDigests{}, "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	diff, err := replayed.Output.Diff.Digest()
	if err != nil {
		return OutputDigests{}, "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	proposals, err := proposalsDigest(replayed.Output.Proposals)
	if err != nil {
		return OutputDigests{}, "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	outputs := OutputDigests{Snapshot: snapshot, Diff: diff, Proposals: proposals}
	if outputs.Snapshot != checkpoint.SnapshotDigest ||
		outputs.Diff != checkpoint.DiffDigest ||
		outputs.Proposals != checkpoint.ProposalsDigest {
		return outputs, VerifyDiverged, nil
	}
	return outputs, VerifyReproduced, nil
}
