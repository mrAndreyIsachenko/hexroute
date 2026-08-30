package ipc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func publication(facts ...string) *PublishConnectivityFactsRequest {
	raw := make([]json.RawMessage, 0, len(facts))
	for _, fact := range facts {
		raw = append(raw, json.RawMessage(fact))
	}
	return &PublishConnectivityFactsRequest{
		Domain: policy.DomainUser, BootID: "boot-abcdef0123456789", Facts: raw,
	}
}

func publishRequest(payload *PublishConnectivityFactsRequest) Request {
	return Request{
		Version: ProtocolVersion, RequestID: "0123456789abcdef",
		Action: ActionPublishConnectivityFacts, PublishConnectivityFacts: payload,
	}
}

func TestPublicationIsAccepted(t *testing.T) {
	request := publishRequest(publication(`{"schema":"x"}`, `{"schema":"y"}`))
	if err := request.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// Root observes its own components. A root-domain publication would be root
// telling itself something, which is a shape this transport should not have.
func TestOnlyTheUserDomainMayPublish(t *testing.T) {
	payload := publication(`{"schema":"x"}`)
	payload.Domain = policy.DomainRoot
	if err := publishRequest(payload).Validate(); !errors.Is(err, ErrConnectivityDomain) {
		t.Fatalf("got %v, want %v", err, ErrConnectivityDomain)
	}
	payload.Domain = policy.Domain("other")
	if err := publishRequest(payload).Validate(); !errors.Is(err, ErrConnectivityDomain) {
		t.Fatalf("got %v, want %v", err, ErrConnectivityDomain)
	}
}

func TestPublicationBoundsAreEnforced(t *testing.T) {
	oversized := strings.Repeat("a", MaxPublishedFactBytes+1)
	large := strings.Repeat("b", MaxPublishedFactBytes)

	tests := []struct {
		name    string
		payload *PublishConnectivityFactsRequest
	}{
		{"no facts", publication()},
		{"empty fact", publication("")},
		{"oversized fact", publication(oversized)},
		{"too many facts", func() *PublishConnectivityFactsRequest {
			facts := make([]string, MaxPublishedFacts+1)
			for index := range facts {
				facts[index] = `{"schema":"x"}`
			}
			return publication(facts...)
		}()},
		{"within count and size but over the total", publication(large, large, large)},
		{"missing boot id", func() *PublishConnectivityFactsRequest {
			payload := publication(`{"schema":"x"}`)
			payload.BootID = ""
			return payload
		}()},
		{"boot id carrying a path", func() *PublishConnectivityFactsRequest {
			payload := publication(`{"schema":"x"}`)
			payload.BootID = "../../etc/passwd"
			return payload
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := publishRequest(test.payload).Validate(); err == nil {
				t.Fatal("an out-of-bounds publication was accepted")
			}
		})
	}
}

// A publication is not an action: it may not name a target or a generation,
// and it may not travel alongside another payload.
func TestPublicationCannotLookLikeAnAction(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"with a target", func(r *Request) { r.Target = "tunnel" }},
		{"with a generation", func(r *Request) { r.ExpectedGeneration = 4 }},
		{"with a second payload", func(r *Request) {
			r.ReconcilerShadowStatus = &ReconcilerShadowStatusRequest{}
		}},
		{"without its payload", func(r *Request) { r.PublishConnectivityFacts = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := publishRequest(publication(`{"schema":"x"}`))
			test.mutate(&request)
			if err := request.Validate(); err == nil {
				t.Fatal("a publication shaped like an action was accepted")
			}
		})
	}
}

// The result reports what happened to the facts and nothing the caller could
// act on.
func TestPublicationResultCarriesNoAuthority(t *testing.T) {
	result := PublishConnectivityFactsResult{Accepted: 2, HighWatermark: 9}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	permitted := map[string]struct{}{
		"accepted": {}, "duplicates": {}, "conflicts": {},
		"rejected": {}, "high_watermark": {},
	}
	for name := range generic {
		if _, ok := permitted[name]; !ok {
			t.Fatalf("publication result gained field %q", name)
		}
	}
}

// The whole publication must still fit the frame the transport allows.
func TestPublicationCannotExceedTheFrame(t *testing.T) {
	if MaxPublishedTotalBytes >= MaxFrameBytes {
		t.Fatalf("a maximal publication (%d) does not leave room in a %d byte frame",
			MaxPublishedTotalBytes, MaxFrameBytes)
	}
}

// A well-formed publication response must survive the server's own check.
// This is the gap that made the path unusable: the response type carried the
// result, nothing counted it as a payload, and every correct answer came back
// to the caller as an internal error.
func TestPublicationResponseIsWellFormed(t *testing.T) {
	response := Response{
		Version: ProtocolVersion, RequestID: "publication-response", OK: true,
		PublishConnectivityFacts: &PublishConnectivityFactsResult{
			Accepted: 2, HighWatermark: 9,
		},
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("a correct publication response was refused: %v", err)
	}
}

func TestPublicationResponseRejectsAnEmptyAccounting(t *testing.T) {
	response := Response{
		Version: ProtocolVersion, RequestID: "publication-empty", OK: true,
		PublishConnectivityFacts: &PublishConnectivityFactsResult{},
	}
	if err := response.Validate(); err == nil {
		t.Fatal("a result accounting for no fact at all was accepted")
	}
}

func TestPublicationResponseRejectsMoreOutcomesThanFactsAllowed(t *testing.T) {
	response := Response{
		Version: ProtocolVersion, RequestID: "publication-overflow", OK: true,
		PublishConnectivityFacts: &PublishConnectivityFactsResult{
			Accepted: MaxPublishedFacts + 1,
		},
	}
	if err := response.Validate(); err == nil {
		t.Fatal("a result accounting for more facts than may be sent was accepted")
	}
}
