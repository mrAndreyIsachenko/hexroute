package connectivitysoak

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityruntime"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// A session is one trace's isolated world: its own store, its own journals and
// its own read model, all under a scratch root nobody else is using.

type session struct {
	trace  connectivitytrace.Trace
	root   string
	nodeID metadata.UUID

	store       *connectivitycheckpoint.Store
	rootJournal *connectivityjournal.Journal
	userJournal *connectivityjournal.Journal
	runtime     *connectivityruntime.Runtime

	bootID string
	policy connectivityreduce.PolicyDescriptor

	injected Observation
}

const storeDirectory = "readmodel"

func newSession(trace connectivitytrace.Trace, root string) (*session, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrScratch)
	}
	entries, err := os.ReadDir(root)
	switch {
	case err == nil && len(entries) > 0:
		// A soak that inherited a lineage would be describing that lineage as
		// much as the fault it injected.
		return nil, fmt.Errorf("%w: %s is not empty", ErrScratch, root)
	case err != nil && !os.IsNotExist(err):
		return nil, fmt.Errorf("%w: %v", ErrScratch, err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrScratch, err)
	}
	nodeID, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrScratch, err)
	}
	current := &session{
		trace: trace, root: root, nodeID: nodeID,
		bootID: trace.BootID, policy: trace.Policy,
	}
	if err := current.open(trace.BootID); err != nil {
		return nil, err
	}
	return current, nil
}

// open builds a read model over the scratch store, resuming whatever is
// already there. It is called once to start and again after every restart a
// trace asks for, which is what makes a restart a restart rather than a
// pause.
func (current *session) open(bootID string) error {
	store, err := connectivitycheckpoint.Open(
		filepath.Join(current.root, storeDirectory), connectivitycheckpoint.Options{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScratch, err)
	}
	clock := metadata.NewSystemClock()
	rootJournal, err := connectivityjournal.Open(
		filepath.Join(current.root, "root"), policy.DomainRoot,
		connectivityjournal.Options{NodeID: current.nodeID, Clock: clock})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScratch, err)
	}
	userJournal, err := connectivityjournal.Open(
		filepath.Join(current.root, "user"), policy.DomainUser,
		connectivityjournal.Options{NodeID: current.nodeID, Clock: clock})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScratch, err)
	}
	runtime, err := connectivityruntime.New(connectivityruntime.Options{
		Enabled: true,
		BootID:  bootID,
		Random:  rand.Reader,
		Preconditions: connectivityruntime.Preconditions{
			AtomicPolicyStartup: true, DomainMismatch: true,
			Suspension: true, RedactedStatus: true,
		},
		Checkpoints: store,
		RootJournal: rootJournal,
		UserJournal: userJournal,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScratch, err)
	}
	current.store = store
	current.rootJournal = rootJournal
	current.userJournal = userJournal
	current.runtime = runtime
	current.bootID = bootID
	return nil
}

// phase is a group of steps that reduce together.
//
// Facts that share an evaluation tick are one reduction, because that is how
// the daemon publishes them: a collection cycle produces several facts and one
// tick. Anything that is not a fact — a wake, a restart, a policy change —
// stands alone, so what it did is not mixed into a batch.
type phase struct {
	tick    control.Tick
	facts   []connectivity.Fact
	counted []bool
	wake    bool
	restart bool
	bootID  string
	policy  *connectivityreduce.PolicyDescriptor
}

func phasesOf(steps []connectivitytrace.Step) []phase {
	phases := make([]phase, 0, len(steps))
	for _, step := range steps {
		interrupts := step.Wake || step.Restart || step.Policy != nil
		last := len(phases) - 1
		if !interrupts && step.Fact != nil && last >= 0 &&
			phases[last].tick == step.Tick && !phases[last].isControl() {
			phases[last].facts = append(phases[last].facts, *step.Fact)
			phases[last].counted = append(phases[last].counted, step.Injected)
			continue
		}
		next := phase{
			tick: step.Tick, wake: step.Wake,
			restart: step.Restart, bootID: step.BootID, policy: step.Policy,
		}
		if step.Fact != nil {
			next.facts = append(next.facts, *step.Fact)
			next.counted = append(next.counted, step.Injected)
		}
		phases = append(phases, next)
	}
	return phases
}

