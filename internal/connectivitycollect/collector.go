// Package connectivitycollect turns the observations the daemons already make
// into typed connectivity facts.
//
// It adds no new way to look at the host and no way at all to change it: every
// mapper takes an observation that was already gathered and returns a bounded
// payload. Collection cannot read a credential, because the observations it
// consumes do not carry one and the payloads have nowhere to put one.
package connectivitycollect

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

var (
	ErrInvalidCollector = errors.New("connectivity collector is misconfigured")
	ErrNotOwned         = errors.New("collector does not own the component")
)

// DefaultFreshnessTTL is how far ahead of an observation its deadline sits
// when the caller does not choose.
const DefaultFreshnessTTL = control.Tick(300)

// Clock supplies the time context a fact is stamped with. It is an interface
// so collection is deterministic under test.
type Clock interface {
	Wall() time.Time
	Tick() control.Tick
}

// Options configures one collector.
type Options struct {
	Source       connectivity.SourceID
	Domain       policy.Domain
	BootID       string
	Clock        Clock
	Random       io.Reader
	FreshnessTTL control.Tick
}

// Collector stamps identity, order and time onto the payloads the mappers
// produce. One collector owns one source's sequence.
type Collector struct {
	source   connectivity.SourceID
	domain   policy.Domain
	bootID   string
	clock    Clock
	random   io.Reader
	ttl      control.Tick
	mu       sync.Mutex
	sequence uint64
}

// New returns a collector for one source.
func New(options Options) (*Collector, error) {
	if options.Source == "" || options.Clock == nil || options.Random == nil {
		return nil, fmt.Errorf("%w: source, clock and randomness are required", ErrInvalidCollector)
	}
	if options.Domain != policy.DomainRoot && options.Domain != policy.DomainUser {
		return nil, fmt.Errorf("%w: domain %q", ErrInvalidCollector, options.Domain)
	}
	if options.BootID == "" {
		return nil, fmt.Errorf("%w: boot identity is required", ErrInvalidCollector)
	}
	ttl := options.FreshnessTTL
	if ttl <= 0 {
		ttl = DefaultFreshnessTTL
	}
	return &Collector{
		source: options.Source, domain: options.Domain, bootID: options.BootID,
		clock: options.Clock, random: options.Random, ttl: ttl,
	}, nil
}

// Observation is what a mapper produced about one component.
type Observation struct {
	Component connectivity.Component
	Lifecycle connectivity.Lifecycle
	Reason    connectivity.Reason
	Payload   connectivity.Payload
	// Baseline marks a complete restatement. Collectors emit one on their
	// first publication and after a wake or reboot, because those are the
	// moments a partial answer would be mistaken for a whole one.
	Baseline bool
}

// Emit builds the next fact for an observation.
//
// Ownership is checked against the compiled envelope here rather than trusted:
// a collector wired to the wrong component would otherwise produce facts that
// the acceptor rejects one layer later, with the cause a layer away from the
// mistake.
func (collector *Collector) Emit(observation Observation) (connectivity.Fact, error) {
	declaration, owned := safety.ConnectivityAuthority(observation.Component)
	if !owned || declaration.Source != collector.source {
		return connectivity.Fact{}, fmt.Errorf("%w: %q is not owned by %q",
			ErrNotOwned, observation.Component, collector.source)
	}
	eventID, err := metadata.NewUUID(collector.random)
	if err != nil {
		return connectivity.Fact{}, fmt.Errorf("%w: %v", ErrInvalidCollector, err)
	}

	collector.mu.Lock()
	collector.sequence++
	sequence := collector.sequence
	collector.mu.Unlock()

	tick := collector.clock.Tick()
	fact := connectivity.Fact{
		Schema:            connectivity.FactSchema,
		Version:           connectivity.FactSchemaVersion,
		EventID:           string(eventID),
		Domain:            collector.domain,
		Component:         observation.Component,
		SourceID:          collector.source,
		BootID:            collector.bootID,
		SourceSequence:    sequence,
		ObservedAt:        collector.clock.Wall().UTC(),
		MonotonicTick:     tick,
		FreshnessDeadline: tick + collector.ttl,
		Lifecycle:         observation.Lifecycle,
		Reason:            observation.Reason,
		Baseline:          observation.Baseline,
		Payload:           observation.Payload,
	}
	if err := connectivity.Validate(fact); err != nil {
		return connectivity.Fact{}, err
	}
	return fact, nil
}

// Sequence reports the last sequence this collector issued.
func (collector *Collector) Sequence() uint64 {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	return collector.sequence
}
