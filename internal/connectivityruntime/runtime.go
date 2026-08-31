// Package connectivityruntime joins acceptance, reduction, journalling and
// checkpointing into the observe-only read model the root daemon runs.
//
// It is a feature gate first and a runtime second: when the gate is off it
// holds no state and does nothing, so the path that exists today keeps
// existing exactly as it did. When it is on it still cannot change anything —
// it accepts facts, folds them and writes down what it saw.
package connectivityruntime

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityaccept"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

var (
	ErrDisabled      = errors.New("connectivity read model is disabled")
	ErrMisconfigured = errors.New("connectivity runtime is misconfigured")
	ErrUnknownDomain = errors.New("connectivity domain has no journal")
	ErrPrecondition  = errors.New("connectivity read model precondition is not met")
)

// UserLinkState is what the root aggregate knows about the user daemon.
//
// It exists so that "the user domain is quiet" is a recorded observation about
// the link rather than an inference about the components behind it. Root never
// answers on the user's behalf.
type UserLinkState string

const (
	UserLinkUnknown   UserLinkState = "unknown"
	UserLinkConnected UserLinkState = "connected"
	UserLinkAbsent    UserLinkState = "absent"
)

// Preconditions are the policy-foundation contracts the read model depends on.
//
// They are passed in as explicit claims rather than probed, so enabling the
// read model is a decision someone made and recorded, not something that
// happens because a check happened to pass at startup. Every field must be
// true; a false one names exactly which contract is missing.
type Preconditions struct {
	// AtomicPolicyStartup: startup revalidates the active policy generation
	// before anything reads it.
	AtomicPolicyStartup bool
	// DomainMismatch: a domain mismatch is refused rather than resolved.
	DomainMismatch bool
	// Suspension: an authorization suspension is honoured by every consumer.
	Suspension bool
	// RedactedStatus: local status output is already bounded and redacted.
	RedactedStatus bool
}

// Missing names the first unmet precondition, or an empty string.
func (preconditions Preconditions) Missing() string {
	switch {
	case !preconditions.AtomicPolicyStartup:
		return "atomic policy startup revalidation"
	case !preconditions.DomainMismatch:
		return "domain mismatch handling"
	case !preconditions.Suspension:
		return "authorization suspension handling"
	case !preconditions.RedactedStatus:
		return "redacted local status"
	default:
		return ""
	}
}

// Options configures the runtime.
type Options struct {
	// Enabled is the observe-only feature gate.
	Enabled bool
	// Preconditions must all hold before the gate may be opened.
	Preconditions Preconditions
	BootID        string
	Checkpoints   *connectivitycheckpoint.Store
	RootJournal   *connectivityjournal.Journal
	UserJournal   *connectivityjournal.Journal
	Random        io.Reader
}

// Runtime owns the host's read model.
type Runtime struct {
	mu       sync.Mutex
	enabled  bool
	bootID   string
	random   io.Reader
	store    *connectivitycheckpoint.Store
	journals map[policy.Domain]*connectivityjournal.Journal

	acceptor *connectivityaccept.Acceptor
	snapshot *connectivityreduce.Snapshot
	parent   *connectivitycheckpoint.Checkpoint
	// broken is set when startup could not prove the stored lineage and a
	// lineage already exists. The next checkpoint carries it, which is what
	// lets the store accept a restart instead of refusing every write from
	// here on.
	broken   *connectivitycheckpoint.LineageBreak
	pending  []connectivityreduce.Event
	consumed uint64

	resume   connectivitycheckpoint.Resume
	userLink UserLinkState
}

// Report is what one publication turned into.
type Report struct {
	Accepted   uint16
	Duplicates uint16
	Conflicts  uint16
	// Stale counts arrivals that were behind the accepted order. They are
	// kept apart from Rejected because they are a different event: a rejected
	// fact never entered the order and is not reduced, while a stale one
	// arrived late and is still folded as evidence. Counting them together
	// would leave a caller unable to tell a malformed publisher from a slow
	// one.
	Stale     uint16
	Rejected  uint16
	Watermark uint64
}

