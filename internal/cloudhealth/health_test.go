package cloudhealth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const healthInstanceID = metadata.UUID("11111111-1111-4111-8111-111111111111")

func TestWriterPublishesValidatedHeartbeat(t *testing.T) {
	now := time.Date(2026, time.July, 24, 20, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	writer, err := NewWriter(
		store,
		"primary",
		healthInstanceID,
		"v0.1.0-test",
		now.Add(-time.Minute),
		30*time.Second,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Once(context.Background()); err != nil {
		t.Fatalf("Once() error = %v", err)
	}
	if len(store.writes) != 1 || store.writes[0].HeartbeatAt != now {
		t.Fatalf("writes = %+v", store.writes)
	}
}

func TestCheckerRequiresDatabaseAndFreshWorker(t *testing.T) {
	now := time.Date(2026, time.July, 24, 20, 0, 0, 0, time.UTC)
	fresh := Heartbeat{
		WorkerName:         "primary",
		InstanceID:         healthInstanceID,
		ApplicationVersion: "v0.1.0",
		StartedAt:          now.Add(-time.Hour),
		HeartbeatAt:        now.Add(-10 * time.Second),
	}
	tests := []struct {
		name      string
		store     *fakeStore
		wantReady bool
	}{
		{name: "fresh", store: &fakeStore{heartbeat: fresh}, wantReady: true},
		{
			name:  "database unavailable",
			store: &fakeStore{pingErr: errors.New("offline"), heartbeat: fresh},
		},
		{
			name:  "worker missing",
			store: &fakeStore{readErr: ErrWorkerNotFound},
		},
		{
			name: "worker stale",
			store: &fakeStore{heartbeat: func() Heartbeat {
				value := fresh
				value.HeartbeatAt = now.Add(-2 * time.Minute)
				return value
			}()},
		},
		{
			name: "worker timestamp too far in future",
			store: &fakeStore{heartbeat: func() Heartbeat {
				value := fresh
				value.HeartbeatAt = now.Add(time.Minute)
				return value
			}()},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker, err := NewChecker(
				test.store,
				"primary",
				time.Minute,
				15*time.Second,
				func() time.Time { return now },
			)
			if err != nil {
				t.Fatalf("NewChecker() error = %v", err)
			}
			status, err := checker.Check(context.Background())
			if test.wantReady {
				if err != nil || status != StatusReady {
					t.Fatalf("Check() = %q, %v", status, err)
				}
				return
			}
			if !errors.Is(err, ErrNotReady) || status != StatusNotReady {
				t.Fatalf("Check() = %q, %v", status, err)
			}
		})
	}
}

func TestReadinessHandlerReturnsBoundedResponses(t *testing.T) {
	now := time.Date(2026, time.July, 24, 20, 0, 0, 0, time.UTC)
	freshStore := &fakeStore{heartbeat: Heartbeat{
		WorkerName:         "primary",
		InstanceID:         healthInstanceID,
		ApplicationVersion: "v0.1.0",
		StartedAt:          now.Add(-time.Hour),
		HeartbeatAt:        now,
	}}
	handler := testHandler(t, freshStore, now)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Body.String() != "{\"status\":\"ready\"}\n" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ready response = %d %q", response.Code, response.Body.String())
	}

	unavailable := testHandler(t, &fakeStore{pingErr: errors.New("database detail")}, now)
	request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response = httptest.NewRecorder()
	unavailable.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		response.Body.String() != "{\"status\":\"not_ready\"}\n" ||
		strings.Contains(response.Body.String(), "database") {
		t.Fatalf("unavailable response = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/readyz", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed ||
		response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method response = %d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

type fakeStore struct {
	pingErr   error
	readErr   error
	writeErr  error
	heartbeat Heartbeat
	writes    []Heartbeat
}

func (store *fakeStore) Ping(context.Context) error {
	return store.pingErr
}

func (store *fakeStore) WriteHeartbeat(
	_ context.Context,
	heartbeat Heartbeat,
) error {
	store.writes = append(store.writes, heartbeat)
	return store.writeErr
}

func (store *fakeStore) ReadHeartbeat(
	context.Context,
	string,
) (Heartbeat, error) {
	return store.heartbeat, store.readErr
}

func testHandler(t *testing.T, store Store, now time.Time) *Handler {
	t.Helper()
	checker, err := NewChecker(
		store,
		"primary",
		time.Minute,
		15*time.Second,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}
	handler, err := NewHandler(checker)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}
