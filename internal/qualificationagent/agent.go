package qualificationagent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
)

type Agent struct {
	config   Config
	store    *store
	reader   StatusReader
	platform Platform
}

func New(config Config, reader StatusReader, platform Platform) (*Agent, error) {
	if config.Root == "" || config.RootSocket == "" || config.UserSocket == "" ||
		config.SampleInterval < 10*time.Second || config.SampleInterval > 10*time.Minute ||
		config.MaximumGap < 2*config.SampleInterval || config.MaximumGap > 30*time.Minute ||
		reader == nil || platform == nil {
		return nil, ErrInvalidConfig
	}
	storage, err := openStore(config.Root)
	if err != nil {
		return nil, err
	}
	return &Agent{config: config, store: storage, reader: reader, platform: platform}, nil
}

func (agent *Agent) Start(ctx context.Context) error {
	return agent.start(ctx, false)
}

func (agent *Agent) RestartSession(ctx context.Context) error {
	return agent.start(ctx, true)
}

func (agent *Agent) start(ctx context.Context, replace bool) error {
	lock, err := agent.store.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	if _, err := agent.store.readState(); err == nil && !replace {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) && !replace {
		return err
	}

	sample, err := agent.platform.Sample(ctx)
	if err != nil || validatePlatformSample(sample) != nil {
		return ErrUnsupportedPlatform
	}
	snapshot, err := agent.reader.ReadPolicySnapshot(ctx)
	if err != nil {
		return ErrStatusUnavailable
	}
	sessionID, err := metadata.NewUUID(nil)
	if err != nil {
		return err
	}
	binding, err := snapshot.activeBinding(sessionID)
	if err != nil {
		return ErrStatusUnavailable
	}
	recorder, sources, err := agent.session(binding)
	if err != nil {
		return err
	}
	reference, observedDigest, err := persistStatusSource(sources, binding, sample, snapshot)
	if err != nil {
		return err
	}
	expectedDigest, err := projectionDigest(expectedProjection(binding))
	if err != nil || expectedDigest != observedDigest {
		return ErrStatusUnavailable
	}
	observation := passedObservation(sample, reference)
	if _, err := recorder.RecordSafetyComparison(observation, policyqualification.SafetyComparison{
		ExpectedSHA256: expectedDigest, ObservedSHA256: observedDigest,
	}); err != nil {
		return err
	}
	return agent.store.writeState(State{
		Schema: stateSchema, Lifecycle: LifecycleCollecting, Reason: ReasonNone,
		Binding: binding, CurrentBootID: sample.BootID,
		LastObservedAt: canonicalUTC(sample.ObservedAt), LastMonotonicNS: sample.MonotonicNS,
	})
}

func (agent *Agent) Serve(ctx context.Context) error {
	runID, err := metadata.NewUUID(nil)
	if err != nil {
		return err
	}
	if err := agent.attachRun(runID); err != nil && !errors.Is(err, ErrSessionInvalid) {
		return err
	}
	agent.sampleForRun(ctx, runID)
	ticker := time.NewTicker(agent.config.SampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			agent.sampleForRun(ctx, runID)
		}
	}
}

func (agent *Agent) sampleForRun(ctx context.Context, runID metadata.UUID) {
	err := agent.Sample(ctx, runID)
	if errors.Is(err, ErrStatusUnavailable) || errors.Is(err, ErrSessionInvalid) ||
		errors.Is(err, context.Canceled) {
		return
	}
}

func (agent *Agent) attachRun(runID metadata.UUID) error {
	if metadataUUID(runID) != nil {
		return ErrInvalidState
	}
	lock, err := agent.store.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	state, err := agent.store.readState()
	if err != nil {
		return err
	}
	if state.Lifecycle != LifecycleCollecting {
		return ErrSessionInvalid
	}
	state.AgentRunID = runID
	state.SleepArm = nil
	return agent.store.writeState(state)
}

func (agent *Agent) Sample(ctx context.Context, runID metadata.UUID) error {
	lock, err := agent.store.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	state, err := agent.store.readState()
	if err != nil {
		return err
	}
	if state.Lifecycle != LifecycleCollecting || state.AgentRunID != runID {
		return ErrSessionInvalid
	}
	return agent.sampleLocked(ctx, &state)
}

