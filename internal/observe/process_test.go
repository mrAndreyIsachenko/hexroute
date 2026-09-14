package observe

import (
	"context"
	"strings"
	"testing"
)

const (
	tunnelConfig = "/Library/Application Support/Hexroute/observe-root/state/tunnel-config.json"
	otherConfig  = "/Library/Application Support/twilight/supervisor/client/twilight-sing-box-tun.json"
)

func processRunner(listing string) *fakeRunner {
	return &fakeRunner{outputs: map[string][]byte{
		fakeKey(psCommand, "-axo", "pid=,ppid=,uid=,args="): []byte(listing),
	}}
}

// The tunnel is the sing-box running the owner's configuration.
//
// On 2026-09-14 an ingress probe ran beside the tunnel as
// `sing-box run -c /tmp/twilight-vless-ingress.*`, and the lookup this replaces
// took the first process named sing-box. The probe is listed first here, as it
// would be whenever its pid is the lower.
func TestTheTunnelIsTheProcessRunningTheOwnersConfiguration(t *testing.T) {
	observer, err := NewProcessObserver(processRunner(
		"  100     1     0 /usr/libexec/something\n" +
			"  200    88     0 /opt/homebrew/bin/sing-box run -c /tmp/twilight-vless-ingress.gAuoCv\n" +
			"  321     1     0 /opt/homebrew/bin/sing-box run -c " + tunnelConfig + "\n",
	))
	if err != nil {
		t.Fatalf("NewProcessObserver() error: %v", err)
	}
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if !observation.Running || observation.Process.PID != 321 {
		t.Fatalf("Tunnel() = %+v, want pid 321", observation)
	}
}

// A probe alone is not a running tunnel.
//
// Counting it as one would hide a lost tunnel for as long as a probe happened
// to be running, which is the reverse of the stopped-probe fault and as bad.
func TestAProbeIsNotARunningTunnel(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(
		"  200    88     0 /opt/homebrew/bin/sing-box run -c /tmp/twilight-vless-ingress.gAuoCv\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("a probe was taken for the tunnel: %+v", observation)
	}
}

// The previous owner's tunnel is not this owner's.
func TestAnotherOwnersTunnelIsNotThisOnes(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(
		"  300     1     0 /opt/homebrew/bin/sing-box run -c " + otherConfig + "\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("another owner's tunnel was reported as this one: %+v", observation)
	}
	theirs, err := observer.Tunnel(context.Background(), otherConfig)
	if err != nil || !theirs.Running || theirs.Process.PID != 300 {
		t.Fatalf("Tunnel(other) = %+v, %v", theirs, err)
	}
}

// A path that merely starts with the configuration is a different file.
func TestASimilarPathIsNotTheConfiguration(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(
		"  400     1     0 /opt/homebrew/bin/sing-box run -c " + tunnelConfig + ".bak\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("a backup of the configuration was taken for it: %+v", observation)
	}
}

// Another executable given the same configuration is not the tunnel.
func TestOnlySingBoxRunsTheTunnel(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(
		"  500     1     0 /usr/bin/less " + tunnelConfig + "\n" +
			"  501     1     0 /bin/cat -c " + tunnelConfig + "\n" +
			// The one that matters: the right command line under another name.
			// Without it this test passed with the executable check removed,
			// because the two lines above were already refused for not saying
			// "run".
			"  502     1     0 /opt/other/bin/tunnel-helper run -c " + tunnelConfig + "\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("another executable was taken for the tunnel: %+v", observation)
	}
}

func TestNothingRunningIsNotRunning(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner("  100     1     0 /usr/libexec/something\n"))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("Tunnel() = %+v, want not running", observation)
	}
}

// A relative path is not a configuration anyone runs by.
func TestARelativeConfigurationIsRefused(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(""))
	if _, err := observer.Tunnel(context.Background(), "tunnel-config.json"); err == nil {
		t.Fatal("a relative configuration path was accepted")
	}
}

// A long command line elsewhere does not hide the tunnel.
//
// Measured on 2026-09-14: 1,005 processes, two of them with command lines over
// 4,096 bytes, and a bound on that length refused the whole listing — so the
// tunnel was neither found nor reported absent.
func TestALongCommandLineElsewhereDoesNotHideTheTunnel(t *testing.T) {
	long := "/Applications/Some.app/Contents/Frameworks/Helper " + strings.Repeat("--flag=value ", 540)
	observer, _ := NewProcessObserver(processRunner(
		"  82817     1   501 " + long + "\n" +
			"  321     1     0 /opt/homebrew/bin/sing-box run -c " + tunnelConfig + "\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("a %d-byte command line elsewhere failed the observation: %v", len(long), err)
	}
	if !observation.Running || observation.Process.PID != 321 {
		t.Fatalf("Tunnel() = %+v, want pid 321", observation)
	}
}

// A sing-box that is not root's is not the tunnel, whatever it runs.
//
// Both owners start the tunnel as root. Any user can start a sing-box naming
// the owner's configuration; counted, it would hide a lost tunnel or be what a
// release stops. It is listed first, as a lower pid would be.
func TestAnUnprivilegedSingBoxIsNotTheTunnel(t *testing.T) {
	observer, _ := NewProcessObserver(processRunner(
		"  250     1   501 /Users/someone/bin/sing-box run -c " + tunnelConfig + "\n",
	))
	observation, err := observer.Tunnel(context.Background(), tunnelConfig)
	if err != nil {
		t.Fatalf("Tunnel() error: %v", err)
	}
	if observation.Running {
		t.Fatalf("a user's sing-box was taken for the tunnel: %+v", observation)
	}

	withRoot, _ := NewProcessObserver(processRunner(
		"  250     1   501 /Users/someone/bin/sing-box run -c " + tunnelConfig + "\n" +
			"  321     1     0 /opt/homebrew/bin/sing-box run -c " + tunnelConfig + "\n",
	))
	observation, err = withRoot.Tunnel(context.Background(), tunnelConfig)
	if err != nil || !observation.Running || observation.Process.PID != 321 {
		t.Fatalf("Tunnel() beside a user's sing-box = %+v, %v; want root's pid 321", observation, err)
	}
}