// New prepares the runtime, resuming from the stored lineage when the gate is
// on. A disabled runtime touches no store and holds no state.
func New(options Options) (*Runtime, error) {
	runtime := &Runtime{
		enabled:  options.Enabled,
		bootID:   options.BootID,
		random:   options.Random,
		store:    options.Checkpoints,
		journals: make(map[policy.Domain]*connectivityjournal.Journal, 2),
		userLink: UserLinkUnknown,
	}
	if !options.Enabled {
		return runtime, nil
	}
	if missing := options.Preconditions.Missing(); missing != "" {
		return nil, fmt.Errorf("%w: %s is not established", ErrPrecondition, missing)
	}
	if options.BootID == "" || options.Checkpoints == nil ||
		options.RootJournal == nil || options.Random == nil {
		return nil, fmt.Errorf("%w: boot identity, checkpoints, root journal and randomness are required",
			ErrMisconfigured)
	}
	runtime.journals[policy.DomainRoot] = options.RootJournal
	if options.UserJournal != nil {
		runtime.journals[policy.DomainUser] = options.UserJournal
	}

	resume, err := options.Checkpoints.Resume()
	if err != nil {
		return nil, err
	}
	runtime.resume = resume
	if resume.Usable() {
		checkpoint := *resume.Checkpoint
		snapshot := checkpoint.Snapshot
		runtime.snapshot = &snapshot
		runtime.parent = &checkpoint
		runtime.consumed = checkpoint.ConsumedTo
		acceptor, restoreErr := restoreAcceptor(checkpoint)
		if restoreErr != nil {
			return nil, restoreErr
		}
		runtime.acceptor = acceptor
		if err := runtime.resumeUnfolded(checkpoint); err != nil {
			return nil, err
		}
		return runtime, nil
	}
	// Nothing provable was stored. Starting from an empty read model is the
	// honest outcome: every component is unknown until an owner speaks, which
	// is exactly what the operator should see after an unrecoverable lineage.
	//
	// The acceptor may not start from zero, though. A host sequence orders
	// every fact this host ever accepted and the journals still hold the ones
	// it issued, so handing those numbers out again would mint a second fact
	// for a position already taken. Nothing notices immediately: the facts are
	// written, the model folds them, and the daemon looks well. It is the next
	// restart that fails, when replay reaches the journal and finds one
	// sequence with two meanings — and it fails at startup, so the daemon
	// never comes up again.
	//
	// So the count continues above everything already written. The lineage is
	// gone; the order it numbered is not.
	issued, err := highestIssued(runtime.journals)
	if err != nil {
		return nil, err
	}
	acceptor, err := connectivityaccept.Restore(connectivityaccept.State{
		HostSequence: issued,
		Sources:      make(map[connectivity.SourceID]*connectivityaccept.SourceState),
	})
	if err != nil {
		return nil, err
	}
	runtime.acceptor = acceptor
	runtime.consumed = issued
	// If a lineage is nonetheless on disk, the next checkpoint has to say it
	// is abandoning it. Without that the store refuses a parentless record
	// against an existing pointer — correctly, since a silent restart would
	// read ever after as though the lineage had always started there — and
	// the read model would be left unable to store anything at all.
	pointer, err := options.Checkpoints.Pointer()
	switch {
	case errors.Is(err, connectivitycheckpoint.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		reason := resume.Reason
		if reason == connectivitycheckpoint.ResumeReasonNone {
			reason = connectivitycheckpoint.ResumeReasonRecordInvalid
		}
		runtime.broken = &connectivitycheckpoint.LineageBreak{
			AfterID: pointer.ID, Reason: reason,
		}
	}
	return runtime, nil
}

// highestIssued is the last host sequence the journals show as handed out.
//
// It reads both domains: one host sequence orders the facts of both, so a
// watermark taken from one alone would leave the other's positions reusable.
func highestIssued(
	journals map[policy.Domain]*connectivityjournal.Journal,
) (uint64, error) {
	highest := uint64(0)
	for _, journal := range journals {
		records, err := journal.Records()
		if err != nil {
			return 0, err
		}
		for _, record := range records {
			if record.HostSequence > highest {
				highest = record.HostSequence
			}
		}
	}
	return highest, nil
}

func restoreAcceptor(checkpoint connectivitycheckpoint.Checkpoint) (*connectivityaccept.Acceptor, error) {
	state := connectivityaccept.State{
		HostSequence: checkpoint.ConsumedTo,
		Sources:      make(map[connectivity.SourceID]*connectivityaccept.SourceState),
	}
	for _, watermark := range checkpoint.SourceWatermarks {
		state.Sources[watermark.Source] = &connectivityaccept.SourceState{
			BootID:       watermark.BootID,
			LastSequence: watermark.LastSequence,
			Gaps:         append([]connectivityaccept.GapRange(nil), watermark.Gaps...),
			GapOverflow:  watermark.GapOverflow,
			PendingBaseline: append(
				[]connectivity.Component(nil), watermark.PendingBaseline...),
			Conflicts: watermark.Conflicts,
		}
	}
	return connectivityaccept.Restore(state)
}

// resumeUnfolded re-accepts the facts the journals hold beyond the checkpoint.
//
// A checkpoint is written only when a reduction was effective, so facts can be
// accepted and journalled and still lie beyond the newest one. Restoring the
// acceptor from the checkpoint alone leaves it about to hand those sequences
// out a second time, and the journal then holds two different facts under one
// number — which makes it unreadable in full, permanently, and takes the
// evidence chain with it.
//
// They are re-accepted rather than trusted. The acceptor decides ownership,
// order and duplication again from the same records, which is the same
// discipline replay uses and the reason a restored run reaches the state the
// original one did instead of a state it was told about.
func (runtime *Runtime) resumeUnfolded(
	checkpoint connectivitycheckpoint.Checkpoint,
) error {
	records := make([]connectivityjournal.Record, 0)
	for _, journal := range runtime.journals {
		held, _, err := journal.RecordsAfter(checkpoint.ConsumedTo)
		if err != nil {
			return err
		}
		records = append(records, held...)
	}
	if len(records) == 0 {
		return nil
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].HostSequence < records[j].HostSequence
	})
	for _, record := range records {
		acceptance, err := runtime.acceptor.Accept(record.Fact, record.Fact.Domain)
		if err != nil {
			// A retained record the acceptor now refuses cannot be folded and
			// must not be skipped past: the sequences after it would then be
			// handed out again. Refusing to start is the honest answer, and
			// the operator sees an unrecoverable lineage rather than a model
			// quietly built on part of its evidence.
			return fmt.Errorf("%w: retained record %d: %v",
				ErrMisconfigured, record.HostSequence, err)
		}
		if acceptance.Outcome == connectivityaccept.OutcomeRejected {
			return fmt.Errorf("%w: retained record %d was refused",
				ErrMisconfigured, record.HostSequence)
		}
		runtime.pending = append(runtime.pending,
			connectivityreduce.Event{Acceptance: acceptance, Fact: record.Fact})
	}
	return nil
}

