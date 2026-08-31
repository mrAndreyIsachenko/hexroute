package userdaemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycollect"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

// The user domain publishes what it saw and asks for nothing.
//
// This file builds facts and hands them to root. It cannot reduce, cannot hold
// a proposal and cannot receive one: root answers with counts and a watermark.
// That asymmetry is the design — root owns the aggregate because it owns the
// network observations, and the user domain owns what only it can see.

// factPublisher turns user observations into facts and sends them to root.
type factPublisher struct {
	sources   map[connectivity.Component]*connectivitycollect.Collector
	bootID    string
	socket    string
	roundTrip func(context.Context, string, ipc.Request) (ipc.Response, error)
	baseline  bool

	// streamPath is where this daemon remembers how far its own sources got.
	//
	// Root decides the order and root holds the lineage, but a source
	// sequence numbers the source, and only the source can continue it. A
	// restart that began again at zero would publish facts that are all
	// behind the accepted watermark, and root would refuse every one of them
	// while the daemon reported success — so the two components this domain
	// speaks for would keep whatever they last said, for ever.
	streamPath string

	// The clock pair is how a sleep is noticed. One counts through it and the
	// other stops for it, so the difference between what they advanced by is
	// the sleep. Root detects the same thing for its own components; nothing
	// tells this daemon, and nothing was.
	clocks     func() (time.Duration, time.Duration, error)
	continuous time.Duration
	awake      time.Duration
}

// sleepFloor is the smallest difference between the two clocks that is a sleep
// rather than the microseconds between reading one and reading the other.
const sleepFloor = 60 * time.Second

// streamState is the sequence each source had reached.
type streamState struct {
	Schema    string            `json:"schema"`
	BootID    string            `json:"boot_id"`
	Sequences map[string]uint64 `json:"sequences"`
}

const streamSchema = "hexroute.user-connectivity-stream.v1"

// userComponents is what this daemon speaks about.
func userComponents() []connectivity.Component {
	return []connectivity.Component{
		connectivity.ComponentUserAccess,
		connectivity.ComponentSessionExpiry,
	}
}

// publisherClock measures freshness on the continuous clock, the same one the
// root aggregate evaluates deadlines against. A deadline set by one clock and
// judged by another is how a stale component would look fresh.
type publisherClock struct{}

func (publisherClock) Wall() time.Time { return time.Now().UTC() }

func (publisherClock) Tick() control.Tick {
	elapsed, err := policyclock.ContinuousNow()
	if err != nil {
		return 1
	}
	tick := control.Tick(elapsed / time.Second)
	if tick < 1 {
		tick = 1
	}
	return tick
}

// newFactPublisher builds one collector per user-owned source.
func newFactPublisher(bootID, socket, streamPath string) (*factPublisher, error) {
	if bootID == "" || socket == "" {
		return nil, nil
	}
	publisher := &factPublisher{
		sources:    make(map[connectivity.Component]*connectivitycollect.Collector),
		bootID:     bootID,
		socket:     socket,
		streamPath: streamPath,
		clocks: func() (time.Duration, time.Duration, error) {
			continuous, err := policyclock.ContinuousNow()
			if err != nil {
				return 0, 0, err
			}
			awake, err := policyclock.AwakeNow()
			if err != nil {
				return 0, 0, err
			}
			return continuous, awake, nil
		},
		roundTrip: func(ctx context.Context, path string, request ipc.Request) (ipc.Response, error) {
			return (ipc.Client{Path: path}).Do(ctx, request)
		},
	}
	resumed := publisher.resumeStreams()
	built := make(map[connectivity.SourceID]*connectivitycollect.Collector)
	for _, component := range userComponents() {
		declaration, owned := safety.ConnectivityAuthority(component)
		if !owned || declaration.Domain != policy.DomainUser {
			return nil, ErrInvalidConfig
		}
		collector, known := built[declaration.Source]
		if !known {
			var err error
			collector, err = connectivitycollect.New(connectivitycollect.Options{
				Source: declaration.Source,
				Domain: policy.DomainUser,
				BootID: bootID,
				Clock:  publisherClock{},
				Random: rand.Reader,
				// Continue this source's stream rather than starting one
				// that is entirely behind the accepted watermark.
				Sequence: resumed[string(declaration.Source)],
			})
			if err != nil {
				return nil, err
			}
			built[declaration.Source] = collector
		}
		publisher.sources[component] = collector
	}
	return publisher, nil
}

