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
	// VerifyBroken means the link has no parent because it abandoned one.
	//
	// It is told apart from genesis deliberately. Both have nothing to replay
	// from, but genesis says the store began here and this says the store
	// gave up here and started again — and reporting the second as the first
	// is exactly the silent restart the record exists to prevent.
	VerifyBroken VerifyStatus = "lineage_broken"
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

	// ExpectedFrom, ExpectedTo and Found say which host sequences the link
	// consumed and how many the journals still hold. A link reported
	// unreplayable without them names a condition and hides its cause, which
	// is the failure this whole verification exists to refuse elsewhere.
	ExpectedFrom uint64 `json:"expected_from"`
	ExpectedTo   uint64 `json:"expected_to"`
	Found        int    `json:"found"`
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

	// JournalError names why the evidence could not be read at all, when that
	// is what happened. Without it a journal that refuses to open reads as a
	// lineage where nothing could be replayed, which is the same words for a
	// different problem.
	JournalError string `json:"journal_error,omitempty"`
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

	// The journals are read once. Reading them per link would ask the same
	// question nine times and, when the answer is an error, would report it
	// nine times as a property of nine links instead of once as a property of
	// the evidence.
	facts, journalErr := allRecords(root, user)
	if journalErr != nil {
		result.JournalError = journalErr.Error()
	}

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
			switch {
			case checkpoint.Parent != "":
				link.Status = VerifyUnreplayable
				result.Unreplayable++
			case checkpoint.Break != nil:
				link.Status = VerifyBroken
			}
			result.Links = append(result.Links, link)
			continue
		}
		link.ExpectedFrom = parent.ConsumedTo
		link.ExpectedTo = checkpoint.ConsumedTo
		reproduced, found, status, err := reproduce(
			checkpoint, parent, facts, policyComponents)
		if err != nil {
			return VerifyResult{}, err
		}
		link.Status = status
		link.Reproduced = reproduced
		link.Found = found
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

// allRecords reads both domains once and returns the host order they shared.
//
// Both are required because the sequence numbers one stream that both
// contributed to: a domain that contributed nothing to a stretch sees a hole
// in its own numbering where the host order has none.
func allRecords(
	root *connectivityjournal.Journal,
	user *connectivityjournal.Journal,
) ([]connectivityjournal.Record, error) {
	rootRecords, err := root.Records()
	if err != nil {
		return nil, fmt.Errorf("root journal: %w", err)
	}
	userRecords, err := user.Records()
	if err != nil {
		return nil, fmt.Errorf("user journal: %w", err)
	}
	all := append(append(
		make([]connectivityjournal.Record, 0, len(rootRecords)+len(userRecords)),
		rootRecords...), userRecords...)
	sort.Slice(all, func(i, j int) bool {
		// Merged in the order the events were folded, which is the order the
		// reduction read them in. The accepted order has no place for the
		// duplicates, conflicts and late arrivals sitting between them.
		return all[i].FoldPosition < all[j].FoldPosition
	})
	return all, nil
}

// span returns the retained facts a link consumed, and whether they cover the
// range without a hole.
func span(
	facts []connectivityjournal.Record,
	from uint64,
	consumedTo uint64,
) ([]connectivityjournal.Record, bool) {
	kept := make([]connectivityjournal.Record, 0, consumedTo-from)
	for _, record := range facts {
		if record.FoldPosition <= from || record.FoldPosition > consumedTo {
			continue
		}
		kept = append(kept, record)
	}
	if uint64(len(kept)) != consumedTo-from {
		// The retained facts do not cover the range. Folding what is left
		// would produce a different snapshot and call the shortfall a
		// divergence, which would turn retention into a contradiction.
		return kept, false
	}
	for index, record := range kept {
		if record.FoldPosition != from+uint64(index)+1 {
			return kept, false
		}
	}
	return kept, true
}

// reproduce replays one link and compares what came out.
func reproduce(
	checkpoint Checkpoint,
	parent Checkpoint,
	facts []connectivityjournal.Record,
	policyComponents []connectivityreduce.ComponentPolicy,
) (OutputDigests, int, VerifyStatus, error) {
	records, continuous := span(facts, parent.ConsumedTo, checkpoint.ConsumedTo)
	if !continuous {
		return OutputDigests{}, len(records), VerifyUnreplayable, nil
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
		// The wake the reduction was told about. Without it a replay asks
		// what this checkpoint would have concluded had the host never
		// slept, and reports the honest difference as a contradiction.
		Wake: checkpoint.Wake,
	})
	if err != nil {
		return OutputDigests{}, len(records), VerifyUnreplayable, nil
	}
	if replayed.Status != ReplayComplete {
		return OutputDigests{}, len(records), VerifyUnreplayable, nil
	}

	snapshot, err := replayed.Output.Snapshot.Digest()
	if err != nil {
		return OutputDigests{}, len(records), "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	diff, err := replayed.Output.Diff.Digest()
	if err != nil {
		return OutputDigests{}, len(records), "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	proposals, err := proposalsDigest(replayed.Output.Proposals)
	if err != nil {
		return OutputDigests{}, len(records), "", fmt.Errorf("%w: %v", ErrVerify, err)
	}
	outputs := OutputDigests{Snapshot: snapshot, Diff: diff, Proposals: proposals}
	if outputs.Snapshot != checkpoint.SnapshotDigest ||
		outputs.Diff != checkpoint.DiffDigest ||
		outputs.Proposals != checkpoint.ProposalsDigest {
		return outputs, len(records), VerifyDiverged, nil
	}
	return outputs, len(records), VerifyReproduced, nil
}
