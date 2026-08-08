package event

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRegisteredSchemasRoundTrip(t *testing.T) {
	tests := []struct {
		schema  Schema
		payload any
	}{
		{
			schema: SchemaObservation,
			payload: Observation{
				Component: control.ComponentTunnel,
				Health:    control.HealthReady,
				Reason:    control.ReasonProbeSucceeded,
			},
		},
		{
			schema: SchemaTransition,
			payload: Transition{
				Component:  control.ComponentTunnel,
				From:       control.StateRecovering,
				To:         control.StateHealthy,
				Reason:     control.ReasonVerificationPassed,
				Generation: 4,
			},
		},
		{
			schema: SchemaTransition,
			payload: Transition{
				Component:  control.ComponentPritunl,
				From:       control.StateSafeMode,
				To:         control.StateDegraded,
				Reason:     control.ReasonOperatorResume,
				Generation: 5,
			},
		},
		{
			schema: SchemaAction,
			payload: Action{
				Kind:       control.ActionRestart,
				Target:     control.TargetSingBox,
				Outcome:    ActionVerified,
				Attempt:    1,
				Generation: 4,
				Reason:     control.ReasonRecoveryAllowed,
			},
		},
		{
			schema: SchemaIncident,
			payload: Incident{
				IncidentID: "inc-01",
				Status:     IncidentOpened,
				Severity:   SeverityCritical,
				Category:   IncidentSpoolOverflow,
				Component:  control.ComponentTunnel,
				Generation: 4,
			},
		},
		{
			schema: SchemaDeployment,
			payload: Deployment{
				DeploymentID: "deploy-01",
				Release:      "v0.1.0",
				Status:       DeploymentActivated,
				DigestSHA256: testDigest,
			},
		},
		{
			schema: SchemaConfigVersion,
			payload: ConfigVersion{
				ConfigID:     "config-01",
				Version:      "2026.07.23",
				Status:       ConfigActivated,
				DigestSHA256: testDigest,
			},
		},
		{
			schema: SchemaDiagnostic,
			payload: Diagnostic{
				Component:  control.ComponentRuntime,
				Code:       DiagnosticRetryScheduled,
				Count:      2,
				DurationMS: 500,
			},
		},
		{
			schema: SchemaSleep,
			payload: Sleep{
				Phase:  SleepStarted,
				Reason: SleepReasonLidClosed,
			},
		},
		{
			schema: SchemaPolicy,
			payload: PolicyLifecycle{
				Status: policy.Status{
					Schema: policy.PolicyStatusSchema, Domain: policy.DomainRoot,
					State: policy.PolicyActive, BundleGeneration: 7,
					PolicyGeneration: 5, ManifestSHA256: testDigest,
					ActivatedAt: "2030-01-01T00:00:00Z", Reason: policy.ReasonNone,
				},
				AuthorizationSuspension: policy.AuthorizationSuspension{
					Schema: policy.AuthorizationSuspensionSchema, Reason: policy.ReasonNone,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(string(test.schema), func(t *testing.T) {
			encoded, err := Encode(test.schema, test.payload)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			record, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			definition, _ := DefinitionFor(test.schema)
			if record.Schema != test.schema || record.Version != SchemaVersion ||
				record.Priority != definition.Priority {
				t.Fatalf("Decode() metadata = %+v", record)
			}
		})
	}
}

func TestDecodeRejectsUnknownFieldsVersionsPrioritiesAndSchemas(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{
			name: "outer unknown field",
			data: `{"schema":"component.observation","version":1,"priority":"operational","payload":{},"raw":"secret"}`,
			want: ErrMalformedEvent,
		},
		{
			name: "payload unknown field",
			data: `{"schema":"component.observation","version":1,"priority":"operational","payload":{"component":"tunnel","health":"ready","reason":"probe_succeeded","consecutive_failures":0,"raw":"secret"}}`,
			want: ErrMalformedEvent,
		},
		{
			name: "unknown schema",
			data: `{"schema":"raw.log","version":1,"priority":"diagnostic","payload":{}}`,
			want: ErrUnknownSchema,
		},
		{
			name: "unsupported version",
			data: `{"schema":"component.observation","version":2,"priority":"operational","payload":{}}`,
			want: ErrUnsupportedVersion,
		},
		{
			name: "priority mismatch",
			data: `{"schema":"state.transition","version":1,"priority":"diagnostic","payload":{}}`,
			want: ErrPriorityMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.data))
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEncodeRejectsMismatchedAndOversizedFields(t *testing.T) {
	if _, err := Encode(SchemaObservation, Deployment{}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("Encode(mismatched payload) error = %v, want %v", err, ErrInvalidField)
	}

	payload := Deployment{
		DeploymentID: strings.Repeat("a", MaxReferenceBytes+1),
		Release:      "v0.1.0",
		Status:       DeploymentStaged,
		DigestSHA256: testDigest,
	}
	if _, err := Encode(SchemaDeployment, payload); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("Encode(oversized field) error = %v, want %v", err, ErrInvalidField)
	}

	oversized := make([]byte, MaxEncodedEventBytes+1)
	if _, err := Decode(oversized); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("Decode(oversized event) error = %v, want %v", err, ErrEventTooLarge)
	}

	if _, err := Encode(SchemaIncident, Incident{
		IncidentID: "incident-zero-generation",
		Status:     IncidentOpened,
		Severity:   SeverityCritical,
		Category:   IncidentAvailability,
		Component:  control.ComponentTunnel,
	}); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("zero-generation incident error = %v, want %v", err, ErrInvalidField)
	}
}

