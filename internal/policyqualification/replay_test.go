package policyqualification

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestReplayDerivesGateOnlyFromCompleteDurableEvidence(t *testing.T) {
	fixture := newCompleteChain(t, chainOptions{})

	gate, err := Replay(fixture.root, fixture.binding, fixture.loader())
	if err != nil || !gate.Complete() {
		t.Fatalf("Replay() gate=%+v error=%v", gate, err)
	}
	if (Gate{}).Complete() {
		t.Fatal("zero gate enabled enforcement")
	}

	records, err := ReadRecords(fixture.root)
	if err != nil || len(records) != 10 {
		t.Fatalf("ReadRecords() count=%d error=%v", len(records), err)
	}
	for index, record := range records {
		if record.Sequence != uint64(index+1) || record.Validate() != nil {
			t.Fatalf("record[%d] = %+v", index, record)
		}
		if index > 0 && record.PreviousSHA256 != records[index-1].RecordSHA256 {
			t.Fatalf("record[%d] parent mismatch", index)
		}
	}
}

func TestReplayRejectsIncompleteQualificationCriteria(t *testing.T) {
	tests := []struct {
		name    string
		options chainOptions
	}{
		{name: "eligible duration", options: chainOptions{shortWindow: true}},
		{name: "sleep wake", options: chainOptions{skipSecondSleep: true}},
		{name: "reboot", options: chainOptions{skipReboot: true}},
		{name: "invalid signature", options: chainOptions{skipInvalidSignature: true}},
		{name: "selector conflict", options: chainOptions{skipSelectorConflict: true}},
		{name: "stale generation", options: chainOptions{skipStaleGeneration: true}},
		{name: "cross domain", options: chainOptions{skipCrossDomainCrash: true}},
		{name: "safety comparison", options: chainOptions{skipSafetyComparison: true}},
		{name: "failed evidence", options: chainOptions{failedSafetyComparison: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCompleteChain(t, test.options)
			gate, err := Replay(fixture.root, fixture.binding, fixture.loader())
			if !errors.Is(err, ErrIncompleteEvidence) || gate.Complete() {
				t.Fatalf("Replay() gate=%+v error=%v", gate, err)
			}
		})
	}
}

func TestReplayFailsClosedOnMissingReorderedRewrittenAndCrossGenerationEvidence(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		fixture := newCompleteChain(t, chainOptions{})
		records, _ := ReadRecords(fixture.root)
		records = append(records[:2], records[3:]...)
		writeRecords(t, fixture.root, records)
		assertInvalidReplay(t, fixture, fixture.binding)
	})

	t.Run("reordered", func(t *testing.T) {
		fixture := newCompleteChain(t, chainOptions{})
		records, _ := ReadRecords(fixture.root)
		records[2], records[3] = records[3], records[2]
		writeRecords(t, fixture.root, records)
		assertInvalidReplay(t, fixture, fixture.binding)
	})

	t.Run("rewritten", func(t *testing.T) {
		fixture := newCompleteChain(t, chainOptions{})
		records, _ := ReadRecords(fixture.root)
		records[0].ObservedAt = "2030-01-01T00:00:01Z"
		writeRecords(t, fixture.root, records)
		assertInvalidReplay(t, fixture, fixture.binding)
	})

	t.Run("cross generation", func(t *testing.T) {
		fixture := newCompleteChain(t, chainOptions{})
		other := fixture.binding
		other.BundleGeneration++
		assertInvalidReplay(t, fixture, other)
	})

	t.Run("source rewritten", func(t *testing.T) {
		fixture := newCompleteChain(t, chainOptions{})
		for id := range fixture.sources {
			fixture.sources[id] = []byte("rewritten source")
			break
		}
		assertInvalidReplay(t, fixture, fixture.binding)
	})
}

func TestRecorderRejectsBootChangeWithoutRebootEvidence(t *testing.T) {
	fixture := newChainFixture(t)
	recorder := fixture.recorder(t)
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	comparison := SafetyComparison{
		ExpectedSHA256: policy.SHA256Hex([]byte("same")),
		ObservedSHA256: policy.SHA256Hex([]byte("same")),
	}
	if _, err := recorder.RecordSafetyComparison(
		fixture.observation(bootOne, base, time.Second, ResultPassed, ReasonNone), comparison,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.RecordSafetyComparison(
		fixture.observation(bootTwo, base.Add(time.Second), 0, ResultPassed, ReasonNone), comparison,
	); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("boot change error = %v, want %v", err, ErrInvalidChain)
	}
}

