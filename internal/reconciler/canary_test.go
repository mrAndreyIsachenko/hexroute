package reconciler

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActionRecordsRejectSecretTopologyAndSessionCanaries(t *testing.T) {
	for _, canary := range actionCanaries(t) {
		t.Run(canary, func(t *testing.T) {
			_, err := NewActionRecord(testProvenance(RecordReadiness), ReadinessRecord{
				Target: canary, Status: ReadinessDenied, Reason: ReasonTarget, RetryClass: RetryNever,
			})
			if !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("NewActionRecord() error = %v, want %v", err, ErrInvalidContract)
			}
		})
	}
}

func TestActionRecordProjectionDoesNotContainProtectedFields(t *testing.T) {
	record, err := NewActionRecord(testProvenance(RecordOutcome), OutcomeRecord{
		ActionID: testActionID, AttemptID: testAttemptID, Outcome: OutcomeSafeMode,
		Reason: ReasonSafeMode, ReportDelivery: ReportPending,
	})
	if err != nil {
		t.Fatalf("NewActionRecord() error = %v", err)
	}
	encoded, _, err := EncodeActionRecord(record)
	if err != nil {
		t.Fatalf("EncodeActionRecord() error = %v", err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"command", "argument", "credential", "endpoint", "keychain", "path",
		"process_output", "selector", "session_id", "topology", "totp",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("encoded action record leaked forbidden field fragment %q: %s", forbidden, encoded)
		}
	}
}

func actionCanaries(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reconciler", "v1", "action-contract-rejections.json"))
	if err != nil {
		t.Fatalf("read rejection fixture: %v", err)
	}
	var fixture struct {
		Canaries []string `json:"canaries"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode rejection fixture: %v", err)
	}
	if len(fixture.Canaries) == 0 {
		t.Fatal("empty canary fixture")
	}
	for _, value := range repositorySecretCanaries(t) {
		fixture.Canaries = append(fixture.Canaries, value)
	}
	return fixture.Canaries
}

func repositorySecretCanaries(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "secrets", "v1", "canaries.json"))
	if err != nil {
		t.Fatalf("read secret canary fixture: %v", err)
	}
	var fixture struct {
		Canaries []struct {
			Value string `json:"value"`
		} `json:"canaries"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode secret canary fixture: %v", err)
	}
	values := make([]string, 0, len(fixture.Canaries))
	for _, canary := range fixture.Canaries {
		values = append(values, canary.Value)
	}
	return values
}
