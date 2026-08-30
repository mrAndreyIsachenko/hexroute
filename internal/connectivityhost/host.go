// Package connectivityhost is the seam between what the root daemon observes
// and the read model that describes it.
//
// It exists as its own package for one reason. The daemon holds route plans,
// and a route plan is how a host gets changed; the read model holds proposals,
// and a proposal must never be executable. Keeping both in one package would
// put a proposal and the means to act on it within reach of each other, which
// an architectural test in connectivityreduce refuses — correctly. So the
// proposal stops here: the daemon receives an aggregate, not something it
// could act on.
package connectivityhost

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycollect"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityruntime"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityview"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// The read model observes; it never acts. Everything below turns observations
// the cycle already made into facts and folds them into a snapshot. No probe,
// no command, no route change, and no path by which a proposal could be
// executed — the runtime it drives cannot import anything that mutates.
//
// The gate is off unless a root is configured. A daemon started without one
// behaves exactly as it did before this file existed.

// Evidence is one observation cycle's raw readings, exactly as they were made.
//
// Nothing here is derived. The daemon's own conclusions stay with the daemon;
// this is what it saw before drawing them, so the read model reaches its own
// conclusions from the same evidence rather than from a second look at the
// host.
type Evidence struct {
	// Reached reports that the cycle got far enough to observe anything. A
	// suspended or failed cycle leaves the rest zero, and that absence is the
	// honest answer rather than a stale repeat.
	Reached bool

	Physical      observe.PhysicalNetwork
	PhysicalError error

	Process      observe.ProcessObservation
	ProcessError error

	TUNs       []observe.TUNInterface
	ManagedTUN observe.TUNInterface
	TUNError   error

	Routes           []observe.RouteObservation
	RouteError       error
	ConfiguredRoutes uint16

	Readiness      []observe.ReadinessObservation
	ReadinessError error
}

// ErrStore reports that the read model could not be opened.
var ErrStore = errors.New("connectivity read model store unavailable")

// readModelClock is the read model's view of time.
//
// It does not use the daemon's heartbeat-relative tick. That one starts at
// zero on a fresh heartbeat, and a reduction refuses a zero tick because zero
// means "no time context" — the two contracts do not meet. It also stops while
// the host sleeps, which is the opposite of what a freshness deadline needs:
// a component that went quiet across six hours of sleep must be stale, not
// preserved by a clock that slept with it.
//
// So the tick comes from the continuous clock, which counts through sleep and
// is nonzero from the first read.
type readModelClock struct{}

func (readModelClock) Wall() time.Time { return time.Now().UTC() }

func (readModelClock) Tick() control.Tick {
	tick, err := continuousTick()
	if err != nil {
		return 0
	}
	return tick
}

// continuousTick is the monotonic second the read model measures freshness in.
func continuousTick() (control.Tick, error) {
	elapsed, err := policyclock.ContinuousNow()
	if err != nil {
		return 0, err
	}
	// Seconds since boot, floored at one: a tick of zero is how a reduction
	// says it has no time context, so it may never be a real instant.
	tick := control.Tick(elapsed / time.Second)
	if tick < 1 {
		tick = 1
	}
	return tick, nil
}

// Reader owns the host's read model for the observe loop.
type Reader struct {
	runtime  *connectivityruntime.Runtime
	sources  map[connectivity.SourceID]*connectivitycollect.Collector
	owners   map[connectivity.Component]connectivity.SourceID
	baseline bool
}

