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
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
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

			_, policyEventErr := event.Encode(event.SchemaPolicy, event.PolicyLifecycle{
				Status: policy.Status{
					Schema: policy.PolicyStatusSchema, Domain: policy.DomainRoot,
					State: policy.PolicyActive, BundleGeneration: 7,
					PolicyGeneration: 5, ManifestSHA256: canary,
					ActivatedAt: "2030-01-01T00:00:00Z", Reason: policy.ReasonNone,
				},
				AuthorizationSuspension: policy.AuthorizationSuspension{
					Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
				},
			})
			if policyEventErr == nil {
				t.Fatal("policy telemetry accepted secret canary")
			}

			// The connectivity projection is the read model's one path off the
			// host. Its own package asserts an allowlist over field names; this
			// asserts the complementary property, that no bounded field will
			// carry a value it was not meant to hold. Without it a field added
			// to the projection stays green under the repository's secret gate.
			// Without this the loop below would pass vacuously if the base
			// payload ever stopped being encodable at all.
			if _, err := event.Encode(
				event.SchemaConnectivityProjection, connectivityProjectionBase(),
			); err != nil {
				t.Fatalf("the projection fixture no longer encodes: %v", err)
			}
			for _, offered := range connectivityProjections(canary) {
				if _, err := event.Encode(
					event.SchemaConnectivityProjection, offered.payload,
				); err == nil {
					t.Fatalf("connectivity projection accepted secret canary in %s",
						offered.field)
				}
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

// connectivityProjectionBase is a projection that must encode cleanly.
func connectivityProjectionBase() event.ConnectivityProjection {
	return event.ConnectivityProjection{
		SnapshotGeneration: 4, ReducerVersion: 2,
		BundleGeneration: 7, RootGeneration: 3, UserGeneration: 2,
		Aggregate: "ready", Authorization: "authorized",
		AuthorizationReason: "none",
		Components: []event.ProjectedComponent{{
			Component: "relays", State: "ready",
			Freshness: event.FreshnessFresh, Reason: "none",
		}},
		ProposalClasses: []event.ProjectedProposalClass{{Class: "observe", Count: 1}},
	}
}

// connectivityProjections offers the canary through every string-bearing field
// of the projection, one at a time, so a new field cannot quietly become the
// one that is never tried.
func connectivityProjections(canary string) []struct {
	field   string
	payload event.ConnectivityProjection
} {
	base := connectivityProjectionBase
	offered := make([]struct {
		field   string
		payload event.ConnectivityProjection
	}, 0, 7)
	add := func(field string, mutate func(*event.ConnectivityProjection)) {
		payload := base()
		mutate(&payload)
		offered = append(offered, struct {
			field   string
			payload event.ConnectivityProjection
		}{field: field, payload: payload})
	}
	add("aggregate", func(p *event.ConnectivityProjection) { p.Aggregate = canary })
	add("authorization", func(p *event.ConnectivityProjection) { p.Authorization = canary })
	add("authorization_reason", func(p *event.ConnectivityProjection) {
		p.AuthorizationReason = canary
	})
	add("components[].component", func(p *event.ConnectivityProjection) {
		p.Components[0].Component = canary
	})
	add("components[].state", func(p *event.ConnectivityProjection) {
		p.Components[0].State = canary
	})
	add("components[].reason", func(p *event.ConnectivityProjection) {
		p.Components[0].Reason = canary
	})
	add("proposal_classes[].class", func(p *event.ConnectivityProjection) {
		p.ProposalClasses[0].Class = canary
	})
	return offered
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