func (agent *Agent) sampleLocked(ctx context.Context, state *State) error {
	recorder, sources, err := agent.session(state.Binding)
	if err != nil {
		return agent.invalidate(state, ReasonChainInvalid)
	}
	progress, err := policyqualification.Inspect(
		agent.store.sessionRoot(state.Binding), state.Binding, sources,
	)
	if err != nil {
		return agent.invalidate(state, ReasonSourceInvalid)
	}
	if progress.Complete {
		state.Lifecycle = LifecycleComplete
		state.Reason = ReasonNone
		return agent.store.writeState(*state)
	}
	sample, err := agent.platform.Sample(ctx)
	if err != nil || validatePlatformSample(sample) != nil {
		return ErrStatusUnavailable
	}
	snapshot, err := agent.reader.ReadPolicySnapshot(ctx)
	if err != nil || snapshot.Validate() != nil {
		return ErrStatusUnavailable
	}
	reference, observedDigest, err := persistStatusSource(sources, state.Binding, sample, snapshot)
	if err != nil {
		return agent.invalidate(state, ReasonSourceInvalid)
	}

	bootChanged := sample.BootID != state.CurrentBootID
	if bootChanged {
		if _, err := recorder.RecordReboot(
			passedObservation(sample, reference),
			policyqualification.Reboot{
				PreviousBootID: state.CurrentBootID, CurrentBootID: sample.BootID,
				ObservedAt: canonicalUTC(sample.ObservedAt),
			},
		); err != nil {
			return agent.invalidate(state, ReasonChainInvalid)
		}
		state.CurrentBootID = sample.BootID
		state.LastObservedAt = canonicalUTC(sample.ObservedAt)
		state.LastMonotonicNS = sample.MonotonicNS
		state.SleepArm = nil
	}

	expectedDigest, err := projectionDigest(expectedProjection(state.Binding))
	if err != nil {
		return agent.invalidate(state, ReasonChainInvalid)
	}
	if expectedDigest != observedDigest {
		reason := ReasonBindingChanged
		if snapshot.Root.AuthorizationSuspension.Suspended ||
			snapshot.User.AuthorizationSuspension.Suspended {
			reason = ReasonAuthorization
		}
		if _, err := recorder.RecordSafetyComparison(
			failedObservation(sample, reference, policyqualification.ReasonSafetyMismatch),
			policyqualification.SafetyComparison{
				ExpectedSHA256: expectedDigest, ObservedSHA256: observedDigest,
			},
		); err != nil {
			return agent.invalidate(state, ReasonChainInvalid)
		}
		return agent.invalidate(state, reason)
	}
	if bootChanged {
		return agent.store.writeState(*state)
	}

	lastAt, _ := time.Parse(time.RFC3339Nano, state.LastObservedAt)
	wallElapsed := sample.ObservedAt.Sub(lastAt)
	monotonicElapsed := time.Duration(sample.MonotonicNS - state.LastMonotonicNS)
	if wallElapsed <= 0 || monotonicElapsed <= 0 ||
		absDuration(wallElapsed-monotonicElapsed) > 2*time.Minute {
		return agent.invalidate(state, ReasonClockAnomaly)
	}
	window := policyqualification.EligibleWindow{
		StartedAt: state.LastObservedAt, EndedAt: canonicalUTC(sample.ObservedAt),
		StartedMonotonicNS: state.LastMonotonicNS, EndedMonotonicNS: sample.MonotonicNS,
	}
	if wallElapsed > agent.config.MaximumGap {
		if !agent.validSleepWake(ctx, *state, sample) {
			if _, err := recorder.RecordEligibleWindow(
				failedObservation(sample, reference, policyqualification.ReasonTimingGap), window,
			); err != nil {
				return agent.invalidate(state, ReasonChainInvalid)
			}
			return agent.invalidate(state, ReasonTimingGap)
		}
		armAt, _ := time.Parse(time.RFC3339Nano, state.SleepArm.ArmedAt)
		if _, err := recorder.RecordSleepWake(
			passedObservation(sample, reference),
			policyqualification.SleepWake{
				SleptAt: canonicalUTC(armAt), WokeAt: canonicalUTC(sample.ObservedAt),
				SleptMonotonicNS: state.SleepArm.MonotonicNS,
				WokeMonotonicNS:  sample.MonotonicNS,
			},
		); err != nil {
			return agent.invalidate(state, ReasonWakeInvalid)
		}
	}
	if _, err := recorder.RecordEligibleWindow(passedObservation(sample, reference), window); err != nil {
		return agent.invalidate(state, ReasonChainInvalid)
	}
	state.LastObservedAt = canonicalUTC(sample.ObservedAt)
	state.LastMonotonicNS = sample.MonotonicNS
	state.SleepArm = nil
	return agent.store.writeState(*state)
}

