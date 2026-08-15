package telemetry

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
)

func TestCanonicalGapRepairTracesCoverRetainedAndUnrecoverableTask102Scenarios(t *testing.T) {
	traces, err := CanonicalGapRepairTraces()
	if err != nil {
		t.Fatalf("CanonicalGapRepairTraces() error = %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("trace count = %d, want 2", len(traces))
	}
	byScenario := make(map[GapRepairTraceScenario]GapRepairTrace, len(traces))
	for _, trace := range traces {
		if err := trace.Validate(); err != nil {
			t.Fatalf("%s Validate() error = %v", trace.Scenario, err)
		}
		encoded, err := EncodeAcknowledgement(trace.Acknowledgement)
		if err != nil {
			t.Fatalf("%s EncodeAcknowledgement() error = %v", trace.Scenario, err)
		}
		decoded, err := DecodeAcknowledgement(encoded)
		if err != nil {
			t.Fatalf("%s DecodeAcknowledgement() error = %v", trace.Scenario, err)
		}
		if decoded.RequestID != trace.Acknowledgement.RequestID ||
			decoded.NodeID != trace.Acknowledgement.NodeID ||
			decoded.HighWatermark != trace.Acknowledgement.HighWatermark {
			t.Fatalf("%s acknowledgement changed after decode", trace.Scenario)
		}
		assertNoGapTraceActionAuthority(t, trace)
		byScenario[trace.Scenario] = trace
	}
	retained := byScenario[GapTraceRetainedRange]
	if !retained.Expected.Replay ||
		retained.Expected.FirstSequence != 2 ||
		retained.Expected.LastSequence != 3 ||
		retained.Expected.ReplayedEvents != 2 ||
		retained.Expected.Unrecoverable {
		t.Fatalf("retained trace expectation = %+v", retained.Expected)
	}
	unrecoverable := byScenario[GapTraceUnrecoverableGap]
	if unrecoverable.Expected.Replay ||
		!unrecoverable.Expected.Unrecoverable ||
		unrecoverable.Expected.DiagnosticCode != event.DiagnosticTelemetryGapUnrecoverable ||
		!unrecoverable.Expected.NewerUploadsAllowed {
		t.Fatalf("unrecoverable trace expectation = %+v", unrecoverable.Expected)
	}
	again, err := CanonicalGapRepairTraces()
	if err != nil {
		t.Fatalf("CanonicalGapRepairTraces(second) error = %v", err)
	}
	for index, trace := range again {
		if trace.TraceSHA256 != traces[index].TraceSHA256 {
			t.Fatalf("%s trace digest changed: %s != %s", trace.Scenario, trace.TraceSHA256, traces[index].TraceSHA256)
		}
	}
}

func TestReplayGapRepairTracesRebuildsCanonicalReplayEvidence(t *testing.T) {
	traces, err := CanonicalGapRepairTraces()
	if err != nil {
		t.Fatalf("CanonicalGapRepairTraces() error = %v", err)
	}
	for _, trace := range traces {
		t.Run(string(trace.Scenario), func(t *testing.T) {
			first, err := ReplayGapRepairTrace(trace)
			if err != nil {
				t.Fatalf("ReplayGapRepairTrace() error = %v", err)
			}
			second, err := ReplayGapRepairTrace(trace)
			if err != nil {
				t.Fatalf("ReplayGapRepairTrace(second) error = %v", err)
			}
			if first.ReplaySHA256 != second.ReplaySHA256 ||
				first.TraceSHA256 != trace.TraceSHA256 ||
				first.Expected != trace.Expected ||
				first.HighWatermark != trace.Acknowledgement.HighWatermark {
				t.Fatalf("unstable gap replay:\nfirst=%+v\nsecond=%+v\ntrace=%+v", first, second, trace)
			}
		})
	}
}

func TestReplayGapRepairTraceRejectsTamperedLineage(t *testing.T) {
	trace, err := BuildCanonicalGapRepairTrace(GapTraceRetainedRange)
	if err != nil {
		t.Fatalf("BuildCanonicalGapRepairTrace() error = %v", err)
	}
	trace.RetainedSequences = []uint64{1, 3}
	if _, err := ReplayGapRepairTrace(trace); !errors.Is(err, ErrGapRepairTraceReplay) {
		t.Fatalf("ReplayGapRepairTrace(tampered retained range) error = %v, want %v", err, ErrGapRepairTraceReplay)
	}

	trace, err = BuildCanonicalGapRepairTrace(GapTraceUnrecoverableGap)
	if err != nil {
		t.Fatalf("BuildCanonicalGapRepairTrace(unrecoverable) error = %v", err)
	}
	trace.Expected.Replay = true
	if _, err := ReplayGapRepairTrace(trace); !errors.Is(err, ErrGapRepairTraceReplay) {
		t.Fatalf("ReplayGapRepairTrace(tampered expectation) error = %v, want %v", err, ErrGapRepairTraceReplay)
	}
}

func assertNoGapTraceActionAuthority(t *testing.T, trace GapRepairTrace) {
	t.Helper()
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"command", "callback", "capability_request", "local_callback",
		"policy_override", "target_override", "credential", "keychain",
		"otp", "pin", "pritunl", "vless", "twilight", "adguard",
		"sing-box", "xray", "gitlab.smart-dev", "access.medvidi",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("%s trace contains forbidden action/control fragment %q", trace.Scenario, forbidden)
		}
	}
}