// Open builds the read model under the given root.
//
// The preconditions are passed as claims rather than probed, matching the
// runtime's own contract: enabling this is a decision someone recorded, not
// something that happened because a check passed at startup.
func Open(
	root string,
	bootID string,
) (*Reader, error) {
	if root == "" {
		return nil, nil
	}
	if _, err := continuousTick(); err != nil {
		return nil, fmt.Errorf("%w: no continuous clock on this platform",
			ErrStore)
	}
	// A boot this daemon cannot name is a boot whose freshness deadlines it
	// cannot compare, so it refuses rather than inventing one.
	if bootID == "" {
		return nil, fmt.Errorf("%w: the boot session has no identity",
			ErrStore)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: root: %v", ErrStore, err)
	}
	nodeID, err := nodeIdentity(root)
	if err != nil {
		return nil, err
	}
	journalClock := metadata.NewSystemClock()
	collectorClock := readModelClock{}
	checkpoints, err := connectivitycheckpoint.Open(
		filepath.Join(root, "readmodel"), connectivitycheckpoint.Options{})
	if err != nil {
		return nil, fmt.Errorf("%w: checkpoints: %v", ErrStore, err)
	}
	rootJournal, err := connectivityjournal.Open(
		filepath.Join(root, "root"), policy.DomainRoot,
		connectivityjournal.Options{NodeID: nodeID, Clock: journalClock})
	if err != nil {
		return nil, fmt.Errorf("%w: root journal: %v", ErrStore, err)
	}
	userJournal, err := connectivityjournal.Open(
		filepath.Join(root, "user"), policy.DomainUser,
		connectivityjournal.Options{NodeID: nodeID, Clock: journalClock})
	if err != nil {
		return nil, fmt.Errorf("%w: user journal: %v", ErrStore, err)
	}

	runtime, err := connectivityruntime.New(connectivityruntime.Options{
		Enabled: true,
		BootID:  bootID,
		Random:  rand.Reader,
		Preconditions: connectivityruntime.Preconditions{
			AtomicPolicyStartup: true,
			DomainMismatch:      true,
			Suspension:          true,
			RedactedStatus:      true,
		},
		Checkpoints: checkpoints,
		RootJournal: rootJournal,
		UserJournal: userJournal,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStore, err)
	}

	reader := &Reader{
		runtime: runtime,
		sources: make(map[connectivity.SourceID]*connectivitycollect.Collector),
		owners:  make(map[connectivity.Component]connectivity.SourceID),
	}
	// One collector per source, not per component: a source sequence numbers
	// the source, and root.network speaks for both physical network and
	// default path. Two collectors there would mint two streams under one
	// identity and every second fact would look like a reused sequence.
	// A restored lineage already knows how far each source got. Collectors
	// resume from there so a restart continues the stream instead of starting
	// one that is entirely behind the watermark.
	resumed := make(map[connectivity.SourceID]uint64)
	if snapshot := runtime.Snapshot(); snapshot != nil {
		for _, watermark := range snapshot.Sources {
			resumed[watermark.Source] = watermark.LastSequence
		}
	}
	for _, component := range rootComponents() {
		declaration, owned := safety.ConnectivityAuthority(component)
		if !owned || declaration.Domain != policy.DomainRoot {
			return nil, fmt.Errorf("%w: %s is not root-owned",
				ErrStore, component)
		}
		reader.owners[component] = declaration.Source
		if _, built := reader.sources[declaration.Source]; built {
			continue
		}
		collector, collectErr := connectivitycollect.New(connectivitycollect.Options{
			Source:   declaration.Source,
			Domain:   policy.DomainRoot,
			BootID:   bootID,
			Clock:    collectorClock,
			Random:   rand.Reader,
			Sequence: resumed[declaration.Source],
		})
		if collectErr != nil {
			return nil, fmt.Errorf("%w: collector %s: %v",
				ErrStore, declaration.Source, collectErr)
		}
		reader.sources[declaration.Source] = collector
	}
	return reader, nil
}

// nodeIdentity returns this store's node identity, minting one on first use.
//
// It lives beside the read model rather than in configuration because it
// identifies the store's lineage, not the host's role: a store restored onto a
// different machine keeps the identity its records were written under.
func nodeIdentity(root string) (metadata.UUID, error) {
	path := filepath.Join(root, "node-id")
	stored, err := os.ReadFile(path)
	if err == nil {
		parsed, parseErr := metadata.ParseUUID(strings.TrimSpace(string(stored)))
		if parseErr != nil {
			return "", fmt.Errorf("%w: stored node identity is invalid",
				ErrStore)
		}
		return parsed, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: node identity: %v", ErrStore, err)
	}
	minted, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("%w: node identity: %v", ErrStore, err)
	}
	if err := os.WriteFile(path, []byte(minted+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("%w: node identity: %v", ErrStore, err)
	}
	return minted, nil
}

// rootComponents is what this daemon observes. DNS is absent on purpose: there
// is no DNS observer, so the component keeps its declared owner and reports
// unknown rather than being invented here.
func rootComponents() []connectivity.Component {
	return []connectivity.Component{
		connectivity.ComponentPhysicalNetwork,
		connectivity.ComponentDefaultPath,
		connectivity.ComponentScopedRoutes,
		connectivity.ComponentTransports,
		connectivity.ComponentRelays,
	}
}

// Observe folds one cycle's evidence into the read model and returns the
// operator view of the result.
//
// A cycle that never reached its observations publishes nothing. Saying
// nothing lets the components pass their freshness deadlines and go stale on
// their own evidence, which is true; repeating the last answer would not be.
func (reader *Reader) Observe(
	evidence Evidence,
	descriptor connectivityreduce.PolicyDescriptor,
	at control.Tick,
) (connectivityview.LocalStatus, error) {
	if reader == nil || reader.runtime == nil {
		return connectivityview.LocalStatus{}, nil
	}
	if evidence.Reached {
		facts, err := reader.facts(evidence)
		if err != nil {
			return connectivityview.LocalStatus{}, err
		}
		if _, err := reader.runtime.Publish(facts, policy.DomainRoot); err != nil {
			return connectivityview.LocalStatus{}, err
		}
		reader.baseline = true
	}
	output, err := reader.runtime.Tick(connectivityruntime.TickInput{
		Policy:         descriptor,
		EvaluationTick: at,
	})
	if err != nil {
		return connectivityview.LocalStatus{}, err
	}
	return connectivityview.Local(output.Snapshot, output.Diff, output.Proposals), nil
}

