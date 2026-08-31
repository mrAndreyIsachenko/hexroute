package connectivitycheckpoint

import (
	"fmt"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

// ReplayStatus is what replay was able to do.
type ReplayStatus string

const (
	// ReplayComplete means the journal continued the checkpoint and the fold
	// succeeded.
	ReplayComplete ReplayStatus = "complete"
	// ReplayJournalGap means the retained facts do not continue the
	// checkpoint, so what happened cannot be reconstructed.
	ReplayJournalGap ReplayStatus = "journal_gap"
	// ReplayUnavailable means there was no checkpoint to continue from.
	ReplayUnavailable ReplayStatus = "unavailable"
)

// ReplayInput is everything a deterministic replay needs.
type ReplayInput struct {
	// Wake is the wake the original reduction was told about, when it was
	// told about one. A replay without it is replaying different inputs.
	Wake    *connectivityreduce.Wake
	Resume  Resume
	Records []connectivityjournal.Record
	// Continuous is the journal's own answer about whether the records
	// continue the checkpoint without a hole.
	Continuous bool

	Policy           connectivityreduce.PolicyDescriptor
	PolicyComponents []connectivityreduce.ComponentPolicy
	BootID           string
	EvaluationTick   control.Tick
}

// ReplayResult is the outcome of folding a journal onto a checkpoint.
type ReplayResult struct {
	Status ReplayStatus
	Output connectivityreduce.Output
}

// Replay folds retained facts onto a proven checkpoint under the current
// policy.
//
// It re-accepts the facts rather than trusting the order they were read in, so
// the gaps and boot transitions the original run saw are derived again from
// the same evidence instead of being taken on faith.
//
// It never consults or moves the policy active pointer. Recovery here selects
// an older read model; it never authorizes an older policy.
func Replay(input ReplayInput) (ReplayResult, error) {
	if !input.Resume.Usable() {
		return ReplayResult{Status: ReplayUnavailable}, nil
	}
	checkpoint := *input.Resume.Checkpoint
	if !input.Continuous {
		return ReplayResult{Status: ReplayJournalGap}, nil
	}

	acceptor, err := restoreAcceptor(checkpoint)
	if err != nil {
		return ReplayResult{}, err
	}
	events := make([]connectivityreduce.Event, 0, len(input.Records))
	expected := checkpoint.ConsumedTo
	for _, record := range input.Records {
		// Counted in folded positions, because that is what the range is
		// bounded in. Reading it as an accepted sequence drops every
		// duplicate, conflict and late arrival on the floor: they carry no
		// accepted sequence, so zero reads as already folded and the replay
		// quietly leaves out the events it most needs.
		if record.FoldPosition <= checkpoint.ConsumedTo {
			continue
		}
		expected++
		if record.FoldPosition != expected {
			return ReplayResult{Status: ReplayJournalGap}, nil
		}
		acceptance, acceptErr := acceptor.Accept(record.Fact, record.Fact.Domain)
		if acceptErr != nil {
			return ReplayResult{}, fmt.Errorf("%w: replaying %d: %v",
				ErrLineageBroken, record.HostSequence, acceptErr)
		}
		// Every outcome is folded, because every outcome was. What may not
		// differ is which outcome: a fact judged one way then and another way
		// now means the retained evidence disagrees with itself, which is a
		// finding rather than something to smooth over.
		if string(acceptance.Outcome) != record.Outcome {
			return ReplayResult{Status: ReplayJournalGap}, nil
		}
		if acceptance.HostSequence != record.HostSequence {
			return ReplayResult{Status: ReplayJournalGap}, nil
		}
		events = append(events, connectivityreduce.Event{
			Acceptance: acceptance, Fact: record.Fact,
		})
	}

	prior := checkpoint.Snapshot
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Prior:            &prior,
		Events:           events,
		Policy:           input.Policy,
		PolicyComponents: input.PolicyComponents,
		BootID:           input.BootID,
		EvaluationTick:   input.EvaluationTick,
		Wake:             input.Wake,
	})
	if err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{Status: ReplayComplete, Output: output}, nil
}

