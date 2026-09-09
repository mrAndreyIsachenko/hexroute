//go:build darwin

package pritunlrescue

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The parse has only ever been checked against strings written by hand.
//
// loadedAndNotRunning reads one field out of `launchctl print`, and every test
// of it supplies the output itself. If this version of macOS words that field
// differently, or stops printing it, the check answers "not stale" for every
// service forever — and the failure is silent, because answering "not stale" is
// what it does when it cannot tell.
//
// So this runs the real command against a job it creates, in the user domain,
// touching nothing that matters. It also answers a question the runbook could
// not: whether a loaded, not-running service is observable at all.
func TestTheStaleParseAgreesWithRealLaunchctl(t *testing.T) {
	if !userSessionAvailable() {
		t.Skip("no launchd user session")
	}
	label := fmt.Sprintf("com.hexroute.test.stale.%d", os.Getpid())
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)

	// A job that is loaded and does nothing. RunAtLoad and KeepAlive are both
	// absent, which is the whole point: launchd has no reason to start it, so
	// it stays in the state the verifier is looking for.
	plist := filepath.Join(t.TempDir(), label+".plist")
	write(t, plist, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>/bin/sleep</string><string>60</string></array>
  <key>RunAtLoad</key><false/>
</dict></plist>
`, label))

	if out, err := run("/bin/launchctl", "bootstrap",
		fmt.Sprintf("gui/%d", os.Getuid()), plist); err != nil {
		t.Fatalf("bootstrap: %v: %s", err, out)
	}
	t.Cleanup(func() { _, _ = run("/bin/launchctl", "bootout", target) })

	loaded, err := run("/bin/launchctl", "print", target)
	if err != nil {
		t.Fatalf("print a loaded job: %v: %s", err, loaded)
	}
	if !loadedAndNotRunning(loaded) {
		t.Fatalf("a loaded job that is not running did not read as stale.\n"+
			"This is what the verifier reads, and it answers 'not stale' when "+
			"it cannot tell:\n%s", firstLines(loaded, 12))
	}

	// And the other way, so the check is not simply always true.
	if out, err := run("/bin/launchctl", "kickstart", target); err != nil {
		t.Fatalf("kickstart: %v: %s", err, out)
	}
	running, err := run("/bin/launchctl", "print", target)
	if err != nil {
		t.Fatalf("print a running job: %v: %s", err, running)
	}
	if loadedAndNotRunning(running) {
		t.Fatalf("a running job read as stale:\n%s", firstLines(running, 12))
	}
}

func userSessionAvailable() bool {
	_, err := run("/bin/launchctl", "print", fmt.Sprintf("gui/%d", os.Getuid()))
	return err == nil
}

func run(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	return string(output), err
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func firstLines(text string, count int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > count {
		lines = lines[:count]
	}
	return strings.Join(lines, "\n")
}
