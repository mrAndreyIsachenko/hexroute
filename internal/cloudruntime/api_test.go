package cloudruntime

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