// facts maps one cycle's evidence onto the components this daemon owns.
func (reader *Reader) facts(
	observed Evidence,
) ([]connectivity.Fact, error) {
	observations := map[connectivity.Component]connectivitycollect.Observation{
		connectivity.ComponentPhysicalNetwork: connectivitycollect.MapPhysicalNetwork(
			observed.Physical, observed.PhysicalError),
		connectivity.ComponentDefaultPath: connectivitycollect.MapDefaultPath(
			observed.Physical, observed.TUNs, observed.TUNError),
		connectivity.ComponentScopedRoutes: connectivitycollect.MapScopedRoutes(
			observed.ConfiguredRoutes, observed.Routes,
			observed.ManagedTUN.Name, observed.RouteError),
		connectivity.ComponentTransports: connectivitycollect.MapTransports(
			1, observed.Process, observed.ProcessError),
		connectivity.ComponentRelays: connectivitycollect.MapRelays(
			observed.Readiness, 0, connectivity.SelectedPrimary),
	}

	facts := make([]connectivity.Fact, 0, len(observations))
	for _, component := range rootComponents() {
		observation := observations[component]
		// The first publication of a boot restates every component in full,
		// because a partial first answer would be mistaken for a whole one.
		observation.Baseline = !reader.baseline
		fact, err := reader.sources[reader.owners[component]].Emit(observation)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// Fold folds one cycle's evidence into the read model and reports its
// aggregate.
//
// A read model that cannot describe the host is a read model that failed, not
// a host that failed: the cycle's own conclusions are already emitted, and
// nothing here may change them. So the error is reported as a bounded
// diagnostic and the loop continues.
func Fold(
	reader *Reader,
	evidence Evidence,
	logger *logging.Logger,
) error {
	if reader == nil {
		return nil
	}
	// Evaluated on the same clock the facts were stamped with. Measuring a
	// deadline against a different clock than the one that set it is how a
	// stale component would look fresh.
	at, err := continuousTick()
	if err != nil {
		return logger.Emit(logging.LevelWarn, logging.EventConnectivitySnapshot,
			logging.ResultDegraded, "")
	}
	// The log line is coarse on purpose. The aggregate is a bounded state, but
	// the reason vocabulary is a closed allowlist and widening it to carry a
	// read-model conclusion would make it a second status surface. The
	// operator view is where component detail lives.
	status, observeErr := reader.Observe(evidence, unauthorizedPolicy(), at)
	if observeErr != nil {
		return logger.Emit(
			logging.LevelWarn,
			logging.EventConnectivitySnapshot,
			logging.ResultDegraded,
			"",
		)
	}
	result := logging.ResultOK
	if status.Aggregate != connectivityreduce.AggregateReady {
		result = logging.ResultDegraded
	}
	return logger.Emit(
		logging.LevelInfo,
		logging.EventConnectivitySnapshot,
		result,
		"",
	)
}

// unauthorizedPolicy is the descriptor the read model runs under until the
// daemon revalidates an active policy generation for it.
//
// Absent policy does not withhold observations — it withholds the authority to
// say what should be. Components are still observed, classified and shown; the
// desired state and every proposal are marked unauthorized, which is the
// honest position for a daemon that has not yet been given one.
func unauthorizedPolicy() connectivityreduce.PolicyDescriptor {
	return connectivityreduce.PolicyDescriptor{}
}

// PublishUser folds facts the user domain observed into the read model.
//
// The facts arrive as opaque bytes and are decoded here, under the same strict
// codec the user daemon encoded them with. Root does not trust the sender's
// account of what they mean: ownership, domain and order are all decided by
// the acceptor against the compiled envelope, exactly as they are for facts
// root observed itself.
//
// Nothing flows back. The report is counts and a watermark — enough for the
// publisher to know it was heard, and nothing it could act on.
func (reader *Reader) PublishUser(encoded []json.RawMessage) (Report, error) {
	if reader == nil || reader.runtime == nil {
		return Report{}, ErrStore
	}
	if len(encoded) == 0 || len(encoded) > ipc.MaxPublishedFacts {
		return Report{}, fmt.Errorf("%w: publication size", ErrStore)
	}
	facts := make([]connectivity.Fact, 0, len(encoded))
	for _, raw := range encoded {
		fact, err := connectivity.Decode(raw)
		if err != nil {
			return Report{}, fmt.Errorf("%w: %v", ErrStore, err)
		}
		facts = append(facts, fact)
	}
	report, err := reader.runtime.Publish(facts, policy.DomainUser)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Accepted:   report.Accepted,
		Duplicates: report.Duplicates,
		Conflicts:  report.Conflicts,
		Rejected:   report.Rejected,
		Watermark:  report.Watermark,
	}, nil
}

// Report is what one publication turned into.
type Report struct {
	Accepted   uint16
	Duplicates uint16
	Conflicts  uint16
	Rejected   uint16
	Watermark  uint64
}
