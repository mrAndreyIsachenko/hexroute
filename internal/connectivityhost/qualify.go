package connectivityhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
)

// The qualification observer records what a soak lived through: the hours the
// host was awake and being observed, the sleeps it came back from, the reboots
// it survived, and periodic proof that the stored lineage still reproduces
// from its journals.
//
// It runs inside the root daemon rather than beside it. Nothing outside can do
// this job: the store is root-owned, the boot identity and both clocks belong
// to the running process, and a separate agent would be describing a read
// model it cannot see. What it must never do is change what the daemon
// observes, so every failure here is reported and dropped, exactly like the
// read model it describes.
//
// Nothing here decides that a gate is finished. The records say what happened;
// what that adds up to is recomputed by replaying the chain.

const (
	// qualifyStateFilename holds what the observer needs to compare one cycle
	// with the last one, across daemon restarts.
	qualifyStateFilename = "observer-state.json"
	qualifyStateSchema   = "hexroute.connectivity-qualification-observer.v1"

	// qualifyMaximumGap is how much awake time may pass between samples. Past
	// it the host was up and nobody was watching, which is not eligible time
	// however honest the clocks are about it.
	qualifyMaximumGap = 15 * time.Minute
	// qualifyWakeSettle is how much awake time the read model gets to restate
	// itself after a wake before the wake is recorded as one it did not
	// recover from.
	qualifyWakeSettle = 15 * time.Minute
	// qualifyVerifyInterval is how much awake time passes between replays of
	// the stored lineage against its journals.
	qualifyVerifyInterval = time.Hour
	// clockSkewTolerance is how far the two clocks may disagree about one
	// interval before the readings are called impossible rather than
	// sequential. It is enormous next to the microseconds a pair of reads
	// costs and next to any scheduling delay between them, and tiny next to
	// the shortest sleep this notices, so nothing real hides underneath it.
	clockSkewTolerance = time.Second
)

// ErrQualification reports that the observer could not be prepared.
var ErrQualification = errors.New("connectivity qualification observer unavailable")

// pendingWake is a wake whose recovery has not been decided yet.
//
// The record is held back on purpose. What the gate wants to know about a wake
// is not that one happened but that the model came back from it, and that is
// only visible once the owners have restated themselves.
type pendingWake struct {
	SleptSeconds    uint64 `json:"slept_seconds"`
	DetectedAwakeNS int64  `json:"detected_awake_ns"`
}

// qualifyState is what one cycle needs to know about the last one.
type qualifyState struct {
	Schema    string        `json:"schema"`
	SessionID metadata.UUID `json:"session_id"`
	BootID    string        `json:"boot_id"`

	LastWall         string `json:"last_wall"`
	LastContinuousNS int64  `json:"last_continuous_ns"`
	LastAwakeNS      int64  `json:"last_awake_ns"`
	// LastVerifyAwakeNS is when the lineage was last replayed, on the clock
	// that stops during sleep, so a host that slept a week does not owe a
	// week of catch-up verifications.
	LastVerifyAwakeNS int64 `json:"last_verify_awake_ns"`

	PendingWake *pendingWake `json:"pending_wake,omitempty"`
}

// reading is one look at the three clocks a soak is measured on.
//
// They are taken together and passed as a value so a test can state a sleep, a
// gap or a disagreement directly. Nothing else could: a test that had to sleep
// for two hours to exercise a two-hour sleep would not be run.
type reading struct {
	Wall       time.Time
	Continuous time.Duration
	Awake      time.Duration
}

func systemReading() (reading, error) {
	continuous, err := policyclock.ContinuousNow()
	if err != nil {
		return reading{}, fmt.Errorf("%w: continuous clock: %v", ErrQualification, err)
	}
	awake, err := policyclock.AwakeNow()
	if err != nil {
		return reading{}, fmt.Errorf("%w: awake clock: %v", ErrQualification, err)
	}
	return reading{Wall: time.Now().UTC(), Continuous: continuous, Awake: awake}, nil
}

