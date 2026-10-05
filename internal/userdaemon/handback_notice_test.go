package userdaemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelexec"
)

func noticeLogger(t *testing.T) (*logging.Logger, *bytes.Buffer) {
	t.Helper()
	output := &bytes.Buffer{}
	logger, err := logging.New(output, logging.ComponentUser)
	if err != nil {
		t.Fatal(err)
	}
	return logger, output
}

// Word the root runtime left of giving the tunnel up reaches the operator from
// the session that can speak to them.
func TestAHandbackNoticeIsAnnouncedToTheOperator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel-handback.json")
	at := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano)
	if err := tunnelexec.WriteNotice(path, tunnelexec.Notice{
		Reason: string(tunnelexec.HandbackGrantLapsed), At: at, Given: true,
	}); err != nil {
		t.Fatal(err)
	}
	notifier := &fakeIncidentNotifier{}
	logger, _ := noticeLogger(t)
	dispatchTunnelHandbackNotification(context.Background(), notifier, path, logger)
	if len(notifier.calls) != 1 {
		t.Fatalf("the operator was told %d times", len(notifier.calls))
	}
	incident := notifier.calls[0].Incident
	if incident.Component != control.ComponentTunnel {
		t.Fatalf("the incident is about %q", incident.Component)
	}
	if incident.Severity != event.SeverityCritical {
		t.Fatalf("severity = %q; a machine that may have no tunnel is worth a night", incident.Severity)
	}
	if !strings.Contains(incident.IncidentID, at) {
		t.Fatalf("the identity does not carry when it happened: %q", incident.IncidentID)
	}
	if !notifier.calls[0].DurableGeneration {
		t.Fatal("the announcement would be made again by the next process")
	}
}

// Nothing to read tells nobody anything.
func TestNoNoticeAnnouncesNothing(t *testing.T) {
	notifier := &fakeIncidentNotifier{}
	logger, _ := noticeLogger(t)
	dispatchTunnelHandbackNotification(
		context.Background(), notifier, filepath.Join(t.TempDir(), "absent.json"), logger)
	if len(notifier.calls) != 0 {
		t.Fatalf("an absent notice told the operator %d times", len(notifier.calls))
	}
	// And a daemon that was not told where to look does not guess.
	dispatchTunnelHandbackNotification(context.Background(), notifier, "", logger)
	if len(notifier.calls) != 0 {
		t.Fatal("a daemon with no notice path announced something")
	}
}

// A notice that cannot be read is not an alert about a tunnel. A runtime that
// announced a tunnel loss because it could not parse a file would be worse than
// one that said nothing.
func TestANoticeThatCannotBeReadAnnouncesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel-handback.json")
	if err := os.WriteFile(path, []byte(`{"schema":"something.else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	notifier := &fakeIncidentNotifier{}
	logger, _ := noticeLogger(t)
	dispatchTunnelHandbackNotification(context.Background(), notifier, path, logger)
	if len(notifier.calls) != 0 {
		t.Fatalf("an unreadable notice told the operator %d times", len(notifier.calls))
	}
}