func (agent *Agent) validSleepWake(ctx context.Context, state State, sample PlatformSample) bool {
	if state.SleepArm == nil || state.SleepArm.AgentRunID != state.AgentRunID ||
		state.SleepArm.BootID != sample.BootID || sample.MonotonicNS <= state.SleepArm.MonotonicNS {
		return false
	}
	armedAt, err := time.Parse(time.RFC3339Nano, state.SleepArm.ArmedAt)
	if err != nil || sample.ObservedAt.Sub(armedAt) <= 0 ||
		sample.ObservedAt.Sub(armedAt) > maximumArmDuration ||
		absDuration(sample.ObservedAt.Sub(armedAt)-
			time.Duration(sample.MonotonicNS-state.SleepArm.MonotonicNS)) > 2*time.Minute {
		return false
	}
	wake, err := agent.platform.Wake(ctx)
	return err == nil && wake.Lid == observe.LidStateOpen && wake.Wake == observe.WakeKindFull
}

func (agent *Agent) ArmSleep(ctx context.Context) error {
	lock, err := agent.store.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	state, err := agent.store.readState()
	if err != nil {
		return err
	}
	if state.Lifecycle != LifecycleCollecting || state.AgentRunID == "" {
		return ErrSessionInvalid
	}
	sample, err := agent.platform.Sample(ctx)
	if err != nil || sample.BootID != state.CurrentBootID || sample.MonotonicNS < state.LastMonotonicNS {
		return ErrSessionInvalid
	}
	snapshot, err := agent.reader.ReadPolicySnapshot(ctx)
	if err != nil {
		return ErrStatusUnavailable
	}
	observed, err := projectionDigest(snapshot.projection())
	expected, expectedErr := projectionDigest(expectedProjection(state.Binding))
	wake, wakeErr := agent.platform.Wake(ctx)
	if err != nil || expectedErr != nil || observed != expected || wakeErr != nil ||
		wake.Lid != observe.LidStateOpen || wake.Wake != observe.WakeKindFull {
		return ErrSessionInvalid
	}
	state.SleepArm = &SleepArm{
		AgentRunID: state.AgentRunID, BootID: sample.BootID,
		ArmedAt: canonicalUTC(sample.ObservedAt), MonotonicNS: sample.MonotonicNS,
	}
	return agent.store.writeState(state)
}

func (agent *Agent) ImportFault(
	ctx context.Context,
	kind policyqualification.Kind,
	reportPath string,
) error {
	outcome, ok := passedFaultOutcome(kind)
	if !ok {
		return ErrInvalidState
	}
	reportDigest, err := digestBoundedRegularFile(reportPath)
	if err != nil {
		return ErrInvalidState
	}
	lock, err := agent.store.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	state, err := agent.store.readState()
	if err != nil || state.Lifecycle != LifecycleCollecting {
		return ErrSessionInvalid
	}
	sample, err := agent.platform.Sample(ctx)
	if err != nil || sample.BootID != state.CurrentBootID || sample.MonotonicNS < state.LastMonotonicNS {
		return ErrSessionInvalid
	}
	snapshot, err := agent.reader.ReadPolicySnapshot(ctx)
	if err != nil {
		return ErrStatusUnavailable
	}
	observed, err := projectionDigest(snapshot.projection())
	expected, expectedErr := projectionDigest(expectedProjection(state.Binding))
	if err != nil || expectedErr != nil || observed != expected {
		return ErrSessionInvalid
	}
	recorder, sources, err := agent.session(state.Binding)
	if err != nil {
		return err
	}
	eventID, err := metadata.NewUUID(nil)
	if err != nil {
		return err
	}
	source := faultSource{
		Schema: faultSourceSchema, EventID: eventID,
		ObservedAt: canonicalUTC(sample.ObservedAt), BootID: sample.BootID,
		Binding: state.Binding, Kind: kind, Outcome: outcome,
		TestReportSHA256: reportDigest,
	}
	_, content, err := policy.CanonicalSHA256(source)
	if err != nil {
		return err
	}
	reference, err := sources.put(eventID, append(content, '\n'))
	if err != nil {
		return err
	}
	_, err = recorder.RecordFaultInjection(
		kind, passedObservation(sample, reference),
		policyqualification.FaultInjection{Outcome: outcome},
	)
	return err
}