// Qualifier appends soak evidence for one session.
type Qualifier struct {
	recorder  *connectivityqualification.Recorder
	session   metadata.UUID
	statePath string
	chainRoot string
	state     qualifyState

	store       *connectivitycheckpoint.Store
	rootJournal *connectivityjournal.Journal
	userJournal *connectivityjournal.Journal
}

// ValidateQualification reports whether these arguments could open an
// observer, without opening one.
//
// It exists so a configuration check refuses exactly what a run would. The
// daemon is bootstrapped under KeepAlive, so an argument only the run rejects
// turns a message to whoever installed it into a restart loop.
func ValidateQualification(chainRoot, session string) error {
	if chainRoot == "" && session == "" {
		return nil
	}
	if chainRoot == "" {
		// Someone asked for a soak to be recorded. Starting without one and
		// saying nothing would leave them believing hours were being written
		// down that were not.
		return fmt.Errorf("%w: a session without a chain records nothing",
			ErrQualification)
	}
	// Without a session there is nothing keeping two runs apart, and a chain
	// holding two runs adds up to a number about neither.
	if _, err := metadata.ParseUUID(session); err != nil {
		return fmt.Errorf("%w: session must be a UUID", ErrQualification)
	}
	return nil
}

// OpenQualifier prepares the observer, or returns nil when no chain root is
// configured. A daemon started without one behaves exactly as it did before.
func OpenQualifier(
	chainRoot string,
	session string,
	store *connectivitycheckpoint.Store,
	rootJournal *connectivityjournal.Journal,
	userJournal *connectivityjournal.Journal,
) (*Qualifier, error) {
	if err := ValidateQualification(chainRoot, session); err != nil {
		return nil, err
	}
	if chainRoot == "" {
		return nil, nil
	}
	identity, err := metadata.ParseUUID(session)
	if err != nil {
		return nil, fmt.Errorf("%w: session must be a UUID", ErrQualification)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: no lineage to bind evidence to", ErrQualification)
	}
	if err := os.MkdirAll(chainRoot, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQualification, err)
	}
	qualifier := &Qualifier{
		session:     metadata.UUID(identity),
		statePath:   filepath.Join(chainRoot, qualifyStateFilename),
		chainRoot:   chainRoot,
		store:       store,
		rootJournal: rootJournal,
		userJournal: userJournal,
	}
	if err := qualifier.loadState(); err != nil {
		return nil, err
	}
	if qualifier.state.SessionID != "" && qualifier.state.SessionID != qualifier.session {
		return nil, fmt.Errorf(
			"%w: the chain at %s belongs to session %s", ErrQualification,
			chainRoot, qualifier.state.SessionID)
	}
	return qualifier, nil
}

func (qualifier *Qualifier) loadState() error {
	raw, err := os.ReadFile(qualifier.statePath)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("%w: %v", ErrQualification, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state qualifyState
	if err := decoder.Decode(&state); err != nil || state.Schema != qualifyStateSchema {
		return fmt.Errorf("%w: observer state at %s is not readable",
			ErrQualification, qualifier.statePath)
	}
	qualifier.state = state
	return nil
}

func (qualifier *Qualifier) writeState() error {
	qualifier.state.Schema = qualifyStateSchema
	qualifier.state.SessionID = qualifier.session
	encoded, err := json.Marshal(qualifier.state)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrQualification, err)
	}
	temporary := qualifier.statePath + ".partial"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: %v", ErrQualification, err)
	}
	if err := os.Rename(temporary, qualifier.statePath); err != nil {
		return fmt.Errorf("%w: %v", ErrQualification, err)
	}
	return nil
}

