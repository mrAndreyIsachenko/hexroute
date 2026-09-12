package observe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The assertion the sixth cause rests on.
//
// A socket that accepts a connection and then closes proves that something
// accepted a socket. Anything that only dials calls that a success. It is not
// traversal, and a tunnel that is up and carrying nothing is exactly what the
// production supervisor restarts for — twice in sixty-one days.
func TestASocketThatAnswersWithoutCarryingThePayloadIsAFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Accepted and dropped: the shape of a path that is up and carries
			// nothing.
			_ = conn.Close()
		}
	}()

	prober := NewPayloadProber()
	observation, err := prober.Payload(context.Background(), PayloadEndpoint{
		Name:    "payload",
		URL:     "http://" + listener.Addr().String() + "/",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if observation.Traversed {
		t.Fatal("a connection that carried nothing was reported as traversal")
	}
}

func TestAResponseIsTraversal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}))
	defer server.Close()

	prober := NewPayloadProber()
	observation, err := prober.Payload(context.Background(), PayloadEndpoint{
		Name:    "payload",
		URL:     server.URL,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if !observation.Traversed {
		t.Fatal("a response was not counted as traversal")
	}
	if observation.Status != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", observation.Status, http.StatusNoContent)
	}
}

// A status that is not a success is still traversal: the request reached a
// server and the server answered. What the sixth cause counts is the path, not
// the application's opinion of the request.
func TestAnErrorStatusIsStillTraversal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadGateway)
		}))
	defer server.Close()

	prober := NewPayloadProber()
	observation, err := prober.Payload(context.Background(), PayloadEndpoint{
		Name: "payload", URL: server.URL, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if !observation.Traversed {
		t.Fatalf("status %d was not counted as traversal", observation.Status)
	}
}

// Nothing listening is not traversal, and is not an error the cycle has to
// handle: the probe answers the question it was asked.
func TestNothingListeningIsNotTraversal(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()

	prober := NewPayloadProber()
	observation, err := prober.Payload(context.Background(), PayloadEndpoint{
		Name: "payload", URL: "http://" + address + "/", Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Payload() error: %v", err)
	}
	if observation.Traversed {
		t.Fatal("an unreachable address was reported as traversal")
	}
}

func TestAnEndpointThatCannotBeProbedIsRefused(t *testing.T) {
	prober := NewPayloadProber()
	for _, endpoint := range []PayloadEndpoint{
		{Name: "", URL: "http://127.0.0.1/", Timeout: time.Second},
		{Name: "payload", URL: "", Timeout: time.Second},
		{Name: "payload", URL: "http://127.0.0.1/", Timeout: 0},
		{Name: "payload", URL: "http://127.0.0.1/", Timeout: time.Hour},
	} {
		if _, err := prober.Payload(context.Background(), endpoint); err == nil {
			t.Fatalf("Payload() accepted %+v", endpoint)
		}
	}
}
