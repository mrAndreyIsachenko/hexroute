package reconciler

import "testing"

func TestCanonicalSyntheticTraceCatalogCoversMandatoryTask101Scenarios(t *testing.T) {
	traces, err := CanonicalSyntheticTraces()
	if err != nil {
		t.Fatalf("CanonicalSyntheticTraces() error = %v", err)
	}
	wantScenarios := []SyntheticTraceScenario{
		TraceNoop,
		TraceAcknowledgementAccept,
		TraceAcknowledgementBlock,
		TraceAcknowledgementDeny,
		TraceOperationResumeAccept,
		TraceOperationResumeReject,
		TraceExpiry,
		TraceCancellationBeforeApply,
		TraceCancellationAfterApply,
		TraceCompensation,
		TraceGenerationChange,
	}
	seen := make(map[SyntheticTraceScenario]string, len(traces))
	ackClasses := make(map[AckClass]bool)
	for _, trace := range traces {
		if err := trace.Validate(); err != nil {
			t.Fatalf("%s Validate() error = %v", trace.Scenario, err)
		}
		if existing := seen[trace.Scenario]; existing != "" {
			t.Fatalf("duplicate scenario %s", trace.Scenario)
		}
		seen[trace.Scenario] = trace.TraceSHA256
		assertNoLiveFixtureValues(t, trace)
		for _, record := range trace.Records {
			encoded, digest, err := EncodeActionRecord(record)
			if err != nil {
				t.Fatalf("%s EncodeActionRecord(%s) error = %v", trace.Scenario, record.Provenance.Kind, err)
			}
			decoded, err := DecodeActionRecord(encoded, record.Provenance.Kind)
			if err != nil {
				t.Fatalf("%s DecodeActionRecord(%s) error = %v", trace.Scenario, record.Provenance.Kind, err)
			}
			if decoded.RecordSHA256 != digest || digest != record.RecordSHA256 {
				t.Fatalf("%s record digest changed: decoded=%s encoded=%s record=%s", trace.Scenario, decoded.RecordSHA256, digest, record.RecordSHA256)
			}
		}
		if trace.Expected.Acknowledgement != "" {
			ackClasses[trace.Expected.Acknowledgement] = true
		}
	}
	for _, scenario := range wantScenarios {
		if seen[scenario] == "" {
			t.Fatalf("task 10.1 scenario %s not covered", scenario)
		}
	}
	for _, class := range []AckClass{AckAccepted, AckTemporarilyRejected, AckDenied} {
		if !ackClasses[class] {
			t.Fatalf("acknowledgement class %s not covered", class)
		}
	}
	again, err := CanonicalSyntheticTraces()
	if err != nil {
		t.Fatalf("CanonicalSyntheticTraces(second) error = %v", err)
	}
	for index, trace := range again {
		if trace.TraceSHA256 != traces[index].TraceSHA256 {
			t.Fatalf("%s trace digest changed: %s != %s", trace.Scenario, trace.TraceSHA256, traces[index].TraceSHA256)
		}
	}
}

func TestCanonicalSyntheticTraceCatalogCoversMandatoryTask102Scenarios(t *testing.T) {
	traces, err := CanonicalSyntheticTraces()
	if err != nil {
		t.Fatalf("CanonicalSyntheticTraces() error = %v", err)
	}
	byScenario := make(map[SyntheticTraceScenario]SyntheticTrace, len(traces))
	for _, trace := range traces {
		byScenario[trace.Scenario] = trace
	}
	for _, test := range []struct {
		scenario SyntheticTraceScenario
		reason   Reason
		attempt  AttemptState
		outcome  TerminalOutcome
		recovery StartupRecoveryClass
		hasDiff  bool
	}{
		{scenario: TraceCrashAfterClaim, reason: ReasonAccepted, attempt: AttemptClaimed, recovery: RecoveryUntouched},
		{scenario: TraceCrashAfterApply, reason: ReasonSafeMode, attempt: AttemptRunning, recovery: RecoverySafeMode},
		{scenario: TraceVerificationMismatch, reason: ReasonVerification, attempt: AttemptFailed, outcome: OutcomeFailed},
		{scenario: TraceMissingStateRehydrate, reason: ReasonAccepted, hasDiff: true},
		{scenario: TraceForeignConflict, reason: ReasonOwnership, hasDiff: true},
	} {
		trace, ok := byScenario[test.scenario]
		if !ok {
			t.Fatalf("missing task 10.2 trace %s", test.scenario)
		}
		if trace.Expected.Reason != test.reason ||
			trace.Expected.AttemptState != test.attempt ||
			trace.Expected.TerminalOutcome != test.outcome {
			t.Fatalf("%s expectation = %+v", test.scenario, trace.Expected)
		}
		if test.recovery != "" {
			if trace.StartupRecovery == nil || trace.StartupRecovery.Class != test.recovery {
				t.Fatalf("%s startup recovery = %+v", test.scenario, trace.StartupRecovery)
			}
		}
		if test.hasDiff && trace.SyntheticDiff == nil {
			t.Fatalf("%s missing synthetic diff", test.scenario)
		}
	}
}

