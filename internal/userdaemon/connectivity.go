package userdaemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycollect"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/operator"
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
	// deadline bounds one publication so a slow peer costs a publication
	// rather than the cycle that issued it.
	deadline time.Duration

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

	// refusals counts publications in a row that root did not take.
	//
	// A refusal costs the cycle nothing by design, and that is exactly why it
	// has to be counted: without it the loop goes on reporting healthy while
	// the two components this domain speaks for keep whatever they last said,
	// for as long as it takes somebody to notice by hand.
	refusals uint64
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
// publishDeadline is how long a publication may wait for the peer.
//
// It is derived from the observation interval rather than fixed, because the
// property worth keeping is the relationship: waiting must end well inside the
// cycle that issued it, whatever the cycle is set to. A constant drifts out of
// that relationship the moment the interval is reconfigured, and drifts
// silently.
//
// The default that this replaces was fifteen seconds against a fifteen-second
// cycle, so one slow peer consumed the whole of it. That is how a fault in the
// root daemon presented as a user daemon that would not answer.
func publishDeadline(interval time.Duration) time.Duration {
	deadline := interval / 3
	if deadline < time.Second {
		return time.Second
	}
	return deadline
}

func newFactPublisher(
	bootID, socket, streamPath string,
	interval time.Duration,
) (*factPublisher, error) {
	if bootID == "" || socket == "" {
		return nil, nil
	}
	deadline := publishDeadline(interval)
	publisher := &factPublisher{
		deadline:   deadline,
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
			// The deadline goes on the client as well as the context: Do
			// honours the context while connecting and its own timeout while
			// waiting for the answer, so a context alone bounds the wrong half.
			return (ipc.Client{Path: path, Timeout: deadline}).Do(ctx, request)
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
func (publisher *factPublisher) Publish(
	ctx context.Context,
	evidence Evidence,
	logger *logging.Logger,
) error {
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
	callCtx, cancel := context.WithTimeout(ctx, publisher.deadline)
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
		//
		// Dropped, but no longer in silence. Refusing to fail the cycle is the
		// right answer and it is also what hid a day of refused publications:
		// root answers most refusals with an error code, which is a well formed
		// response it does not report, so neither side wrote anything down
		// while the stream stood still.
		publisher.reportRefusal(logger, publicationReason(err, response.Error))
		return nil
	}
	publisher.reportAccepted(logger)
	publisher.baseline = true
	// Root holds the truth about where each stream stands. A publisher can
	// remember its own position, but only across a restart it survived to
	// write about — the first run after an upgrade, a lost file or a crash
	// between the publication and the write has nothing to remember, and
	// beginning again at zero puts every fact behind the accepted watermark
	// for as long as the boot lasts.
	//
	// So the position is adopted from the answer. A publication that was
	// refused for being behind is corrected by the same response that refused
	// it, and the next cycle lands.
	publisher.adoptStreams(response.PublishConnectivityFacts)
	// Recorded only after root accepted them. Remembering a sequence root
	// never took would leave the next process starting above the watermark
	// and opening a hole nobody can fill.
	publisher.rememberStreams()
	return nil
}

// adoptStreams moves each source up to where root says it has been accepted.
//
// It only ever moves forward. Root reporting a lower position than this
// process has already published would mean the two disagree about what was
// accepted, and winding back would republish sequences root has already taken.
func (publisher *factPublisher) adoptStreams(result *ipc.PublishConnectivityFactsResult) {
	if result == nil {
		return
	}
	for _, position := range result.Streams {
		for _, collector := range publisher.sources {
			if string(collector.Source()) != position.Source {
				continue
			}
			if position.LastSequence > collector.Sequence() {
				collector.Resume(position.LastSequence)
				// The stream this process was publishing was never accepted,
				// so what it last said about these components did not land.
				// The next publication restates them in full.
				publisher.baseline = false
			}
		}
	}
}

// refusalReportEvery restates a continuing refusal about once an hour at the
// default cycle.
//
// A line every cycle would bury the log it exists to make readable, and a line
// only at the transition would leave an outage that began before anyone looked
// with nothing in the tail to find.
const refusalReportEvery = 240

// reportRefusal writes down a publication root did not take.
func (publisher *factPublisher) reportRefusal(
	logger *logging.Logger,
	reason logging.Reason,
) {
	publisher.refusals++
	if publisher.refusals == 1 || publisher.refusals%refusalReportEvery == 0 {
		_ = logger.Emit(logging.LevelWarn, logging.EventConnectivityPublication,
			logging.ResultRejected, reason)
	}
}

// reportAccepted closes a run of refusals, so the log says when it ended and
// not only that it began.
func (publisher *factPublisher) reportAccepted(logger *logging.Logger) {
	if publisher.refusals == 0 {
		return
	}
	publisher.refusals = 0
	_ = logger.Emit(logging.LevelInfo, logging.EventConnectivityPublication,
		logging.ResultOK, "")
}

// publicationReason names what root said, or what stopped the round trip.
//
// The distinction worth keeping is between an answer and no answer: an error
// code is root deciding, and a transport failure is root never getting to. A
// reason that collapsed the two would say a refusal happened without saying
// which side to go and look at.
func publicationReason(err error, code ipc.ErrorCode) logging.Reason {
	if err != nil {
		// A round trip can fail without root ever hearing it. The client
		// validates the request before it dials, so a publication this daemon
		// built wrong never leaves the process — and reading that as an
		// unreachable socket sends the next reader to the wrong machine.
		if named, ok := operator.RejectionReason(err); ok {
			return named
		}
		switch {
		case errors.Is(err, os.ErrDeadlineExceeded),
			errors.Is(err, context.DeadlineExceeded):
			return logging.ReasonPublicationTimeout
		case errors.Is(err, fs.ErrNotExist):
			return logging.ReasonSocketAbsent
		case errors.Is(err, fs.ErrPermission):
			return logging.ReasonSocketDenied
		}
		return logging.ReasonSocketUnavailable
	}
	switch code {
	case ipc.ErrorInvalidRequest:
		return logging.ReasonMalformedRequest
	case ipc.ErrorUnauthorized:
		return logging.ReasonUnauthorizedPeer
	case ipc.ErrorStaleGeneration:
		return logging.ReasonGenerationConflict
	case ipc.ErrorPrecondition:
		return logging.ReasonReadModelUnavailable
	default:
		return logging.ReasonRootInternal
	}
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
