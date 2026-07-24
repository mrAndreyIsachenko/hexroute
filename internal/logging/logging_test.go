package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesStructuredAllowlistedEvent(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, ComponentDaemon)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.now = func() time.Time {
		return time.Date(2026, time.July, 23, 12, 30, 0, 0, time.UTC)
	}

	if err := logger.Emit(LevelInfo, EventStartupCheck, ResultOK, ""); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("log is not valid JSON: %v", err)
	}
	want := map[string]string{
		"schema":             Schema,
		"timestamp":          "2026-07-23T12:30:00Z",
		"level":              "info",
		"component":          "hexrouted",
		"event":              "startup_check",
		"result":             "ok",
		"mode":               "observe-only",
		"mutation_authority": "none",
	}
	for field, value := range want {
		if record[field] != value {
			t.Fatalf("record[%q] = %#v, want %q", field, record[field], value)
		}
	}
	if _, exists := record["reason"]; exists {
		t.Fatal("successful event unexpectedly contains reason")
	}
}

func TestLoggerRejectsNonAllowlistedValuesWithoutWriting(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, ComponentDaemon)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name   string
		level  Level
		event  EventName
		result Result
		reason Reason
	}{
		{name: "level", level: "debug-secret", event: EventCommandStatus, result: ResultOK},
		{name: "event", level: LevelInfo, event: "raw-message", result: ResultOK},
		{name: "result", level: LevelInfo, event: EventCommandStatus, result: "token-value"},
		{name: "reason", level: LevelWarn, event: EventArgumentRejected, result: ResultRejected, reason: "raw-error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output.Reset()
			if err := logger.Emit(test.level, test.event, test.result, test.reason); err == nil {
				t.Fatal("Emit() error = nil, want rejection")
			}
			if output.Len() != 0 {
				t.Fatalf("rejected event wrote %q", output.String())
			}
		})
	}
}

func TestCloudLoggerDeclaresTelemetryOnlyAuthority(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, ComponentIngest)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := logger.Emit(
		LevelInfo,
		EventCloudAPIStarted,
		ResultOK,
		"",
	); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if record["mode"] != "telemetry-only" ||
		record["mutation_authority"] != "none" {
		t.Fatalf("cloud authority fields = %+v", record)
	}
}

func TestSecretCanariesCannotEnterLoggerFields(t *testing.T) {
	canaries := loadCanaries(t)
	for _, canary := range canaries {
		t.Run(canary, func(t *testing.T) {
			var output bytes.Buffer
			if _, err := New(&output, Component(canary)); err == nil {
				t.Fatal("New() accepted canary component")
			}

			logger, err := New(&output, ComponentDaemon)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			attempts := []func() error{
				func() error { return logger.Emit(Level(canary), EventCommandStatus, ResultOK, "") },
				func() error { return logger.Emit(LevelInfo, EventName(canary), ResultOK, "") },
				func() error { return logger.Emit(LevelInfo, EventCommandStatus, Result(canary), "") },
				func() error {
					return logger.Emit(LevelWarn, EventArgumentRejected, ResultRejected, Reason(canary))
				},
			}
			for _, attempt := range attempts {
				if err := attempt(); err == nil {
					t.Fatal("Emit() accepted a secret canary")
				}
			}
			if strings.Contains(output.String(), canary) {
				t.Fatalf("logger leaked canary %q", canary)
			}
		})
	}
}

func loadCanaries(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "secrets", "v1", "canaries.json"))
	if err != nil {
		t.Fatalf("read canaries: %v", err)
	}
	var fixture struct {
		Canaries []struct {
			Value string `json:"value"`
		} `json:"canaries"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode canaries: %v", err)
	}
	values := make([]string, 0, len(fixture.Canaries))
	for _, canary := range fixture.Canaries {
		values = append(values, canary.Value)
	}
	return values
}