func TestMandatorySyntheticTraceExpectationsMatchTask101Semantics(t *testing.T) {
	traces, err := CanonicalSyntheticTraces()
	if err != nil {
		t.Fatalf("CanonicalSyntheticTraces() error = %v", err)
	}
	byScenario := make(map[SyntheticTraceScenario]SyntheticTrace, len(traces))
	for _, trace := range traces {
		byScenario[trace.Scenario] = trace
	}
	for _, test := range []struct {
		scenario       SyntheticTraceScenario
		ack            AckClass
		reason         Reason
		attempt        AttemptState
		outcome        TerminalOutcome
		lifecycle      SessionLifecycle
		unchanged      bool
		resumeAccepted *bool
	}{
		{scenario: TraceNoop, ack: AckAccepted, reason: ReasonNoAction, unchanged: true},
		{scenario: TraceAcknowledgementAccept, ack: AckAccepted, reason: ReasonAccepted, attempt: AttemptCommitted, outcome: OutcomeCommitted},
		{scenario: TraceAcknowledgementBlock, ack: AckTemporarilyRejected, reason: ReasonCooldown, unchanged: true},
		{scenario: TraceAcknowledgementDeny, ack: AckDenied, reason: ReasonPolicy, unchanged: true},
		{scenario: TraceOperationResumeAccept, reason: ReasonAccepted, lifecycle: SessionRunning, resumeAccepted: boolPtr(true)},
		{scenario: TraceOperationResumeReject, reason: ReasonLineage, lifecycle: SessionSuspended, unchanged: true, resumeAccepted: boolPtr(false)},
		{scenario: TraceExpiry, reason: ReasonExpired, attempt: AttemptExpired, outcome: OutcomeExpired},
		{scenario: TraceCancellationBeforeApply, reason: ReasonCancelled, attempt: AttemptCancelled, outcome: OutcomeCancelled, unchanged: true},
		{scenario: TraceCancellationAfterApply, reason: ReasonCompensation, attempt: AttemptRolledBack, outcome: OutcomeRolledBack},
		{scenario: TraceCompensation, reason: ReasonCompensation, attempt: AttemptRolledBack, outcome: OutcomeRolledBack},
		{scenario: TraceGenerationChange, ack: AckDenied, reason: ReasonGeneration, unchanged: true},
	} {
		trace, ok := byScenario[test.scenario]
		if !ok {
			t.Fatalf("missing trace %s", test.scenario)
		}
		if trace.Expected.Acknowledgement != test.ack ||
			trace.Expected.Reason != test.reason ||
			trace.Expected.AttemptState != test.attempt ||
			trace.Expected.TerminalOutcome != test.outcome ||
			trace.Expected.SessionLifecycle != test.lifecycle ||
			trace.Expected.ActionStateUnchanged != test.unchanged {
			t.Fatalf("%s expectation = %+v", test.scenario, trace.Expected)
		}
		if test.resumeAccepted != nil {
			if trace.ResumeDecision == nil || trace.ResumeDecision.Accepted != *test.resumeAccepted {
				t.Fatalf("%s resume decision = %+v", test.scenario, trace.ResumeDecision)
			}
		}
	}
}

func boolPtr(value bool) *bool {
	return &value
}
