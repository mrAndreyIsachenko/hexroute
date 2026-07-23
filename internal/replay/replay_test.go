package replay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedTracesMatchApprovedRootBehavior(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "traces", "v1", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob() error: %v", err)
	}
	if len(paths) != 7 {
		t.Fatalf("trace count = %d, want 7", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open() error: %v", err)
			}
			defer file.Close()

			trace, err := Decode(file)
			if err != nil {
				t.Fatalf("Decode() error: %v", err)
			}
			if err := CompareRoot(trace); err != nil {
				t.Fatalf("CompareRoot() error: %v", err)
			}
		})
	}
}

func TestRootPlannerRejectsRestartWithoutObservedExit(t *testing.T) {
	trace := Trace{
		Name: "unsafe-restart",
		Events: []Event{{
			Schema:    TraceSchema,
			Trace:     "unsafe-restart",
			Sequence:  1,
			Component: ComponentRoot,
			Kind:      KindDecision,
			State:     StateRecovering,
			Reason:    "process_exit_recovery_allowed",
			Action:    ActionRestartSingBox,
		}},
	}

	if err := CompareRoot(trace); !errors.Is(err, ErrDecisionDiverged) {
		t.Fatalf("CompareRoot() error = %v, want %v", err, ErrDecisionDiverged)
	}
}

func TestRootPlannerRejectsFailoverBeforeThreshold(t *testing.T) {
	trace := Trace{
		Name: "unsafe-failover",
		Events: []Event{{
			Schema:    TraceSchema,
			Trace:     "unsafe-failover",
			Sequence:  1,
			Component: ComponentRoot,
			Kind:      KindDecision,
			State:     StateRecovering,
			Reason:    "alternate_ingress_ready",
			Action:    ActionSelectNextIngress,
		}},
	}

	if err := CompareRoot(trace); !errors.Is(err, ErrDecisionDiverged) {
		t.Fatalf("CompareRoot() error = %v, want %v", err, ErrDecisionDiverged)
	}
}

func TestRootPlannerNeverCapturesPritunlReconnect(t *testing.T) {
	planner := &RootPlanner{}
	event := Event{
		Component: ComponentUser,
		Kind:      KindDecision,
		State:     StateRecovering,
		Reason:    "reconnect_threshold_reached",
		Action:    ActionReconnectPritunl,
	}

	action, err := planner.Step(event)
	if err != nil {
		t.Fatalf("Step() error: %v", err)
	}
	if action != ActionNone {
		t.Fatalf("root action = %q, want none", action)
	}
}

func TestDecodeRejectsUnknownActionAndFields(t *testing.T) {
	fixtures := []string{
		`{"schema":"hexroute.trace-event.v1","trace":"bad","seq":1,"offset_ms":0,"component":"root","kind":"decision","state":"RECOVERING","reason":"bad","action":"restart_adguard"}`,
		`{"schema":"hexroute.trace-event.v1","trace":"bad","seq":1,"offset_ms":0,"component":"root","kind":"decision","state":"RECOVERING","reason":"bad","action":"none","command":"arbitrary"}`,
	}
	for _, fixture := range fixtures {
		if _, err := Decode(strings.NewReader(fixture)); !errors.Is(err, ErrInvalidTrace) {
			t.Fatalf("Decode() error = %v, want %v", err, ErrInvalidTrace)
		}
	}
}
