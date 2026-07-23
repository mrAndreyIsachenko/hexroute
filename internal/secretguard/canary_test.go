package secretguard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/spool"
	"github.com/mrAndreyIsachenko/hexroute/internal/telemetry"
)

func TestRepositorySecretCanariesCannotReachSerializedOutputs(t *testing.T) {
	canaries := loadCanaries(t)
	for _, canary := range canaries {
		t.Run(canary, func(t *testing.T) {
			var outputs bytes.Buffer

			encodedEvent, eventErr := event.Encode(event.SchemaDeployment, event.Deployment{
				DeploymentID: canary,
				Release:      "v0.1.0",
				Status:       event.DeploymentStaged,
				DigestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			})
			if eventErr == nil {
				outputs.Write(encodedEvent)
				t.Fatal("event serializer accepted secret canary")
			}

			logger, err := logging.New(&outputs, logging.ComponentDaemon)
			if err != nil {
				t.Fatalf("logging.New() error = %v", err)
			}
			if err := logger.Emit(
				logging.LevelWarn,
				logging.EventArgumentRejected,
				logging.ResultRejected,
				logging.Reason(canary),
			); err == nil {
				t.Fatal("logger accepted secret canary")
			}

			entry := spool.Entry{
				Sequence: 1,
				Priority: event.PriorityCritical,
				Metadata: metadata.Metadata{
					EventID:        "22222222-2222-4222-8222-222222222222",
					NodeID:         "11111111-1111-4111-8111-111111111111",
					SessionID:      "33333333-3333-4333-8333-333333333333",
					Sequence:       1,
					WallClock:      time.Date(2026, time.July, 23, 20, 0, 0, 0, time.UTC),
					MonotonicNanos: 1,
				},
				Event: []byte(`{"schema":"raw.log","value":` + quoted(canary) + `}`),
			}
			encodedBundle, bundleErr := telemetry.EncodeIncidentBundle(
				"incident-01",
				time.Date(2026, time.July, 23, 20, 0, 0, 0, time.UTC),
				[]spool.Entry{entry},
			)
			if bundleErr == nil {
				outputs.Write(encodedBundle)
				t.Fatal("incident bundle accepted secret canary")
			}

			if strings.Contains(outputs.String(), canary) {
				t.Fatalf("serialized output leaked canary %q", canary)
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

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
