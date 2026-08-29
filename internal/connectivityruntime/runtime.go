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

// Options configures the runtime.
type Options struct {
	// Enabled is the observe-only feature gate.
	Enabled     bool
	BootID      string
	Checkpoints *connectivitycheckpoint.Store
	RootJournal *connectivityjournal.Journal
	UserJournal *connectivityjournal.Journal
	Random      io.Reader
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
	Rejected   uint16
	Watermark  uint64
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
		return runtime, nil
	}
	// Nothing provable was stored. Starting from an empty read model is the
	// honest outcome: every component is unknown until an owner speaks, which
	// is exactly what the operator should see after an unrecoverable lineage.
	runtime.acceptor = connectivityaccept.New()
	return runtime, nil
}

func restoreAcceptor(checkpoint connectivitycheckpoint.Checkpoint) (*connectivityaccept.Acceptor, error) {
	state := connectivityaccept.State{
		HostSequence: checkpoint.ConsumedTo,
		Sources:      make(map[connectivity.SourceID]*connectivityaccept.SourceState),
	}
	for _, watermark := range checkpoint.SourceWatermarks {
		state.Sources[watermark.Source] = &connectivityaccept.SourceState{
			BootID:           watermark.BootID,
			LastSequence:     watermark.LastSequence,
			Gaps:             append([]connectivityaccept.GapRange(nil), watermark.Gaps...),
			GapOverflow:      watermark.GapOverflow,
			AwaitingBaseline: watermark.AwaitingBaseline,
		}
	}
	return connectivityaccept.Restore(state)
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
	checkpoint, err := connectivitycheckpoint.SealFrom(runtime.parent, string(id), output, from)
	if err != nil {
		return output, err
	}
	if err := runtime.store.Append(checkpoint); err != nil {
		return output, err
	}
	stored := checkpoint
	runtime.parent = &stored
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