// Sample records what the cycle that just ran lived through.
//
// It is called after the read model has folded, so the snapshot it judges is
// the one this cycle produced. Every return is an error the caller reports and
// drops: the observer describes the soak, and a soak that stopped because its
// description failed would be a soak about nothing.
func (qualifier *Qualifier) Sample(
	bootID string,
	snapshot *connectivityreduce.Snapshot,
	now reading,
	slept time.Duration,
) error {
	if qualifier == nil {
		return nil
	}
	if now.Continuous <= 0 {
		return fmt.Errorf("%w: the cycle produced no clock reading", ErrQualification)
	}
	wall, continuous, awake := now.Wall, now.Continuous, now.Awake
	binding, ready, err := qualifier.binding(bootID)
	if err != nil {
		return err
	}
	if !ready {
		// Nothing has been checkpointed, so there is no evidence to bind a
		// record to. Recording against absent evidence is how a chain starts
		// meaning nothing while still verifying.
		return nil
	}
	recorder, err := connectivityqualification.OpenRecorder(qualifier.chainRoot, binding)
	if err != nil {
		return fmt.Errorf("%w: chain: %v", ErrQualification, err)
	}

	if qualifier.state.BootID == "" {
		qualifier.seed(bootID, wall, continuous, awake)
		return qualifier.writeState()
	}
	if bootID != qualifier.state.BootID {
		// Every deadline the prior boot issued was measured against a clock
		// that no longer exists, so nothing from before this point compares
		// with anything after it.
		if _, err := qualifier.append(recorder, binding, wall, continuous,
			connectivityqualification.KindReboot,
			connectivityqualification.ResultObserved,
			func(record *connectivityqualification.EvidenceRecord) {
				record.Reboot = &connectivityqualification.Reboot{ToBootID: bootID}
			}); err != nil {
			return err
		}
		qualifier.seed(bootID, wall, continuous, awake)
		return qualifier.writeState()
	}

	continuousDelta := continuous.Nanoseconds() - qualifier.state.LastContinuousNS
	awakeDelta := awake.Nanoseconds() - qualifier.state.LastAwakeNS
	wallDelta := int64(0)
	if last, parseErr := time.Parse(time.RFC3339Nano, qualifier.state.LastWall); parseErr == nil {
		wallDelta = wall.Sub(last).Nanoseconds()
	}
	// The continuous clock counts through sleep and the awake clock stops for
	// it, so the awake reading may lag and may never lead, and neither may run
	// backwards inside one boot.
	//
	// Compared with no tolerance that is wrong, and measurably so: the two are
	// read one after the other, so their deltas differ by whatever happened in
	// between. On this hardware the awake clock advances further than the
	// continuous one in about half of all reads, by tens of microseconds, and
	// a scheduler that parks the process between the two calls can stretch
	// that. Without a tolerance roughly every second cycle was filed as two
	// clocks that could not both be right.
	if continuousDelta == 0 {
		// Two samples at the same instant. Nothing advanced, so there is
		// nothing to measure and nothing was contradicted: recording an
		// anomaly here would call an absence of time a broken clock. A clock
		// that stayed here would simply never accrue eligible time, which is
		// the safe way to fail.
		return qualifier.writeState()
	}
	if continuousDelta < 0 || awakeDelta < -clockSkewTolerance.Nanoseconds() ||
		awakeDelta > continuousDelta+clockSkewTolerance.Nanoseconds() {
		if _, err := qualifier.append(recorder, binding, wall, continuous,
			connectivityqualification.KindClockAnomaly,
			connectivityqualification.ResultDiverged,
			func(record *connectivityqualification.EvidenceRecord) {
				record.ClockAnomaly = &connectivityqualification.ClockAnomaly{
					ContinuousDeltaNS: continuousDelta,
					AwakeDeltaNS:      awakeDelta,
					WallDeltaNS:       wallDelta,
				}
			}); err != nil {
			return err
		}
		qualifier.seed(bootID, wall, continuous, awake)
		return qualifier.writeState()
	}

	// The sleep is the reader's finding, not a second derivation here. One
	// detector means the model and the record of it can never disagree about
	// whether the host was asleep.
	if slept >= sleepFloor {
		qualifier.state.PendingWake = &pendingWake{
			SleptSeconds:    uint64(slept / time.Second),
			DetectedAwakeNS: awake.Nanoseconds(),
		}
	}
	if err := qualifier.settleWake(recorder, binding, wall, continuous, awake, snapshot); err != nil {
		return err
	}

	elapsed := time.Duration(awakeDelta)
	if seconds := uint64(elapsed / time.Second); seconds > 0 {
		result := connectivityqualification.ResultObserved
		if elapsed > qualifyMaximumGap {
			// The host was up and nothing was watching it. That time passed,
			// but it is not time this read model was observed through.
			result = connectivityqualification.ResultDiverged
		}
		if _, err := qualifier.append(recorder, binding, wall, continuous,
			connectivityqualification.KindEligibleWindow, result,
			func(record *connectivityqualification.EvidenceRecord) {
				record.EligibleWindow = &connectivityqualification.EligibleWindow{
					Seconds: seconds,
				}
			}); err != nil {
			return err
		}
	}

	qualifier.state.LastWall = wall.Format(time.RFC3339Nano)
	qualifier.state.LastContinuousNS = continuous.Nanoseconds()
	qualifier.state.LastAwakeNS = awake.Nanoseconds()
	if err := qualifier.verify(recorder, binding, wall, continuous, awake); err != nil {
		return err
	}
	return qualifier.writeState()
}