func (group phase) isControl() bool {
	return group.wake || group.restart || group.policy != nil
}

// play runs the trace to its end.
func (current *session) play() error {
	for _, group := range phasesOf(current.trace.Steps) {
		if group.restart {
			if err := current.open(group.bootID); err != nil {
				return err
			}
		}
		if group.policy != nil {
			current.policy = *group.policy
		}
		for index, fact := range group.facts {
			report, err := current.runtime.Publish([]connectivity.Fact{fact}, fact.Domain)
			if err != nil {
				return fmt.Errorf("%w: publish: %v", ErrInject, err)
			}
			if group.counted[index] {
				current.injected.Accepted += report.Accepted
				current.injected.Duplicates += report.Duplicates
				current.injected.Conflicts += report.Conflicts
				current.injected.Stale += report.Stale
				current.injected.Rejected += report.Rejected
			}
		}
		input := connectivityruntime.TickInput{
			Policy:           current.policy,
			PolicyComponents: current.trace.Components,
			EvaluationTick:   group.tick,
		}
		if group.wake {
			input.Wake = &connectivityreduce.Wake{Tick: group.tick}
		}
		if _, err := current.runtime.Tick(input); err != nil {
			return fmt.Errorf("%w: reduce at tick %d: %v", ErrInject, group.tick, err)
		}
	}
	return nil
}

// observe reads what the model now says, and what the store now holds.
func (current *session) observe() (Observation, error) {
	seen := current.injected
	seen.BootID = current.bootID
	snapshot := current.runtime.Snapshot()
	if snapshot == nil {
		return Observation{}, fmt.Errorf("%w: the trace produced no snapshot", ErrInject)
	}
	summary := snapshot.Summary
	seen.OpenGaps = summary.OpenGaps
	seen.AwaitingBaseline = summary.AwaitingBaseline
	seen.SourceConflicts = summary.SourceConflicts
	seen.StaleComponents = summary.Stale
	seen.Aggregate = summary.State
	seen.Authorization = summary.Authorization
	seen.AuthorizationReason = summary.Reason

	pointer, err := current.store.Pointer()
	if err != nil {
		return Observation{}, fmt.Errorf("%w: no lineage to bind to: %v", ErrInject, err)
	}
	checkpoint, err := current.store.Load(pointer.ID)
	if err != nil {
		return Observation{}, fmt.Errorf("%w: %v", ErrInject, err)
	}
	seen.CheckpointID = checkpoint.ID
	seen.SnapshotSHA256 = checkpoint.SnapshotDigest
	seen.DiffSHA256 = checkpoint.DiffDigest
	seen.ProposalsSHA256 = checkpoint.ProposalsDigest
	return seen, nil
}

// reopen asks a fresh store what it can prove about the damaged lineage.
//
// The runtime is built as well, and its error matters: a store that refuses a
// tampered lineage but leaves startup unable to come up at all has turned a
// recoverable fault into an outage.
func (current *session) reopen(bootID string) (connectivitycheckpoint.Resume, error) {
	store, err := connectivitycheckpoint.Open(
		filepath.Join(current.root, storeDirectory), connectivitycheckpoint.Options{})
	if err != nil {
		return connectivitycheckpoint.Resume{}, fmt.Errorf("%w: %v", ErrScratch, err)
	}
	resume, err := store.Resume()
	if err != nil {
		return connectivitycheckpoint.Resume{}, fmt.Errorf("%w: resume: %v", ErrInject, err)
	}
	if err := current.open(bootID); err != nil {
		return connectivitycheckpoint.Resume{}, err
	}
	return resume, nil
}
