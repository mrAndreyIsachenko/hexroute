package connectivity

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestFixtureBaselinesValidateAndRoundTrip(t *testing.T) {
	for _, fact := range FixtureBaselineSet() {
		encoded, err := Encode(fact)
		if err != nil {
			t.Fatalf("encode %s: %v", fact.Component, err)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", fact.Component, err)
		}
		reencoded, err := Encode(decoded)
		if err != nil {
			t.Fatalf("re-encode %s: %v", fact.Component, err)
		}
		if string(encoded) != string(reencoded) {
			t.Fatalf("%s: round trip is not canonical", fact.Component)
		}
	}
}

func TestEveryComponentHasAFixture(t *testing.T) {
	payloads := FixturePayloads()
	for _, component := range Components() {
		payload, ok := payloads[component]
		if !ok {
			t.Fatalf("component %s has no fixture payload", component)
		}
		named, single := payload.component()
		if !single || named != component {
			t.Fatalf("component %s fixture payload describes %s", component, named)
		}
	}
}

func TestDecodeRejectsMalformedEncodings(t *testing.T) {
	valid, err := Encode(FixtureBaseline(ComponentDNS, 1))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	tests := []struct {
		name    string
		encoded []byte
		want    error
	}{
		{"empty", nil, ErrInvalidEncoding},
		{"trailing data", append(append([]byte{}, valid...), '{', '}'), ErrInvalidEncoding},
		{
			name:    "unknown field",
			encoded: []byte(strings.Replace(string(valid), `"baseline"`, `"unexpected_field":1,"baseline"`, 1)),
			want:    ErrInvalidEncoding,
		},
		{
			name:    "oversized",
			encoded: append(append([]byte{}, valid...), make([]byte, MaxEncodedFactBytes)...),
			want:    ErrInvalidEncoding,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(test.encoded); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

// A persisted fact is deduplicated by its digest, so an encoding that differs
// only in whitespace or key order must not be accepted as a second spelling of
// the same fact.
func TestDecodeRejectsNonCanonicalEncoding(t *testing.T) {
	fact := FixtureBaseline(ComponentRelays, 3)
	loose, err := json.MarshalIndent(fact, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := Decode(loose); !errors.Is(err, ErrNotCanonical) {
		t.Fatalf("got %v, want %v", err, ErrNotCanonical)
	}
}

func TestValidateRejectsForbiddenIdentifiers(t *testing.T) {
	// Each of these is a command, a path or a quoting trick wearing an
	// identifier's clothes.
	forbidden := []string{
		"/usr/bin/env",
		"../../etc/passwd",
		"root.network; rm -rf /",
		"root network",
		"root.network\n",
		"root.network|cat",
		"$(whoami)",
		"`id`",
		"root..network",
		".hidden",
		strings.Repeat("a", MaxIdentifierBytes+1),
		"",
	}
	for _, value := range forbidden {
		t.Run(value, func(t *testing.T) {
			fact := FixtureBaseline(ComponentDNS, 1)
			fact.SourceID = SourceID(value)
			if err := Validate(fact); err == nil {
				t.Fatalf("source id %q was accepted", value)
			}
			fact = FixtureBaseline(ComponentDNS, 1)
			fact.BootID = value
			if err := Validate(fact); err == nil {
				t.Fatalf("boot id %q was accepted", value)
			}
		})
	}
}

// The payload types have no field that can hold a credential, a command or a
// path. This test states that as a property of the encoding rather than of the
// current field list, so adding such a field later fails here.
func TestEncodedFactCarriesNoFreeFormStrings(t *testing.T) {
	allowed := map[string]struct{}{
		"schema": {}, "event_id": {}, "domain": {}, "component": {},
		"source_id": {}, "boot_id": {}, "observed_at": {},
		"lifecycle": {}, "reason": {},
	}
	for _, fact := range FixtureBaselineSet() {
		encoded, err := Encode(fact)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		var generic map[string]any
		if err := json.Unmarshal(encoded, &generic); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		payload, ok := generic["payload"].(map[string]any)
		if !ok {
			t.Fatalf("%s: payload is not an object", fact.Component)
		}
		for _, member := range payload {
			fields, ok := member.(map[string]any)
			if !ok {
				t.Fatalf("%s: payload member is not an object", fact.Component)
			}
			for name, value := range fields {
				if _, isString := value.(string); !isString {
					continue
				}
				// A payload string must be an enumeration member, never text.
				if !validIdentifier(value.(string)) {
					t.Fatalf("%s: payload field %q holds free-form text %q",
						fact.Component, name, value)
				}
			}
		}
		for name, value := range generic {
			if _, isString := value.(string); !isString {
				continue
			}
			if _, permitted := allowed[name]; !permitted {
				t.Fatalf("%s: unexpected top-level string field %q", fact.Component, name)
			}
		}
	}
}

func TestValidateRejectsIncoherentFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Fact)
	}{
		{"wrong schema", func(f *Fact) { f.Schema = "hexroute.other.v1" }},
		{"wrong version", func(f *Fact) { f.Version = FactSchemaVersion + 1 }},
		{"non-uuid event", func(f *Fact) { f.EventID = "not-a-uuid" }},
		{"uppercase uuid", func(f *Fact) { f.EventID = "00000000-0000-4000-8000-0000000000AB" }},
		{"uuid with separator moved", func(f *Fact) { f.EventID = "000000000-000-4000-8000-000000000001" }},
		{"unknown domain", func(f *Fact) { f.Domain = policy.Domain("other") }},
		{"unknown component", func(f *Fact) { f.Component = Component("wifi") }},
		{"zero sequence", func(f *Fact) { f.SourceSequence = 0 }},
		{"zero observed at", func(f *Fact) { f.ObservedAt = time.Time{} }},
		{"non-utc observed at", func(f *Fact) {
			f.ObservedAt = f.ObservedAt.In(time.FixedZone("shifted", 3600))
		}},
		{"zero tick", func(f *Fact) { f.MonotonicTick = 0 }},
		{"expired deadline", func(f *Fact) { f.FreshnessDeadline = f.MonotonicTick }},
		{"unknown lifecycle", func(f *Fact) { f.Lifecycle = Lifecycle("stale") }},
		{"conflict lifecycle", func(f *Fact) { f.Lifecycle = Lifecycle("conflict") }},
		{"unknown reason", func(f *Fact) { f.Reason = Reason("because") }},
		{"empty payload", func(f *Fact) { f.Payload = Payload{} }},
		{"two payloads", func(f *Fact) {
			f.Payload.DNS = &DNSPayload{ResolverClass: ResolverSystem, Responding: true}
		}},
		{"payload for another component", func(f *Fact) {
			f.Payload = Payload{DNS: &DNSPayload{ResolverClass: ResolverSystem, Responding: true}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact := FixtureBaseline(ComponentPhysicalNetwork, 1)
			test.mutate(&fact)
			if err := Validate(fact); err == nil {
				t.Fatal("invalid fact was accepted")
			}
		})
	}
}

func TestPayloadBoundsAreEnforced(t *testing.T) {
	tests := []struct {
		name    string
		payload Payload
	}{
		{"carrier without link", Payload{PhysicalNetwork: &PhysicalNetworkPayload{
			LinkClass: LinkWired, LinkUp: false, HasCarrier: true}}},
		{"no link but up", Payload{PhysicalNetwork: &PhysicalNetworkPayload{
			LinkClass: LinkNone, LinkUp: true}}},
		{"gateway without path", Payload{DefaultPath: &DefaultPathPayload{
			PathClass: PathNone, GatewayPresent: true}}},
		{"failing exceeds scoped", Payload{DNS: &DNSPayload{
			ResolverClass: ResolverScoped, ScopedDomains: 2, FailingDomains: 3}}},
		{"count over bound", Payload{ScopedRoutes: &ScopedRoutesPayload{
			Configured: MaxComponentCount + 1}}},
		{"installed exceeds configured", Payload{ScopedRoutes: &ScopedRoutesPayload{
			Configured: 1, Installed: 2}}},
		{"transport states exceed configured", Payload{Transports: &TransportsPayload{
			Configured: 2, Ready: 2, Degraded: 1}}},
		{"reserve selected without reserve", Payload{Relays: &RelaysPayload{
			Configured: 2, Reachable: 2, Reserve: 0, SelectedClass: SelectedReserve}}},
		{"selected without any relay", Payload{Relays: &RelaysPayload{
			Configured: 0, SelectedClass: SelectedPrimary}}},
		{"connected but unauthenticated", Payload{UserAccess: &UserAccessPayload{
			ProfileClass: ProfileConfigured, Connected: true, Authenticated: false}}},
		{"access without profile", Payload{UserAccess: &UserAccessPayload{
			ProfileClass: ProfileNone, Connected: true, Authenticated: true}}},
		{"sessions without expiry class", Payload{SessionExpiry: &SessionExpiryPayload{
			ExpiryClass: ExpiryNone, Sessions: 2}}},
		{"expiry class without sessions", Payload{SessionExpiry: &SessionExpiryPayload{
			ExpiryClass: ExpiryValid, Sessions: 0}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.payload.validate(); !errors.Is(err, ErrInvalidPayload) {
				t.Fatalf("got %v, want %v", err, ErrInvalidPayload)
			}
		})
	}
}
