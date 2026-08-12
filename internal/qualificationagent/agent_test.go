package qualificationagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

const (
	testBootOne metadata.UUID = "11111111-1111-4111-8111-111111111111"
	testBootTwo metadata.UUID = "22222222-2222-4222-8222-222222222222"
	testRunID   metadata.UUID = "33333333-3333-4333-8333-333333333333"
)

type fakeStatusReader struct {
	snapshots []PolicySnapshot
	index     int
}

func (reader *fakeStatusReader) ReadPolicySnapshot(context.Context) (PolicySnapshot, error) {
	if len(reader.snapshots) == 0 {
		return PolicySnapshot{}, ErrStatusUnavailable
	}
	index := reader.index
	if index >= len(reader.snapshots) {
		index = len(reader.snapshots) - 1
	} else {
		reader.index++
	}
	return reader.snapshots[index], nil
}

type fakePlatform struct {
	samples   []PlatformSample
	index     int
	wake      userobserve.WakeObservation
	wakes     []userobserve.WakeObservation
	wakeIndex int
}

func (platform *fakePlatform) Sample(context.Context) (PlatformSample, error) {
	if platform.index >= len(platform.samples) {
		return PlatformSample{}, ErrUnsupportedPlatform
	}
	sample := platform.samples[platform.index]
	platform.index++
	return sample, nil
}

func (platform *fakePlatform) Wake(context.Context) (userobserve.WakeObservation, error) {
	if len(platform.wakes) != 0 {
		index := platform.wakeIndex
		if index >= len(platform.wakes) {
			index = len(platform.wakes) - 1
		} else {
			platform.wakeIndex++
		}
		return platform.wakes[index], nil
	}
	return platform.wake, nil
}

func TestAgentCollectsEligibleWindowsAndRecoversReboot(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, time.Hour),
			testSample(testBootOne, base.Add(10*time.Second), time.Hour+10*time.Second),
			testSample(testBootTwo, base.Add(20*time.Second), 5*time.Second),
			testSample(testBootTwo, base.Add(30*time.Second), 15*time.Second),
		},
	})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.attachRun(testRunID); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := agent.Sample(context.Background(), testRunID); err != nil {
			t.Fatal(err)
		}
	}
	status, err := agent.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle != LifecycleCollecting || status.Progress.RecordCount != 4 ||
		status.Progress.EligibleSeconds != 20 || !status.Progress.RebootObserved {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentFailsClosedOnUnarmedSamplingGap(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(30*time.Second), 30*time.Second),
		},
	})
	startAndAttach(t, agent)
	if err := agent.Sample(context.Background(), testRunID); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrSessionInvalid)
	}
	status, _ := agent.Status()
	if status.Lifecycle != LifecycleInvalid || status.Reason != ReasonTimingGap ||
		!status.Progress.FailedEvidence {
		t.Fatalf("status = %+v", status)
	}
}

func TestServeExitsWhenCurrentSessionIsInvalid(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(30*time.Second), 30*time.Second),
		},
	})
	startAndAttach(t, agent)
	if err := agent.Sample(context.Background(), testRunID); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrSessionInvalid)
	}
	if err := agent.Serve(context.Background()); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Serve() error = %v, want %v", err, ErrSessionInvalid)
	}
}

