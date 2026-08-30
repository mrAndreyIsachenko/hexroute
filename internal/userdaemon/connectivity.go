package userdaemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
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
}

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
func newFactPublisher(bootID, socket string) (*factPublisher, error) {
	if bootID == "" || socket == "" {
		return nil, nil
	}
	publisher := &factPublisher{
		sources: make(map[connectivity.Component]*connectivitycollect.Collector),
		bootID:  bootID,
		socket:  socket,
		roundTrip: func(ctx context.Context, path string, request ipc.Request) (ipc.Response, error) {
			return (ipc.Client{Path: path}).Do(ctx, request)
		},
	}
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
