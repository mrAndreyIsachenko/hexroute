package tunnelstart

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
)

type testSigner struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

func (signer testSigner) PublicKey() (ed25519.PublicKey, error) { return signer.public, nil }
func (signer testSigner) Sign(message []byte) ([]byte, error) {
	return ed25519.Sign(signer.private, message), nil
}

type watchingRunner struct {
	started  int
	stopped  int
	binary   string
	args     []string
	contents string
}

func (runner *watchingRunner) Start(_ context.Context, binary string, args ...string) (int, error) {
	runner.started++
	runner.binary, runner.args = binary, args
	for index, argument := range args {
		if argument == "-c" && index+1 < len(args) {
			bytes, err := os.ReadFile(args[index+1])
			if err == nil {
				runner.contents = string(bytes)
			}
		}
	}
	return 7331, nil
}

func (runner *watchingRunner) Stop(int) error { runner.stopped++; return nil }

func published(t *testing.T, signer testSigner, target configversion.Target, content string) []byte {
	t.Helper()
	artifact, err := configversion.Publish(
		signer, target, "v1", []byte(content), time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	encoded, err := configversion.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return encoded
}

func starter(t *testing.T) (*Starter, *watchingRunner, testSigner, configversion.Target) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := testSigner{public: public, private: private}
	target := configversion.Target{Kind: configversion.TargetNode, Key: "this-host"}
	runner := &watchingRunner{}
	root := t.TempDir()
	return &Starter{
		ArtifactPath: filepath.Join(root, "version.json"),
		PublicKey:    public,
		Target:       target,
		ContentPath:  filepath.Join(root, "runtime", "sing-box.json"),
		Binary:       "/usr/local/bin/sing-box",
		Runner:       runner,
	}, runner, signer, target
}

// The tunnel starts from the bytes the signature covers.
func TestTheTunnelStartsFromTheSignedContent(t *testing.T) {
	start, runner, signer, target := starter(t)
	content := `{"log":{"level":"warn"}}`
	if err := os.WriteFile(start.ArtifactPath, published(t, signer, target, content), 0o600); err != nil {
		t.Fatal(err)
	}

	pid, err := start.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid != 7331 || runner.started != 1 {
		t.Fatalf("started %d times, pid %d", runner.started, pid)
	}
	if runner.contents != content {
		t.Fatalf("the process was started on %q, want the signed bytes", runner.contents)
	}
	if strings.Join(runner.args, " ") != "run -c "+start.ContentPath {
		t.Fatalf("arguments = %v", runner.args)
	}
	info, err := os.Stat(start.ContentPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the configuration is readable by others: %v", info.Mode().Perm())
	}
}

// A file edited between starts is replaced, not obeyed.
//
// Verifying once at installation would let anyone able to write the file
// afterwards choose the bytes that carry every packet this machine sends.
func TestAnEditedConfigurationIsReplacedAtEveryStart(t *testing.T) {
	start, runner, signer, target := starter(t)
	content := `{"log":{"level":"warn"}}`
	if err := os.WriteFile(start.ArtifactPath, published(t, signer, target, content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := start.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := os.WriteFile(start.ContentPath, []byte(`{"log":{"level":"debug"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := start.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if runner.contents != content {
		t.Fatalf("the second start ran %q; an edit between starts was obeyed", runner.contents)
	}
}

// A version that does not verify does not start, and says which check failed.
func TestAVersionThatDoesNotVerifyDoesNotStart(t *testing.T) {
	for _, item := range []struct {
		name    string
		corrupt func(t *testing.T, start *Starter, signer testSigner, target configversion.Target)
	}{
		{"signed by another key", func(t *testing.T, start *Starter, _ testSigner, target configversion.Target) {
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			other := testSigner{public: public, private: private}
			if err := os.WriteFile(start.ArtifactPath,
				published(t, other, target, `{"log":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"meant for another host", func(t *testing.T, start *Starter, signer testSigner, _ configversion.Target) {
			elsewhere := configversion.Target{Kind: configversion.TargetNode, Key: "another-host"}
			if err := os.WriteFile(start.ArtifactPath,
				published(t, signer, elsewhere, `{"log":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"content changed after signing", func(t *testing.T, start *Starter, signer testSigner, target configversion.Target) {
			encoded := published(t, signer, target, `{"log":{"level":"warn"}}`)
			tampered := strings.Replace(string(encoded), `"content":"`, `"content":"AA`, 1)
			if err := os.WriteFile(start.ArtifactPath, []byte(tampered), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"not a version at all", func(t *testing.T, start *Starter, _ testSigner, _ configversion.Target) {
			if err := os.WriteFile(start.ArtifactPath, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"absent", func(t *testing.T, start *Starter, _ testSigner, _ configversion.Target) {}},
	} {
		t.Run(item.name, func(t *testing.T) {
			start, runner, signer, target := starter(t)
			item.corrupt(t, start, signer, target)

			_, err := start.Start(context.Background())
			if !errors.Is(err, ErrUnverified) {
				t.Fatalf("a version %s started the tunnel: %v", item.name, err)
			}
			if runner.started != 0 {
				t.Fatalf("a version %s started the process %d times", item.name, runner.started)
			}
			if _, statErr := os.Stat(start.ContentPath); statErr == nil {
				t.Fatalf("a version %s was written for the process to read", item.name)
			}
			if err.Error() == ErrUnverified.Error() {
				t.Fatalf("the refusal does not say which check failed: %v", err)
			}
		})
	}
}
