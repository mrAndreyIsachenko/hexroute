package reconciler

import (
	"errors"
	"testing"
)

func TestReplaySyntheticTracesRebuildsCanonicalEvidence(t *testing.T) {
	traces, err := CanonicalSyntheticTraces()
	if err != nil {
		t.Fatalf("CanonicalSyntheticTraces() error = %v", err)
	}
	for _, trace := range traces {
		t.Run(string(trace.Scenario), func(t *testing.T) {
			first, err := ReplaySyntheticTrace(trace)
			if err != nil {
				t.Fatalf("ReplaySyntheticTrace() error = %v", err)
			}
			second, err := ReplaySyntheticTrace(trace)
			if err != nil {
				t.Fatalf("ReplaySyntheticTrace(second) error = %v", err)
			}
			if first.ReplaySHA256 != second.ReplaySHA256 ||
				first.TraceSHA256 != trace.TraceSHA256 ||
				first.Expected != trace.Expected ||
				len(first.RecordSHA256) != len(trace.Records) ||
				len(first.Provenance) != len(trace.Records) {
				t.Fatalf("unstable replay:\nfirst=%+v\nsecond=%+v\ntrace=%+v", first, second, trace)
			}
		})
	}
}

func TestReplaySyntheticTraceRejectsTamperedCanonicalLineage(t *testing.T) {
	trace, err := BuildCanonicalSyntheticTrace(TraceAcknowledgementAccept)
	if err != nil {
		t.Fatalf("BuildCanonicalSyntheticTrace() error = %v", err)
	}
	trace.Records[0].RecordSHA256 = traceDigest(trace.Scenario, "tampered")
	if _, err := ReplaySyntheticTrace(trace); !errors.Is(err, ErrSyntheticTraceReplay) {
		t.Fatalf("ReplaySyntheticTrace(tampered) error = %v, want %v", err, ErrSyntheticTraceReplay)
	}

	trace, err = BuildCanonicalSyntheticTrace(TraceOperationResumeAccept)
	if err != nil {
		t.Fatalf("BuildCanonicalSyntheticTrace(operation) error = %v", err)
	}
	trace.Checkpoints[0].CheckpointSHA256 = traceDigest(trace.Scenario, "tampered-checkpoint")
	if _, err := ReplaySyntheticTrace(trace); !errors.Is(err, ErrSyntheticTraceReplay) {
		t.Fatalf("ReplaySyntheticTrace(tampered checkpoint) error = %v, want %v", err, ErrSyntheticTraceReplay)
	}
}
