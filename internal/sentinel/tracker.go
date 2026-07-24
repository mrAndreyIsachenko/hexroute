package sentinel

import (
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

type Action string

const ActionNone Action = "none"

type HeartbeatObservation struct {
	Present  bool
	Sequence uint64
}

type Decision struct {
	ObserveOnly    bool
	HeartbeatStale bool
	DataPathBroken bool
	EvidenceReady  bool
	Action         Action
}

type Tracker struct {
	staleThreshold    control.Tick
	initialized       bool
	lastSequence      uint64
	lastSequenceAt    control.Tick
	lastObservationAt control.Tick
}

var ErrInvalidObservation = errors.New("invalid sentinel observation")

func NewTracker(staleThreshold control.Tick) (*Tracker, error) {
	if staleThreshold <= 0 {
		return nil, ErrInvalidObservation
	}
	return &Tracker{staleThreshold: staleThreshold}, nil
}

func (tracker *Tracker) Evaluate(
	at control.Tick,
	heartbeat HeartbeatObservation,
	dataPathReady bool,
) (Decision, error) {
	if tracker == nil ||
		tracker.staleThreshold <= 0 ||
		at < 0 ||
		(heartbeat.Present && heartbeat.Sequence == 0) ||
		(!heartbeat.Present && heartbeat.Sequence != 0) ||
		(tracker.initialized && at < tracker.lastObservationAt) {
		return Decision{}, ErrInvalidObservation
	}
	if !tracker.initialized {
		tracker.initialized = true
		tracker.lastSequence = heartbeat.Sequence
		tracker.lastSequenceAt = at
	} else if heartbeat.Present {
		if heartbeat.Sequence < tracker.lastSequence {
			return Decision{}, ErrInvalidObservation
		}
		if heartbeat.Sequence > tracker.lastSequence {
			tracker.lastSequence = heartbeat.Sequence
			tracker.lastSequenceAt = at
		}
	}
	tracker.lastObservationAt = at

	stale := at-tracker.lastSequenceAt >= tracker.staleThreshold
	broken := !dataPathReady
	return Decision{
		ObserveOnly:    true,
		HeartbeatStale: stale,
		DataPathBroken: broken,
		EvidenceReady:  stale && broken,
		Action:         ActionNone,
	}, nil
}
