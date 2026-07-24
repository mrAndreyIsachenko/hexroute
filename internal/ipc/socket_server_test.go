package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
)

type staticHandler struct{}

func (staticHandler) HandleIPC(_ context.Context, request Request) Response {
	status := Status{
		Role:       RoleUser,
		Mode:       ModeObserveOnly,
		State:      control.StateHealthy,
		Generation: 3,
	}
	return Response{
		Version:   ProtocolVersion,
		RequestID: request.RequestID,
		OK:        true,
		Status:    &status,
	}
}

type recordingReporter struct {
	mu     sync.Mutex
	errors []error
}

func (reporter *recordingReporter) ReportIPCRejection(err error) {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.errors = append(reporter.errors, err)
}

func (reporter *recordingReporter) contains(target error) bool {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	for _, err := range reporter.errors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func TestSocketServerAuthenticatesAndRoundTripsTypedRequest(t *testing.T) {
	path := shortSocketPath(t)
	uid := uint32(os.Getuid())
	reporter := &recordingReporter{}
	server, err := Listen(path, uid, uid, staticHandler{}, reporter)
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat() error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()

	request := Request{
		Version:   ProtocolVersion,
		RequestID: "status-roundtrip",
		Action:    ActionStatus,
	}
	response, err := (Client{Path: path}).Do(context.Background(), request)
	if err != nil {
		t.Fatalf("Client.Do() error: %v", err)
	}
	if !response.OK ||
		response.Status == nil ||
		response.Status.Role != RoleUser ||
		response.Status.Generation != 3 {
		t.Fatalf("response = %+v", response)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not stop after cancellation")
	}
}

func TestSocketServerRejectsUnauthorizedAndMalformedPeers(t *testing.T) {
	t.Run("unauthorized UID", func(t *testing.T) {
		path := shortSocketPath(t)
		reporter := &recordingReporter{}
		server, err := Listen(
			path,
			uint32(os.Getuid()),
			uint32(os.Getuid()+1),
			staticHandler{},
			reporter,
		)
		if err != nil {
			t.Fatalf("Listen() error: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- server.Serve(ctx)
		}()

		_, err = (Client{Path: path, Timeout: time.Second}).Do(
			context.Background(),
			Request{
				Version:   ProtocolVersion,
				RequestID: "unauthorized",
				Action:    ActionStatus,
			},
		)
		if err == nil {
			t.Fatal("unauthorized client received a response")
		}
		waitForReport(t, reporter, ErrUnauthorizedPeer)
		cancel()
		<-done
	})

	t.Run("malformed frame", func(t *testing.T) {
		path := shortSocketPath(t)
		uid := uint32(os.Getuid())
		reporter := &recordingReporter{}
		server, err := Listen(path, uid, uid, staticHandler{}, reporter)
		if err != nil {
			t.Fatalf("Listen() error: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- server.Serve(ctx)
		}()

		connection, err := net.DialUnix(
			"unix",
			nil,
			&net.UnixAddr{Name: path, Net: "unix"},
		)
		if err != nil {
			t.Fatalf("DialUnix() error: %v", err)
		}
		if _, err := connection.Write([]byte{0, 0, 0, 1, '{'}); err != nil {
			t.Fatalf("write malformed frame: %v", err)
		}
		_ = connection.Close()
		waitForReport(t, reporter, ErrMalformedFrame)
		cancel()
		<-done
	})
}

func TestListenRejectsUnsafeSocketPaths(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	_, err := Listen(
		filepath.Join(directory, "unsafe.sock"),
		uint32(os.Getuid()),
		uint32(os.Getuid()),
		staticHandler{},
		nil,
	)
	if !errors.Is(err, ErrInvalidSocketPath) {
		t.Fatalf("Listen() error = %v, want %v", err, ErrInvalidSocketPath)
	}
}

func waitForReport(t *testing.T, reporter *recordingReporter, target error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if reporter.contains(target) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("reporter did not receive %v", target)
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "hr-ipc-")
	if err != nil {
		t.Fatalf("MkdirTemp() error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(directory)
	})
	return filepath.Join(directory, "daemon.sock")
}