func TestServeExitsWhenSamplingInvalidatesSession(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(30*time.Second), 30*time.Second),
		},
	})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := agent.Serve(ctx); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Serve() error = %v, want %v", err, ErrSessionInvalid)
	}
	status, err := agent.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle != LifecycleInvalid || status.Reason != ReasonTimingGap ||
		!status.Progress.FailedEvidence {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentRejectsBindingChangeWithDurableFailedComparison(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	changed := activeSnapshot()
	changed.Root.Status.BundleGeneration++
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot(), changed}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(10*time.Second), 10*time.Second),
		},
	})
	startAndAttach(t, agent)
	if err := agent.Sample(context.Background(), testRunID); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Sample() error = %v", err)
	}
	status, _ := agent.Status()
	if status.Lifecycle != LifecycleInvalid || status.Reason != ReasonBindingChanged ||
		!status.Progress.FailedEvidence {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentCountsOnlyExplicitlyArmedFullWake(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	platform := &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, time.Hour),
			testSample(testBootOne, base.Add(time.Second), time.Hour+time.Second),
			testSample(testBootOne, base.Add(31*time.Second), time.Hour+31*time.Second),
		},
		wake: userobserve.WakeObservation{Lid: observe.LidStateOpen, Wake: observe.WakeKindFull},
	}
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, platform)
	startAndAttach(t, agent)
	if err := agent.ArmSleep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.Sample(context.Background(), testRunID); err != nil {
		t.Fatal(err)
	}
	status, _ := agent.Status()
	if status.Lifecycle != LifecycleCollecting || status.Progress.SleepWakeCycles != 1 ||
		status.Progress.EligibleSeconds != 31 {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentDefersArmedDarkWakeUntilFullWake(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	fullWake := userobserve.WakeObservation{Lid: observe.LidStateOpen, Wake: observe.WakeKindFull}
	platform := &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, time.Hour),
			testSample(testBootOne, base.Add(time.Second), time.Hour+time.Second),
			testSample(testBootOne, base.Add(31*time.Second), time.Hour+31*time.Second),
			testSample(testBootOne, base.Add(41*time.Second), time.Hour+41*time.Second),
		},
		wakes: []userobserve.WakeObservation{
			fullWake,
			{Lid: observe.LidStateClosed, Wake: observe.WakeKindDark},
			fullWake,
		},
	}
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, platform)
	startAndAttach(t, agent)
	if err := agent.ArmSleep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.Sample(context.Background(), testRunID); err != nil {
		t.Fatalf("dark wake sample: %v", err)
	}
	status, _ := agent.Status()
	if status.Lifecycle != LifecycleCollecting || status.Progress.SleepWakeCycles != 0 ||
		status.Progress.FailedEvidence {
		t.Fatalf("dark wake status = %+v", status)
	}
	if err := agent.Sample(context.Background(), testRunID); err != nil {
		t.Fatalf("full wake sample: %v", err)
	}
	status, _ = agent.Status()
	if status.Lifecycle != LifecycleCollecting || status.Progress.SleepWakeCycles != 1 ||
		status.Progress.EligibleSeconds != 41 || status.Progress.FailedEvidence {
		t.Fatalf("full wake status = %+v", status)
	}
}

func TestAgentKeepsArmAcrossRegularSamplesBeyondFiveMinutes(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	fullWake := userobserve.WakeObservation{Lid: observe.LidStateOpen, Wake: observe.WakeKindFull}
	samples := []PlatformSample{
		testSample(testBootOne, base, time.Hour),
		testSample(testBootOne, base.Add(time.Second), time.Hour+time.Second),
	}
	for elapsed := 10 * time.Second; elapsed <= 6*time.Minute; elapsed += 10 * time.Second {
		samples = append(samples, testSample(testBootOne, base.Add(elapsed), time.Hour+elapsed))
	}
	samples = append(samples, testSample(
		testBootOne,
		base.Add(6*time.Minute+30*time.Second),
		time.Hour+6*time.Minute+30*time.Second,
	))
	platform := &fakePlatform{
		samples: samples,
		wake:    fullWake,
	}
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, platform)
	startAndAttach(t, agent)
	if err := agent.ArmSleep(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index := 2; index < len(samples); index++ {
		if err := agent.Sample(context.Background(), testRunID); err != nil {
			t.Fatalf("sample %d: %v", index, err)
		}
	}
	status, _ := agent.Status()
	if status.Lifecycle != LifecycleCollecting || status.Progress.SleepWakeCycles != 1 ||
		status.Progress.EligibleSeconds != 390 || status.Progress.FailedEvidence {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentRetainsRejectedArmForInvalidSessionForensics(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	platform := &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(time.Second), time.Second),
			testSample(testBootOne, base.Add(25*time.Hour), 25*time.Hour),
		},
		wake: userobserve.WakeObservation{Lid: observe.LidStateOpen, Wake: observe.WakeKindFull},
	}
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, platform)
	startAndAttach(t, agent)
	if err := agent.ArmSleep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.Sample(context.Background(), testRunID); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrSessionInvalid)
	}
	state, err := agent.store.readState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != LifecycleInvalid || state.Reason != ReasonTimingGap || state.SleepArm == nil {
		t.Fatalf("state = %+v", state)
	}
}