// settleWake decides a held wake once the model has had its say.
//
// What the gate wants to know about a sleep is not that one happened but that
// the read model came back from it, and that is only visible after the owners
// have restated what the wake invalidated.
func (qualifier *Qualifier) settleWake(
	recorder *connectivityqualification.Recorder,
	binding connectivityqualification.Binding,
	wall time.Time,
	continuous time.Duration,
	awake time.Duration,
	snapshot *connectivityreduce.Snapshot,
) error {
	held := qualifier.state.PendingWake
	if held == nil {
		return nil
	}
	// Recovery is about what the wake asked for, not about everything being
	// well. A wake puts every time-sensitive component back on the hook for a
	// complete restatement, and it is answered when nothing is on the hook
	// any more. Asking whether anything is stale answers a wider question:
	// a component can be stale because its own deadline passed, which is not
	// a failure to come back from a sleep, and one component quietly stale
	// for its own reasons would block every sleep this host ever records.
	recovered := snapshot != nil && snapshot.Summary.AwaitingBaseline == 0
	if recovered {
		for _, component := range snapshot.Components {
			if component.RebaselineRequired {
				recovered = false
				break
			}
		}
	}
	waited := time.Duration(awake.Nanoseconds() - held.DetectedAwakeNS)
	if !recovered && waited < qualifyWakeSettle {
		return nil
	}
	result := connectivityqualification.ResultObserved
	if !recovered {
		result = connectivityqualification.ResultDiverged
	}
	if _, err := qualifier.append(recorder, binding, wall, continuous,
		connectivityqualification.KindSleepWake, result,
		func(record *connectivityqualification.EvidenceRecord) {
			record.SleepWake = &connectivityqualification.SleepWake{
				Rebaselined: recovered,
			}
		}); err != nil {
		return err
	}
	qualifier.state.PendingWake = nil
	return nil
}

// verify replays the stored lineage against the journals it was built from.
//
// The interval is measured on the clock that stops during sleep, so a host
// that slept a week does not wake owing a week of catch-up replays.
func (qualifier *Qualifier) verify(
	recorder *connectivityqualification.Recorder,
	binding connectivityqualification.Binding,
	wall time.Time,
	continuous time.Duration,
	awake time.Duration,
) error {
	if awake.Nanoseconds()-qualifier.state.LastVerifyAwakeNS < qualifyVerifyInterval.Nanoseconds() {
		return nil
	}
	result, err := connectivitycheckpoint.Verify(
		qualifier.store, qualifier.rootJournal, qualifier.userJournal, nil)
	if err != nil {
		return fmt.Errorf("%w: verify: %v", ErrQualification, err)
	}
	unbound, err := qualifier.unboundEvidence(result)
	if err != nil {
		return err
	}
	outcome := connectivityqualification.ResultObserved
	if !result.Sound() || unbound > 0 {
		outcome = connectivityqualification.ResultDiverged
	}
	if _, err := qualifier.append(recorder, binding, wall, continuous,
		connectivityqualification.KindVerification, outcome,
		func(record *connectivityqualification.EvidenceRecord) {
			record.Verification = &connectivityqualification.Verification{
				Reproduced:   result.Reproduced,
				Diverged:     result.Diverged,
				Unreplayable: result.Unreplayable,
				Unbound:      unbound,
			}
		}); err != nil {
		return err
	}
	qualifier.state.LastVerifyAwakeNS = awake.Nanoseconds()
	return nil
}