func (agent *Agent) Status() (Status, error) {
	lock, err := agent.store.lock()
	if err != nil {
		return Status{}, err
	}
	defer unlock(lock)
	state, err := agent.store.readState()
	if err != nil {
		return Status{}, err
	}
	result := Status{
		Schema: statusSchema, Lifecycle: state.Lifecycle,
		Reason: state.Reason, Binding: state.Binding,
	}
	_, sources, sessionErr := agent.session(state.Binding)
	if sessionErr != nil {
		result.Lifecycle = LifecycleInvalid
		result.Reason = ReasonChainInvalid
		return result, nil
	}
	result.Progress, err = policyqualification.Inspect(
		agent.store.sessionRoot(state.Binding), state.Binding, sources,
	)
	if err != nil {
		result.Lifecycle = LifecycleInvalid
		result.Reason = ReasonSourceInvalid
		return result, nil
	}
	if result.Progress.Complete && result.Lifecycle == LifecycleCollecting {
		result.Lifecycle = LifecycleComplete
	}
	return result, nil
}

func (agent *Agent) invalidate(state *State, reason StateReason) error {
	state.Lifecycle = LifecycleInvalid
	state.Reason = reason
	state.SleepArm = nil
	if err := agent.store.writeState(*state); err != nil {
		return err
	}
	return ErrSessionInvalid
}

func (agent *Agent) session(
	binding policyqualification.Binding,
) (*policyqualification.Recorder, *sourceStore, error) {
	recorder, err := policyqualification.OpenRecorder(agent.store.sessionRoot(binding), binding)
	if err != nil {
		return nil, nil, err
	}
	sources, err := agent.store.sources(binding)
	if err != nil {
		return nil, nil, err
	}
	return recorder, sources, nil
}

func persistStatusSource(
	sources *sourceStore,
	binding policyqualification.Binding,
	sample PlatformSample,
	snapshot PolicySnapshot,
) (policyqualification.SourceReference, string, error) {
	eventID, err := metadata.NewUUID(nil)
	if err != nil {
		return policyqualification.SourceReference{}, "", err
	}
	source := statusSource{
		Schema: statusSourceSchema, EventID: eventID,
		ObservedAt: canonicalUTC(sample.ObservedAt), BootID: sample.BootID,
		Binding: binding, Projection: snapshot.projection(),
	}
	_, content, err := policy.CanonicalSHA256(source)
	if err != nil {
		return policyqualification.SourceReference{}, "", err
	}
	reference, err := sources.put(eventID, append(content, '\n'))
	if err != nil {
		return policyqualification.SourceReference{}, "", err
	}
	digest, err := projectionDigest(source.Projection)
	return reference, digest, err
}

func projectionDigest(projection policyProjection) (string, error) {
	digest, _, err := policy.CanonicalSHA256(projection)
	return digest, err
}

func passedObservation(
	sample PlatformSample,
	reference policyqualification.SourceReference,
) policyqualification.Observation {
	return policyqualification.Observation{
		BootID: sample.BootID, Sources: []policyqualification.SourceReference{reference},
		ObservedAt: canonicalUTC(sample.ObservedAt), SourceMonotonicNS: sample.MonotonicNS,
		Result: policyqualification.ResultPassed, Reason: policyqualification.ReasonNone,
	}
}

func failedObservation(
	sample PlatformSample,
	reference policyqualification.SourceReference,
	reason policyqualification.Reason,
) policyqualification.Observation {
	result := passedObservation(sample, reference)
	result.Result = policyqualification.ResultFailed
	result.Reason = reason
	return result
}

func validatePlatformSample(sample PlatformSample) error {
	if metadataUUID(sample.BootID) != nil || sample.ObservedAt.IsZero() ||
		sample.ObservedAt.Location() != time.UTC || sample.MonotonicNS < 0 {
		return ErrUnsupportedPlatform
	}
	return nil
}

func passedFaultOutcome(kind policyqualification.Kind) (policyqualification.FaultOutcome, bool) {
	switch kind {
	case policyqualification.KindInvalidSignature, policyqualification.KindSelectorConflict:
		return policyqualification.OutcomeCandidateRejected, true
	case policyqualification.KindStaleGeneration:
		return policyqualification.OutcomeMutationRejected, true
	case policyqualification.KindCrossDomainCrash:
		return policyqualification.OutcomeDomainMismatchBlocked, true
	default:
		return "", false
	}
}

func digestBoundedRegularFile(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrInvalidState
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > policyqualification.MaximumSourceBytes {
		return "", ErrInvalidState
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, policyqualification.MaximumSourceBytes+1))
	if err != nil || len(content) == 0 || len(content) > policyqualification.MaximumSourceBytes ||
		bytes.Contains(bytes.ToLower(content), []byte("private_key")) ||
		bytes.Contains(bytes.ToLower(content), []byte("totp")) ||
		bytes.Contains(bytes.ToLower(content), []byte("password")) {
		return "", ErrInvalidState
	}
	return policy.SHA256Hex(content), nil
}

func canonicalUTC(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
