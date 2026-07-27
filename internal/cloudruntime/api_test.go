package cloudruntime

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/cutoverfreeze"
)

func TestBindPublicHostRejectsAlternateHostBeforeHandler(t *testing.T) {
	calls := 0
	handler := bindPublicHost(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		calls++
		response.WriteHeader(http.StatusNoContent)
	}), "status.example", "hexroute-example.ondigitalocean.app")

	request, err := http.NewRequest(http.MethodGet, "https://status.example/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	response := &responseFixture{header: make(http.Header)}
	handler.ServeHTTP(response, request)
	if response.status != http.StatusNoContent || calls != 1 {
		t.Fatalf("expected-host response=%d calls=%d", response.status, calls)
	}

	request.Host = "hexroute-example.ondigitalocean.app"
	response = &responseFixture{header: make(http.Header)}
	handler.ServeHTTP(response, request)
	if response.status != http.StatusNoContent || calls != 2 {
		t.Fatalf("provider-host response=%d calls=%d", response.status, calls)
	}

	request.Host = "provider-host.example"
	response = &responseFixture{header: make(http.Header)}
	handler.ServeHTTP(response, request)
	if response.status != http.StatusMisdirectedRequest || calls != 2 {
		t.Fatalf("alternate-host response=%d calls=%d", response.status, calls)
	}

	request.URL.Path = "/readyz"
	response = &responseFixture{header: make(http.Header)}
	handler.ServeHTTP(response, request)
	if response.status != http.StatusNoContent || calls != 3 {
		t.Fatalf("platform-probe response=%d calls=%d", response.status, calls)
	}
}

type freezeReaderFunc func(context.Context) (cutoverfreeze.State, error)

func (function freezeReaderFunc) Read(ctx context.Context) (cutoverfreeze.State, error) {
	return function(ctx)
}

func TestRejectFrozenWritesReturnsStableRetryableResponse(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls++
		response.WriteHeader(http.StatusNoContent)
	})
	reader := freezeReaderFunc(func(context.Context) (cutoverfreeze.State, error) {
		return cutoverfreeze.State{Frozen: true}, nil
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/ingest/batches", nil)
	response := httptest.NewRecorder()
	rejectFrozenWrites(next, reader).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || calls != 0 ||
		response.Header().Get("Retry-After") != "60" ||
		response.Body.String() != `{"status":"write_frozen","write_frozen":true}`+"\n" {
		t.Fatalf(
			"frozen response=%d calls=%d retry=%q body=%q",
			response.Code,
			calls,
			response.Header().Get("Retry-After"),
			response.Body.String(),
		)
	}
}

func TestFreezeAwareReadinessBypassesHeartbeatOnlyBeforeDeadline(t *testing.T) {
	now := time.Date(2026, time.July, 27, 1, 0, 0, 0, time.UTC)
	normalCalls := 0
	normal := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		normalCalls++
		response.WriteHeader(http.StatusOK)
	})
	state := cutoverfreeze.State{}
	reader := freezeReaderFunc(func(context.Context) (cutoverfreeze.State, error) {
		return state, nil
	})
	handler := freezeAwareReadiness(normal, reader, func() time.Time { return now })

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK || normalCalls != 1 {
		t.Fatalf("normal readiness=%d calls=%d", response.Code, normalCalls)
	}

	state = cutoverfreeze.State{
		Frozen:     true,
		FrozenAt:   now.Add(-time.Minute),
		DeadlineAt: now.Add(time.Minute),
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK || normalCalls != 1 ||
		response.Body.String() != `{"status":"ready","write_frozen":true}`+"\n" {
		t.Fatalf("frozen readiness=%d calls=%d body=%q", response.Code, normalCalls, response.Body.String())
	}

	state.DeadlineAt = now
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || normalCalls != 1 ||
		response.Body.String() != `{"status":"not_ready","write_frozen":true}`+"\n" {
		t.Fatalf("expired readiness=%d calls=%d body=%q", response.Code, normalCalls, response.Body.String())
	}
}

func TestLivenessIsBoundedAndMethodRestricted(t *testing.T) {
	for _, test := range []struct {
		method string
		status int
		body   string
	}{
		{method: http.MethodGet, status: http.StatusOK, body: `{"status":"live"}` + "\n"},
		{
			method: http.MethodPost,
			status: http.StatusMethodNotAllowed,
			body:   `{"status":"not_live"}` + "\n",
		},
	} {
		request, err := http.NewRequest(test.method, "https://status.example/livez", nil)
		if err != nil {
			t.Fatalf("http.NewRequest() error = %v", err)
		}
		response := &responseFixture{header: make(http.Header)}
		liveness(response, request)
		if response.status != test.status || response.body.String() != test.body {
			t.Fatalf(
				"liveness(%s) = %d %q",
				test.method,
				response.status,
				response.body.String(),
			)
		}
	}
}

func TestServeUntilCanceledStopsAcceptingWithinDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	contextValue, cancel := context.WithCancel(context.Background())
	server := &http.Server{Handler: http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusNoContent)
	})}
	result := make(chan error, 1)
	go func() {
		result <- serveUntilCanceled(
			contextValue,
			server,
			listener,
			time.Second,
		)
	}()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("GET before shutdown error = %v", err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serveUntilCanceled() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveUntilCanceled() did not stop")
	}
	if _, err := client.Get("http://" + listener.Addr().String()); err == nil {
		t.Fatal("server still accepts after shutdown")
	}
}

type responseFixture struct {
	header http.Header
	body   strings.Builder
	status int
}

func (response *responseFixture) Header() http.Header {
	return response.header
}

func (response *responseFixture) Write(value []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return response.body.Write(value)
}

func (response *responseFixture) WriteHeader(status int) {
	response.status = status
}
