package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunDefaultsToObserveOnly(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run("hexrouted", nil, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var record map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("Run() output is not JSON: %v", err)
	}
	want := map[string]string{
		"schema":             "hexroute.log.v1",
		"component":          "hexrouted",
		"event":              "command_status",
		"result":             "skeleton",
		"mode":               "observe-only",
		"mutation_authority": "none",
	}
	for field, value := range want {
		if record[field] != value {
			t.Fatalf("record[%q] = %#v, want %q", field, record[field], value)
		}
	}
}

func TestRunRejectsArgumentsWithoutEchoingThem(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	canary := "HEXROUTE_CANARY_REALITY_PRIVATE_KEY"

	code := Run("hexrouted", []string{"restart", canary}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), canary) {
		t.Fatalf("Run() leaked rejected argument %q", canary)
	}
	var record map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &record); err != nil {
		t.Fatalf("Run() error output is not JSON: %v", err)
	}
	if record["reason"] != "unexpected_arguments" {
		t.Fatalf("reason = %#v, want unexpected_arguments", record["reason"])
	}
}

func TestRunRejectsUnknownFlagWithoutEchoingIt(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	canary := "HEXROUTE_CANARY_VLESS_NOT_UUID"

	code := Run("hexroute-userd", []string{"--" + canary}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), canary) {
		t.Fatalf("Run() leaked rejected flag %q", canary)
	}
	if !strings.Contains(stderr.String(), `"reason":"invalid_flags"`) {
		t.Fatalf("Run() error output = %q, want generic invalid_flags reason", stderr.String())
	}
}

func TestEveryCommandUsesSharedLogger(t *testing.T) {
	names := []string{
		"hexrouted",
		"hexroute-userd",
		"hexroutectl",
		"hexroute-sentinel",
		"hexroute-ingest",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := Run(name, []string{"--check"}, &stdout, &stderr); code != 0 {
				t.Fatalf("Run() code = %d; stderr=%q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), `"schema":"hexroute.log.v1"`) {
				t.Fatalf("Run() output = %q, want shared structured logger", stdout.String())
			}
		})
	}
}