// Enabled reports whether the read model is running.
func (runtime *Runtime) Enabled() bool { return runtime.enabled }

// Resume reports what startup concluded about the stored lineage.
func (runtime *Runtime) Resume() connectivitycheckpoint.Resume {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.resume
}

// SetUserLink records what is known about the user daemon connection.
//
// Recording that the link is absent is all root does about it. It does not
// reconnect, impersonate the user domain or read anything the user owns: the
// components behind the link simply pass their freshness deadlines and become
// stale on their own evidence.
func (runtime *Runtime) SetUserLink(state UserLinkState) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.userLink = state
}

// UserLink reports the recorded link state.
func (runtime *Runtime) UserLink() UserLinkState {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.userLink
}

// Publish offers facts from one authenticated domain.
func (runtime *Runtime) Publish(
	facts []connectivity.Fact,
	authenticatedDomain policy.Domain,
) (Report, error) {
	if !runtime.enabled {
		return Report{}, ErrDisabled
	}
	journal, known := runtime.journals[authenticatedDomain]
	if !known {
		return Report{}, fmt.Errorf("%w: %q", ErrUnknownDomain, authenticatedDomain)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	report := Report{}
	for _, fact := range facts {
		acceptance, err := runtime.acceptor.Accept(fact, authenticatedDomain)
		if err != nil {
			report.Rejected++
			continue
		}
		switch acceptance.Outcome {
		case connectivityaccept.OutcomeAccepted:
			// The journal is written before the event is queued for
			// reduction, so a crash cannot leave the read model ahead of the
			// evidence it was built from.
			if err := journal.Append(fact, acceptance.HostSequence, acceptance.Role); err != nil {
				return report, err
			}
			report.Accepted++
		case connectivityaccept.OutcomeDuplicate:
			report.Duplicates++
		case connectivityaccept.OutcomeConflict:
			report.Conflicts++
		case connectivityaccept.OutcomeStale:
			report.Stale++
		default:
			report.Rejected++
		}
		if acceptance.Outcome != connectivityaccept.OutcomeRejected {
			runtime.pending = append(runtime.pending,
				connectivityreduce.Event{Acceptance: acceptance, Fact: fact})
		}
	}
	report.Watermark = runtime.acceptor.State().HostSequence
	return report, nil
}

// TickInput is the time and policy context for one reduction.
type TickInput struct {
	Policy           connectivityreduce.PolicyDescriptor
	PolicyComponents []connectivityreduce.ComponentPolicy
	EvaluationTick   control.Tick
	Wake             *connectivityreduce.Wake
}

// Tick folds everything published since the last reduction.
//
// A reduction that changes nothing is not checkpointed: the lineage records
// effective change, not the passage of time.
func (runtime *Runtime) Tick(input TickInput) (connectivityreduce.Output, error) {
	if !runtime.enabled {
		return connectivityreduce.Output{}, ErrDisabled
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	before := runtime.consumed
	output, err := connectivityreduce.Reduce(connectivityreduce.Input{
		Prior:            runtime.snapshot,
		PriorConsumed:    runtime.consumed,
		Events:           runtime.pending,
		Policy:           input.Policy,
		PolicyComponents: input.PolicyComponents,
		BootID:           runtime.bootID,
		EvaluationTick:   input.EvaluationTick,
		Wake:             input.Wake,
	})
	if err != nil {
		return connectivityreduce.Output{}, err
	}
	runtime.pending = nil
	runtime.snapshot = &output.Snapshot
	runtime.consumed = output.Snapshot.ConsumedHostSequence

	if !output.Changed {
		return output, nil
	}
	id, err := metadata.NewUUID(runtime.random)
	if err != nil {
		return output, fmt.Errorf("%w: %v", ErrMisconfigured, err)
	}
	// A checkpoint that folded no facts records an absent range rather than
	// claiming one it did not consume.
	from := uint64(0)
	if output.Snapshot.ConsumedHostSequence > before {
		from = before + 1
	}
	checkpoint, err := connectivitycheckpoint.SealFrom(
		runtime.parent, runtime.broken, string(id), output, from, input.Wake)
	if err != nil {
		return output, err
	}
	if err := runtime.store.Append(checkpoint); err != nil {
		return output, err
	}
	stored := checkpoint
	runtime.parent = &stored
	// The break belongs to the one checkpoint that made it. Everything after
	// descends from that record and continues the lineage it started.
	runtime.broken = nil
	return output, nil
}

// Snapshot returns the current read model, or nil before the first reduction.
func (runtime *Runtime) Snapshot() *connectivityreduce.Snapshot {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.snapshot == nil {
		return nil
	}
	copied := *runtime.snapshot
	return &copied
}
