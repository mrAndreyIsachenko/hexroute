package userdaemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

// TestPublishDeadlineStaysInsideTheCycle is the property, not the number.
//
// The default it replaces was fifteen seconds against a fifteen-second cycle,
// so one slow peer consumed the whole of it — which is how a fault in the root
// daemon presented as a user daemon that would not answer. A constant would
// hold that relationship only until someone changed the interval.
func TestPublishDeadlineStaysInsideTheCycle(t *testing.T) {
	for _, interval := range []time.Duration{
		5 * time.Second,
		15 * time.Second,
		60 * time.Second,
		5 * time.Minute,
	} {
		deadline := publishDeadline(interval)
		if deadline >= interval {
			t.Fatalf("a %s cycle waits %s for a publication; the cycle cannot keep its schedule",
				interval, deadline)
		}
		if deadline > interval/2 {
			t.Fatalf("a %s cycle spends %s of it waiting on a peer", interval, deadline)
		}
	}
}

// TestPublishDeadlineHasAFloor keeps a misconfigured or very short interval
// from producing a deadline no round trip could meet, which would turn every
// publication into a failure rather than bounding a slow one.
func TestPublishDeadlineHasAFloor(t *testing.T) {
	if deadline := publishDeadline(time.Millisecond); deadline < time.Second {
		t.Fatalf("deadline = %s, want at least a second even for an absurd interval", deadline)
	}
}

// TestASlowPeerCostsOnePublicationNotTheCycle asserts the behaviour the
// deadline exists for: the publication is abandoned, nothing is buffered, and
// the caller is not held.
//
// Buffering would be the wrong repair and the existing code says so: a fact
// held back and delivered later would describe a moment that has passed.
func TestASlowPeerCostsOnePublicationNotTheCycle(t *testing.T) {
	publisher, err := newFactPublisher(
		"boot-0000000000000000", "/tmp/probe.sock",
		filepath.Join(t.TempDir(), "connectivity-stream.json"),
		3*time.Second,
	)
	if err != nil || publisher == nil {
		t.Fatalf("publisher: %v", err)
	}

	var waited time.Duration
	publisher.roundTrip = func(ctx context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		start := time.Now()
		<-ctx.Done()
		waited = time.Since(start)
		return ipc.Response{}, ctx.Err()
	}

	start := time.Now()
	if err := publisher.Publish(context.Background(), observedEvidence(), nil); err != nil {
		t.Fatalf("Publish() over an unresponsive peer = %v; the cycle was failed rather than continued", err)
	}
	elapsed := time.Since(start)

	if waited == 0 {
		t.Fatal("the publication was never attempted")
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("Publish() held the cycle for %s of its own %s interval", elapsed, 3*time.Second)
	}
	if publisher.baseline {
		t.Fatal("an abandoned publication was recorded as accepted")
	}
}

// TestTheDeadlineBoundsWaitingAndNotOnlyConnecting is the half a stubbed round
// trip cannot see, and it is the half that mattered.
//
// ipc.Client.Do honours the context while dialling and its own Timeout while
// waiting for the answer. A publication that set only the context therefore
// connected promptly and then waited fifteen seconds anyway — which is exactly
// what a fifteen-second cycle could not afford, and what made a healthy user
// daemon look dead.
//
// So this drives the real client against a peer that accepts and never speaks.
func TestTheDeadlineBoundsWaitingAndNotOnlyConnecting(t *testing.T) {
	socket := filepath.Join(shortTempDir(t), "silent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan struct{}, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- struct{}{}
		// Accept and say nothing, holding the connection open.
		<-time.After(time.Minute)
		_ = connection.Close()
	}()

	publisher, err := newFactPublisher(
		"boot-0000000000000000", socket,
		filepath.Join(t.TempDir(), "connectivity-stream.json"),
		3*time.Second,
	)
	if err != nil || publisher == nil {
		t.Fatalf("publisher: %v", err)
	}

	start := time.Now()
	if err := publisher.Publish(context.Background(), observedEvidence(), nil); err != nil {
		t.Fatalf("Publish() against a silent peer = %v", err)
	}
	elapsed := time.Since(start)

	select {
	case <-accepted:
	default:
		t.Fatal("the peer was never connected to; this measured the wrong thing")
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("waiting for a silent peer took %s of a 3s cycle; "+
			"the deadline bounds connecting but not waiting", elapsed)
	}
}

// shortTempDir keeps the socket path inside sun_path, which t.TempDir() alone
// exceeds on macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("", "hexroute-pub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	return base
}