// restoreAcceptor rebuilds the acceptance position the checkpoint recorded.
//
// The retry-window digests are deliberately not restored: only accepted facts
// are journalled, so replay never sees a retry, and remembering digests that
// were never persisted would be inventing evidence.
func restoreAcceptor(checkpoint Checkpoint) (*connectivityaccept.Acceptor, error) {
	state := connectivityaccept.State{
		// The accepted order and the folded order move at different speeds,
		// and the range is counted in the second. Restoring one from the
		// other would hand the next accepted fact a sequence already used.
		HostSequence: checkpoint.Snapshot.ConsumedHostSequence,
		FoldPosition: checkpoint.Snapshot.ConsumedFoldPosition,
		Sources:      make(map[connectivity.SourceID]*connectivityaccept.SourceState),
	}
	for _, watermark := range checkpoint.SourceWatermarks {
		state.Sources[watermark.Source] = &connectivityaccept.SourceState{
			BootID: watermark.BootID,
			Recent: append([]connectivityaccept.RecentDigest(nil),
				watermark.Recent...),
			LastSequence: watermark.LastSequence,
			Gaps:         append([]connectivityaccept.GapRange(nil), watermark.Gaps...),
			GapOverflow:  watermark.GapOverflow,
			PendingBaseline: append(
				[]connectivity.Component(nil), watermark.PendingBaseline...),
			Conflicts: watermark.Conflicts,
		}
	}
	acceptor, err := connectivityaccept.Restore(state)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLineageBroken, err)
	}
	return acceptor, nil
}

// SealFrom builds the next checkpoint from a reduction and its parent.
// SealFrom builds the checkpoint one reduction produced.
//
// A parent and a break are mutually exclusive, and one of them is almost
// always nil: a record either continues a lineage it proved or starts one
// because it could not. Both nil is genesis, which is only correct for the
// first checkpoint a store ever holds.
func SealFrom(
	parent *Checkpoint,
	broken *LineageBreak,
	id string,
	output connectivityreduce.Output,
	consumedFrom uint64,
	wake *connectivityreduce.Wake,
) (Checkpoint, error) {
	snapshotDigest, err := output.Snapshot.Digest()
	if err != nil {
		return Checkpoint{}, err
	}
	diffDigest, err := output.Diff.Digest()
	if err != nil {
		return Checkpoint{}, err
	}
	proposalsDigest, err := proposalsDigest(output.Proposals)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{
		ID:           id,
		ConsumedFrom: consumedFrom,
		// Bounded in folded positions, because that is what a replay reads
		// back: an accepted-only range skips the events the reduction was
		// given and could not arrive where it did.
		ConsumedTo:         output.Snapshot.ConsumedFoldPosition,
		SourceWatermarks:   output.Snapshot.Sources,
		Policy:             output.Snapshot.Policy,
		ReducerID:          connectivityreduce.ReducerID,
		ReducerVersion:     connectivityreduce.ReducerVersion,
		SnapshotGeneration: output.Snapshot.Generation,
		SnapshotDigest:     snapshotDigest,
		DiffDigest:         diffDigest,
		ProposalsDigest:    proposalsDigest,
		Snapshot:           output.Snapshot,
	}
	if wake != nil {
		carried := *wake
		checkpoint.Wake = &carried
	}
	if wake != nil {
		carried := *wake
		checkpoint.Wake = &carried
	}
	switch {
	case parent != nil:
		checkpoint.Parent = parent.ID
		checkpoint.ParentDigest = parent.Digest
		checkpoint.PriorSnapshotDigest = parent.SnapshotDigest
	case broken != nil:
		copied := *broken
		checkpoint.Break = &copied
	}
	return Seal(checkpoint)
}