func TestRecorderReopensAndContinuesDurableChain(t *testing.T) {
	fixture := newChainFixture(t)
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	digest := policy.SHA256Hex([]byte("same"))
	comparison := SafetyComparison{ExpectedSHA256: digest, ObservedSHA256: digest}
	first, err := fixture.recorder(t).RecordSafetyComparison(
		fixture.observation(bootOne, base, 0, ResultPassed, ReasonNone), comparison,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.recorder(t).RecordSafetyComparison(
		fixture.observation(bootOne, base.Add(time.Second), time.Second, ResultPassed, ReasonNone),
		comparison,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 || second.PreviousSHA256 != first.RecordSHA256 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestReplayRejectsInterruptedFinalRecord(t *testing.T) {
	fixture := newCompleteChain(t, chainOptions{})
	path := filepath.Join(fixture.root, ChainFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("fixture is not newline terminated")
	}
	if err := os.WriteFile(path, data[:len(data)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	assertInvalidReplay(t, fixture, fixture.binding)
}

type chainOptions struct {
	shortWindow            bool
	skipSecondSleep        bool
	skipReboot             bool
	skipInvalidSignature   bool
	skipSelectorConflict   bool
	skipStaleGeneration    bool
	skipCrossDomainCrash   bool
	skipSafetyComparison   bool
	failedSafetyComparison bool
}

type chainFixture struct {
	root       string
	binding    Binding
	sources    map[metadata.UUID][]byte
	nextSource int
}

const (
	bootOne metadata.UUID = "22222222-2222-4222-8222-222222222222"
	bootTwo metadata.UUID = "33333333-3333-4333-8333-333333333333"
)

func newChainFixture(t *testing.T) *chainFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "qualification")
	return &chainFixture{
		root: root,
		binding: Binding{
			SessionID:        "11111111-1111-4111-8111-111111111111",
			BundleGeneration: 7, RootPolicyGeneration: 5, UserPolicyGeneration: 6,
			ManifestSHA256: policy.SHA256Hex([]byte("synthetic manifest")),
		},
		sources: make(map[metadata.UUID][]byte),
	}
}

func newCompleteChain(t *testing.T, options chainOptions) *chainFixture {
	t.Helper()
	fixture := newChainFixture(t)
	recorder := fixture.recorder(t)
	base := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	pass := func(boot metadata.UUID, offset, monotonic time.Duration) Observation {
		return fixture.observation(boot, base.Add(offset), monotonic, ResultPassed, ReasonNone)
	}
	mustRecord := func(_ EvidenceRecord, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	if !options.skipSafetyComparison {
		result, reason := ResultPassed, ReasonNone
		observed := policy.SHA256Hex([]byte("same decision"))
		if options.failedSafetyComparison {
			result, reason = ResultFailed, ReasonSafetyMismatch
			observed = policy.SHA256Hex([]byte("different decision"))
		}
		mustRecord(recorder.RecordSafetyComparison(
			fixture.observation(bootOne, base.Add(time.Hour), time.Hour, result, reason),
			SafetyComparison{
				ExpectedSHA256: policy.SHA256Hex([]byte("same decision")),
				ObservedSHA256: observed,
			},
		))
	}
	mustRecord(recorder.RecordSleepWake(
		pass(bootOne, 2*time.Hour, 2*time.Hour),
		SleepWake{
			SleptAt:          base.Add(90 * time.Minute).Format(time.RFC3339Nano),
			WokeAt:           base.Add(2 * time.Hour).Format(time.RFC3339Nano),
			SleptMonotonicNS: int64(90 * time.Minute), WokeMonotonicNS: int64(2 * time.Hour),
		},
	))
	if !options.skipInvalidSignature {
		mustRecord(recorder.RecordFaultInjection(
			KindInvalidSignature, pass(bootOne, 3*time.Hour, 3*time.Hour),
			FaultInjection{Outcome: OutcomeCandidateRejected},
		))
	}
	if !options.skipSelectorConflict {
		mustRecord(recorder.RecordFaultInjection(
			KindSelectorConflict, pass(bootOne, 4*time.Hour, 4*time.Hour),
			FaultInjection{Outcome: OutcomeCandidateRejected},
		))
	}
	firstEnd := 36 * time.Hour
	if options.shortWindow {
		firstEnd = 35 * time.Hour
	}
	mustRecord(recorder.RecordEligibleWindow(
		pass(bootOne, firstEnd, firstEnd),
		EligibleWindow{
			StartedAt:        base.Format(time.RFC3339Nano),
			EndedAt:          base.Add(firstEnd).Format(time.RFC3339Nano),
			EndedMonotonicNS: int64(firstEnd),
		},
	))

	secondBoot := bootTwo
	secondMonotonicBase := time.Duration(0)
	if options.skipReboot {
		secondBoot = bootOne
		secondMonotonicBase = firstEnd
	} else {
		mustRecord(recorder.RecordReboot(
			pass(bootTwo, firstEnd, 0),
			Reboot{
				PreviousBootID: bootOne, CurrentBootID: bootTwo,
				ObservedAt: base.Add(firstEnd).Format(time.RFC3339Nano),
			},
		))
	}
	if !options.skipStaleGeneration {
		mustRecord(recorder.RecordFaultInjection(
			KindStaleGeneration, pass(secondBoot, firstEnd+time.Hour, secondMonotonicBase+time.Hour),
			FaultInjection{Outcome: OutcomeMutationRejected},
		))
	}
	if !options.skipCrossDomainCrash {
		mustRecord(recorder.RecordFaultInjection(
			KindCrossDomainCrash, pass(secondBoot, firstEnd+2*time.Hour, secondMonotonicBase+2*time.Hour),
			FaultInjection{Outcome: OutcomeDomainMismatchBlocked},
		))
	}
	if !options.skipSecondSleep {
		mustRecord(recorder.RecordSleepWake(
			pass(secondBoot, firstEnd+3*time.Hour, secondMonotonicBase+3*time.Hour),
			SleepWake{
				SleptAt:          base.Add(firstEnd + 150*time.Minute).Format(time.RFC3339Nano),
				WokeAt:           base.Add(firstEnd + 3*time.Hour).Format(time.RFC3339Nano),
				SleptMonotonicNS: int64(secondMonotonicBase + 150*time.Minute),
				WokeMonotonicNS:  int64(secondMonotonicBase + 3*time.Hour),
			},
		))
	}
	secondDuration := 36 * time.Hour
	mustRecord(recorder.RecordEligibleWindow(
		pass(secondBoot, firstEnd+secondDuration, secondMonotonicBase+secondDuration),
		EligibleWindow{
			StartedAt:          base.Add(firstEnd).Format(time.RFC3339Nano),
			EndedAt:            base.Add(firstEnd + secondDuration).Format(time.RFC3339Nano),
			StartedMonotonicNS: int64(secondMonotonicBase),
			EndedMonotonicNS:   int64(secondMonotonicBase + secondDuration),
		},
	))
	return fixture
}

func (fixture *chainFixture) recorder(t *testing.T) *Recorder {
	t.Helper()
	recorder, err := OpenRecorder(fixture.root, fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

func (fixture *chainFixture) observation(
	boot metadata.UUID,
	at time.Time,
	monotonic time.Duration,
	result Result,
	reason Reason,
) Observation {
	fixture.nextSource++
	id := metadata.UUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", fixture.nextSource))
	content := []byte(fmt.Sprintf("synthetic source %d", fixture.nextSource))
	fixture.sources[id] = content
	return Observation{
		BootID:     boot,
		Sources:    []SourceReference{{EventID: id, SHA256: policy.SHA256Hex(content)}},
		ObservedAt: at.UTC().Format(time.RFC3339Nano), SourceMonotonicNS: int64(monotonic),
		Result: result, Reason: reason,
	}
}

func (fixture *chainFixture) loader() SourceLoader {
	return SourceLoaderFunc(func(id metadata.UUID) ([]byte, error) {
		content, ok := fixture.sources[id]
		if !ok {
			return nil, os.ErrNotExist
		}
		return append([]byte(nil), content...), nil
	})
}

func assertInvalidReplay(t *testing.T, fixture *chainFixture, binding Binding) {
	t.Helper()
	gate, err := Replay(fixture.root, binding, fixture.loader())
	if err == nil || gate.Complete() {
		t.Fatalf("Replay() gate=%+v error=%v", gate, err)
	}
}

func writeRecords(t *testing.T, root string, records []EvidenceRecord) {
	t.Helper()
	var output bytes.Buffer
	for _, record := range records {
		_, encoded, err := policy.CanonicalSHA256(record)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	path := filepath.Join(root, ChainFilename)
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