// Publish sends one cycle's observations to the root aggregate.
//
// A cycle that observed nothing sends nothing. Root then lets the user
// components pass their freshness deadlines and go stale on their own
// evidence, which is true — repeating the last answer would not be.
func (publisher *factPublisher) Publish(ctx context.Context, evidence Evidence) error {
	if publisher == nil || !evidence.Reached {
		return nil
	}
	if publisher.wokeUp() {
		// What was observed before the sleep describes a host that was not
		// running, so this publication restates both components in full
		// rather than reporting now and leaving the gap unaccounted for.
		publisher.baseline = false
	}
	observations := map[connectivity.Component]connectivitycollect.Observation{
		connectivity.ComponentUserAccess: connectivitycollect.MapUserAccess(
			evidence.Profile, evidence.Service, firstError(evidence.ProfileError, evidence.ServiceError)),
		connectivity.ComponentSessionExpiry: connectivitycollect.MapUserSession(
			evidence.Session, evidence.SessionError),
	}
	encoded := make([]json.RawMessage, 0, len(observations))
	for _, component := range userComponents() {
		observation := observations[component]
		observation.Baseline = !publisher.baseline
		fact, err := publisher.sources[component].Emit(observation)
		if err != nil {
			return err
		}
		raw, err := connectivity.Encode(fact)
		if err != nil {
			return err
		}
		encoded = append(encoded, raw)
	}

	requestID, err := metadata.NewUUID(rand.Reader)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, err := publisher.roundTrip(callCtx, publisher.socket, ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: string(requestID),
		Action:    ipc.ActionPublishConnectivityFacts,
		PublishConnectivityFacts: &ipc.PublishConnectivityFactsRequest{
			Domain: policy.DomainUser,
			BootID: publisher.bootID,
			Facts:  encoded,
		},
	})
	if err != nil || response.Error != ipc.ErrorNone {
		// Root unreachable or refusing. The next cycle republishes; nothing is
		// retried out of order and nothing is buffered, because a fact held
		// back and sent later would describe a moment that has passed.
		return nil
	}
	publisher.baseline = true
	// Recorded only after root accepted them. Remembering a sequence root
	// never took would leave the next process starting above the watermark
	// and opening a hole nobody can fill.
	publisher.rememberStreams()
	return nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// resumeStreams reads back how far each source had got.
//
// A missing file is a first run, which is the one time starting at zero is
// right. A file from another boot is not this stream: the acceptor treats a
// new boot as a new stream and resets it, so continuing an old boot's numbers
// would be continuing something that no longer exists.
func (publisher *factPublisher) resumeStreams() map[string]uint64 {
	empty := map[string]uint64{}
	if publisher.streamPath == "" {
		return empty
	}
	raw, err := os.ReadFile(publisher.streamPath)
	if err != nil {
		return empty
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state streamState
	if decoder.Decode(&state) != nil || state.Schema != streamSchema ||
		state.BootID != publisher.bootID {
		return empty
	}
	if state.Sequences == nil {
		return empty
	}
	return state.Sequences
}

// rememberStreams records how far each source has got, so the next process
// continues instead of publishing behind the watermark.
func (publisher *factPublisher) rememberStreams() {
	if publisher == nil || publisher.streamPath == "" {
		return
	}
	state := streamState{
		Schema: streamSchema, BootID: publisher.bootID,
		Sequences: make(map[string]uint64, len(publisher.sources)),
	}
	for _, collector := range publisher.sources {
		state.Sequences[string(collector.Source())] = collector.Sequence()
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return
	}
	temporary := publisher.streamPath + ".partial"
	if os.WriteFile(temporary, encoded, 0o600) != nil {
		return
	}
	// A rename is what makes the file either the old sequences or the new
	// ones. A half-written file would be read as a first run and start the
	// stream again from nothing.
	_ = os.Rename(temporary, publisher.streamPath)
}

// wokeUp reports that the host slept since the last publication.
//
// The two components this daemon speaks for are time-sensitive, so a wake
// invalidates what it last said about them. Root raises that requirement for
// every component; only the owner can answer it, and until this existed the
// owner never did.
func (publisher *factPublisher) wokeUp() bool {
	continuous, awake, err := publisher.clocks()
	if err != nil {
		return false
	}
	previous, previousAwake := publisher.continuous, publisher.awake
	publisher.continuous, publisher.awake = continuous, awake
	if previous == 0 {
		return false
	}
	return (continuous-previous)-(awake-previousAwake) >= sleepFloor
}