func TestEverySchemaHasFixedVersionPriorityAndPayloadLimit(t *testing.T) {
	schemas := []Schema{
		SchemaObservation,
		SchemaTransition,
		SchemaAction,
		SchemaIncident,
		SchemaDeployment,
		SchemaConfigVersion,
		SchemaDiagnostic,
		SchemaSleep,
		SchemaPolicy,
	}
	for _, schema := range schemas {
		definition, ok := DefinitionFor(schema)
		if !ok {
			t.Fatalf("DefinitionFor(%q) not found", schema)
		}
		if definition.Version != SchemaVersion || definition.MaxPayloadBytes != MaxPayloadBytes {
			t.Fatalf("DefinitionFor(%q) = %+v", schema, definition)
		}
		if definition.Priority != PriorityCritical &&
			definition.Priority != PriorityOperational &&
			definition.Priority != PriorityDiagnostic {
			t.Fatalf("DefinitionFor(%q) has invalid priority %q", schema, definition.Priority)
		}
	}
}

func TestPolicyLifecycleRejectsUnallowlistedFields(t *testing.T) {
	for _, field := range []string{
		"selector", "endpoint", "source_path", "lease", "credential_reference", "credential_value",
	} {
		encoded := `{"schema":"policy.lifecycle","version":1,"priority":"critical",` +
			`"payload":{"status":{"schema":"hexroute.policy-status.v1","domain":"root",` +
			`"state":"active","bundle_generation":7,"policy_generation":5,` +
			`"manifest_sha256":"` + testDigest + `","activated_at":"2030-01-01T00:00:00Z",` +
			`"reason":"none"},"authorization_suspension":{"schema":` +
			`"hexroute.authorization-suspension.v1","suspended":false,"reason":"none"},` +
			`"` + field + `":"forbidden"}}`
		if _, err := Decode([]byte(encoded)); !errors.Is(err, ErrMalformedEvent) {
			t.Fatalf("field %q error = %v, want %v", field, err, ErrMalformedEvent)
		}
	}
}

func TestSleepSchemaRejectsAmbiguousTransitions(t *testing.T) {
	for _, payload := range []Sleep{
		{Phase: SleepStarted, Reason: SleepReasonFullWake},
		{Phase: SleepEnded, Reason: SleepReasonLidClosed},
		{Phase: "unknown", Reason: SleepReasonSystemSleep},
	} {
		if _, err := Encode(SchemaSleep, payload); !errors.Is(err, ErrInvalidField) {
			t.Fatalf("Encode(%+v) error = %v, want %v", payload, err, ErrInvalidField)
		}
	}
}

func TestWirePayloadIsAnObject(t *testing.T) {
	encoded, err := Encode(SchemaObservation, Observation{
		Component: control.ComponentNetwork,
		Health:    control.HealthReady,
		Reason:    control.ReasonDependenciesReady,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode wire JSON: %v", err)
	}
	if len(wire["payload"]) == 0 || wire["payload"][0] != '{' {
		t.Fatalf("payload = %s, want JSON object", wire["payload"])
	}
}