func TestAgentImportsOnlyDigestOfControlledFaultReport(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{
			testSample(testBootOne, base, 0),
			testSample(testBootOne, base.Add(time.Second), time.Second),
		},
	})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(report, []byte("ok synthetic invalid signature test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.ImportFault(
		context.Background(), policyqualification.KindInvalidSignature, report,
	); err != nil {
		t.Fatal(err)
	}
	status, _ := agent.Status()
	if !status.Progress.InvalidSignature {
		t.Fatalf("status = %+v", status)
	}
	sources, err := filepath.Glob(filepath.Join(agent.config.Root, "sessions", "*", "sources", "*.json"))
	if err != nil || len(sources) != 2 {
		t.Fatalf("sources=%v error=%v", sources, err)
	}
	for _, path := range sources {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytesContains(content, []byte("synthetic invalid signature")) {
			t.Fatalf("source retained raw report: %s", content)
		}
	}
}

func TestStatusFailsClosedWhenSourceIsRewritten(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	agent := newTestAgent(t, &fakeStatusReader{snapshots: []PolicySnapshot{activeSnapshot()}}, &fakePlatform{
		samples: []PlatformSample{testSample(testBootOne, base, 0)},
	})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sources, _ := filepath.Glob(filepath.Join(agent.config.Root, "sessions", "*", "sources", "*.json"))
	if len(sources) != 1 {
		t.Fatalf("sources = %v", sources)
	}
	if err := os.WriteFile(sources[0], []byte("rewritten\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := agent.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle != LifecycleInvalid || status.Reason != ReasonSourceInvalid {
		t.Fatalf("status = %+v", status)
	}
}

func newTestAgent(t *testing.T, reader StatusReader, platform Platform) *Agent {
	t.Helper()
	root := filepath.Join(t.TempDir(), "qualification")
	agent, err := New(Config{
		Root: root, RootSocket: "/tmp/root.sock", UserSocket: "/tmp/user.sock",
		SampleInterval: 10 * time.Second, MaximumGap: 20 * time.Second,
	}, reader, platform)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func startAndAttach(t *testing.T, agent *Agent) {
	t.Helper()
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.attachRun(testRunID); err != nil {
		t.Fatal(err)
	}
}

func activeSnapshot() PolicySnapshot {
	digest := policy.SHA256Hex([]byte("synthetic manifest"))
	status := func(domain policy.Domain, generation uint64) ipc.PolicyStatusResult {
		return ipc.PolicyStatusResult{
			Status: policy.Status{
				Schema: policy.PolicyStatusSchema, Domain: domain, State: policy.PolicyActive,
				BundleGeneration: 2, PolicyGeneration: generation,
				ManifestSHA256: digest, ActivatedAt: "2030-01-01T00:00:00Z",
				Reason: policy.ReasonNone,
			},
			AuthorizationSuspension: policy.AuthorizationSuspension{
				Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
			},
		}
	}
	return PolicySnapshot{Root: status(policy.DomainRoot, 2), User: status(policy.DomainUser, 1)}
}

func testSample(boot metadata.UUID, at time.Time, monotonic time.Duration) PlatformSample {
	return PlatformSample{BootID: boot, ObservedAt: at, MonotonicNS: int64(monotonic)}
}

func bytesContains(content, target []byte) bool {
	for index := 0; index+len(target) <= len(content); index++ {
		if string(content[index:index+len(target)]) == string(target) {
			return true
		}
	}
	return false
}
