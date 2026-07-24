package alertdelivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cloudincident"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestTelegramClientPostsBoundedJSON(t *testing.T) {
	const (
		token  = "123456789:synthetic_token_value"
		chatID = "-1001234567890"
	)
	var captured string
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost ||
				request.URL.String() != telegramAPIBase+"/bot"+token+"/sendMessage" ||
				request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("request = %s %s", request.Method, request.URL)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read request: %v", err)
			}
			captured = string(body)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	client, err := NewTelegramClient(httpClient, token, chatID)
	if err != nil {
		t.Fatalf("NewTelegramClient() error = %v", err)
	}
	if err := client.Send(context.Background(), "Hexroute synthetic alert"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.Contains(captured, `"chat_id":"`+chatID+`"`) ||
		!strings.Contains(captured, `"text":"Hexroute synthetic alert"`) ||
		strings.Contains(captured, token) {
		t.Fatalf("request body = %q", captured)
	}
}

func TestTelegramClientReturnsGenericErrorWithoutResponseLeak(t *testing.T) {
	const canary = "HEXROUTE_CANARY_BOT_RESPONSE_SECRET"
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(canary)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	client, err := NewTelegramClient(
		httpClient,
		"123456789:synthetic_token_value",
		"123456789",
	)
	if err != nil {
		t.Fatalf("NewTelegramClient() error = %v", err)
	}
	err = client.Send(context.Background(), "Hexroute synthetic alert")
	if !errors.Is(err, ErrTelegramUnavailable) ||
		strings.Contains(err.Error(), canary) {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestRenderedMessagesContainNoDatabaseIdentifiers(t *testing.T) {
	delivery := Delivery{
		DeliveryID: metadata.UUID("22222222-2222-4222-8222-222222222222"),
		Snapshot: Snapshot{
			IncidentID: policyIncidentID,
			Generation: 4,
			Status:     cloudincident.StatusOpen,
			Severity:   event.SeverityCritical,
			Category:   event.IncidentAvailability,
			Component:  control.ComponentTunnel,
		},
		Actionable: true,
	}
	rendered := renderDelivery(delivery)
	for _, forbidden := range []string{
		string(delivery.DeliveryID),
		string(delivery.Snapshot.IncidentID),
		"HEXROUTE_CANARY",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered message contains %q: %q", forbidden, rendered)
		}
	}
	if len(rendered) > maxTelegramTextBytes {
		t.Fatalf("rendered message length = %d", len(rendered))
	}
}