// unboundEvidence counts the checkpoints this chain rests on that can no
// longer be reproduced from retained facts.
//
// A journal is bounded, so links nobody bound a result to fall out of reach in
// the ordinary course of running and that is not a finding. A link a recorded
// result names is different: it is what the result was derived from, and a
// result read against evidence nobody can reproduce is not evidence.
func (qualifier *Qualifier) unboundEvidence(
	result connectivitycheckpoint.VerifyResult,
) (int, error) {
	records, err := connectivityqualification.ReadRecords(qualifier.chainRoot)
	if err != nil {
		return 0, fmt.Errorf("%w: chain: %v", ErrQualification, err)
	}
	bound := make(map[string]struct{}, len(records))
	for _, record := range records {
		bound[record.Binding.CheckpointID] = struct{}{}
	}
	unbound := 0
	for _, link := range result.Links {
		if _, rests := bound[link.ID]; !rests {
			continue
		}
		switch link.Status {
		case connectivitycheckpoint.VerifyUnreplayable,
			connectivitycheckpoint.VerifyDiverged:
			unbound++
		}
	}
	return unbound, nil
}

func (qualifier *Qualifier) append(
	recorder *connectivityqualification.Recorder,
	binding connectivityqualification.Binding,
	wall time.Time,
	continuous time.Duration,
	kind connectivityqualification.Kind,
	result connectivityqualification.Result,
	fill func(*connectivityqualification.EvidenceRecord),
) (connectivityqualification.EvidenceRecord, error) {
	record, err := recorder.Append(kind, result,
		wall.Format(time.RFC3339Nano), continuous.Nanoseconds(),
		func(record *connectivityqualification.EvidenceRecord) {
			record.Binding = binding
			fill(record)
		})
	if err != nil {
		return record, fmt.Errorf("%w: %s: %v", ErrQualification, kind, err)
	}
	return record, nil
}

// binding reads the evidence this cycle's record will name.
func (qualifier *Qualifier) binding(
	bootID string,
) (connectivityqualification.Binding, bool, error) {
	pointer, err := qualifier.store.Pointer()
	if errors.Is(err, connectivitycheckpoint.ErrNotFound) {
		return connectivityqualification.Binding{}, false, nil
	}
	if err != nil {
		return connectivityqualification.Binding{}, false,
			fmt.Errorf("%w: lineage: %v", ErrQualification, err)
	}
	checkpoint, err := qualifier.store.Load(pointer.ID)
	if err != nil {
		return connectivityqualification.Binding{}, false,
			fmt.Errorf("%w: checkpoint %s: %v", ErrQualification, pointer.ID, err)
	}
	return connectivityqualification.Binding{
		SessionID:       qualifier.session,
		BootID:          bootID,
		CheckpointID:    checkpoint.ID,
		SnapshotSHA256:  checkpoint.SnapshotDigest,
		DiffSHA256:      checkpoint.DiffDigest,
		ProposalsSHA256: checkpoint.ProposalsDigest,
	}, true, nil
}

// seed restarts the comparison from here, without claiming the span it could
// not measure.
func (qualifier *Qualifier) seed(
	bootID string,
	wall time.Time,
	continuous time.Duration,
	awake time.Duration,
) {
	qualifier.state.BootID = bootID
	qualifier.state.LastWall = wall.Format(time.RFC3339Nano)
	qualifier.state.LastContinuousNS = continuous.Nanoseconds()
	qualifier.state.LastAwakeNS = awake.Nanoseconds()
	qualifier.state.LastVerifyAwakeNS = awake.Nanoseconds()
	qualifier.state.PendingWake = nil
}
