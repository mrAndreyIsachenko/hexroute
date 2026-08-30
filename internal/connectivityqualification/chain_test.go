package connectivityqualification

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const digestA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func binding() Binding {
	return Binding{
		SessionID:       metadata.UUID("11111111-1111-4111-8111-111111111111"),
		BootID:          "boot-0000000000000000",
		CheckpointID:    "cp-0001",
		SnapshotSHA256:  digestA,
		DiffSHA256:      digestA,
		ProposalsSHA256: digestA,
	}
}

func recorder(t *testing.T, root string) *Recorder {
	t.Helper()
	made, err := OpenRecorder(root, binding())
	if err != nil {
		t.Fatalf("open recorder: %v", err)
	}
	return made
}

func window(t *testing.T, made *Recorder, seconds uint64) {
	t.Helper()
	if _, err := made.Append(KindEligibleWindow, ResultObserved, "2026-08-30T12:00:00Z", 1,
		func(record *EvidenceRecord) {
			record.EligibleWindow = &EligibleWindow{Seconds: seconds}
		}); err != nil {
		t.Fatalf("append window: %v", err)
	}
}

// Completion is derived from the records every time. Nothing may assert it,
// which is what the spec means by refusing aggregate flags.
func TestCompletionIsDerivedAndNotAsserted(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, EligibleHours*3600)

	progress, err := Inspect(root, binding())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if progress.Complete {
		t.Fatal("enough eligible time alone completed the gate")
	}
	if progress.Blocking == "" {
		t.Fatal("an incomplete gate did not say what stops it")
	}
	if len(progress.FaultsMissing) != len(connectivitytrace.Faults()) {
		t.Fatalf("%d faults missing, want all of them", len(progress.FaultsMissing))
	}
}

// A whole run completes; the same run missing one trace does not.
func TestOneMissingTraceStopsCompletion(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, EligibleHours*3600)
	for cycle := 0; cycle < RequiredSleepWakeCycles; cycle++ {
		if _, err := made.Append(KindSleepWake, ResultExpected, "2026-08-30T13:00:00Z", 2,
			func(r *EvidenceRecord) { r.SleepWake = &SleepWake{Rebaselined: true} }); err != nil {
			t.Fatalf("append wake: %v", err)
		}
	}
	if _, err := made.Append(KindReboot, ResultExpected, "2026-08-30T14:00:00Z", 3,
		func(r *EvidenceRecord) {
			r.Reboot = &Reboot{ToBootID: "boot-1111111111111111"}
		}); err != nil {
		t.Fatalf("append reboot: %v", err)
	}
	faults := connectivitytrace.Faults()
	for _, fault := range faults[:len(faults)-1] {
		if _, err := made.Append(KindFaultInjection, ResultExpected, "2026-08-30T15:00:00Z", 4,
			func(r *EvidenceRecord) {
				r.FaultInjection = &FaultInjection{
					Fault: fault, TraceSHA256: digestA, Visible: "recorded",
				}
			}); err != nil {
			t.Fatalf("append fault: %v", err)
		}
	}
	progress, err := Inspect(root, binding())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if progress.Complete {
		t.Fatal("a run missing one trace completed")
	}
	if len(progress.FaultsMissing) != 1 {
		t.Fatalf("missing %v, want exactly one", progress.FaultsMissing)
	}

	// Injecting the last one completes it.
	if _, err := made.Append(KindFaultInjection, ResultExpected, "2026-08-30T15:30:00Z", 5,
		func(r *EvidenceRecord) {
			r.FaultInjection = &FaultInjection{
				Fault: faults[len(faults)-1], TraceSHA256: digestA, Visible: "recorded",
			}
		}); err != nil {
		t.Fatalf("append last fault: %v", err)
	}
	progress, err = Inspect(root, binding())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !progress.Complete {
		t.Fatalf("a whole run did not complete: %s", progress.Blocking)
	}
}

// One healthy-looking outcome from an injected fault ends the run. It is not
// averaged against the rest.
func TestAGuessedHealthyOutcomeEndsTheRun(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, EligibleHours*3600)
	if _, err := made.Append(KindFaultInjection, ResultDiverged, "2026-08-30T15:00:00Z", 4,
		func(r *EvidenceRecord) {
			r.FaultInjection = &FaultInjection{
				Fault: connectivitytrace.FaultGap, TraceSHA256: digestA,
				Visible: "reported ready", GuessedHealthy: true,
			}
		}); err != nil {
		t.Fatalf("append: %v", err)
	}
	progress, err := Inspect(root, binding())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if progress.Complete || !progress.GuessedHealthy {
		t.Fatalf("progress = %+v", progress)
	}
	if !strings.Contains(progress.Blocking, "healthy") {
		t.Fatalf("blocking = %q", progress.Blocking)
	}
}

// A rewritten record breaks its own seal, and the chain refuses.
func TestRewritingARecordBreaksTheChain(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, 3600)
	window(t, made, 3600)

	path := filepath.Join(root, ChainFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tampered := strings.Replace(string(raw), `"seconds":3600`, `"seconds":9999`, 1)
	if tampered == string(raw) {
		t.Fatal("the fixture no longer contains the value to rewrite")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Inspect(root, binding()); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("error = %v, want ErrInvalidChain", err)
	}
}

// A removed link leaves the one after it naming a predecessor that is not
// there.
func TestRemovingALinkBreaksTheChain(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, 3600)
	window(t, made, 3600)
	window(t, made, 3600)

	path := filepath.Join(root, ChainFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	without := append([]string{lines[0]}, lines[2:]...)
	if err := os.WriteFile(path,
		[]byte(strings.Join(without, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Inspect(root, binding()); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("error = %v, want ErrInvalidChain", err)
	}
}

// Evidence from another run describes another host state; two sessions in one
// chain add up to a number about neither.
func TestCrossSessionEvidenceIsRefused(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, 3600)

	other := binding()
	other.SessionID = metadata.UUID("22222222-2222-4222-8222-222222222222")
	if _, err := Inspect(root, other); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("error = %v, want ErrInvalidChain", err)
	}
	if _, err := OpenRecorder(root, other); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("a recorder opened onto another session's chain: %v", err)
	}
}

// A recorder must not extend a chain it cannot prove.
func TestRecorderRefusesToExtendATamperedChain(t *testing.T) {
	root := t.TempDir()
	made := recorder(t, root)
	window(t, made, 3600)

	path := filepath.Join(root, ChainFilename)
	raw, _ := os.ReadFile(path)
	tampered := strings.Replace(string(raw), `"seconds":3600`, `"seconds":7200`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := OpenRecorder(root, binding()); err == nil {
		t.Fatal("a recorder opened onto a tampered chain")
	}
}
