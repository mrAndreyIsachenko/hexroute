// Package tunnelstart starts the tunnel process from a signed configuration
// version, and refuses to start it from anything else.
//
// The bytes sing-box runs are what carries every packet this machine sends. A
// host that ran whatever was on disk would let anyone able to write that file
// choose them; verifying once at installation would let anyone able to write it
// afterwards do the same. So it is verified at every start.
//
// Nothing here reaches the network to do it. A host needs its tunnel
// configuration exactly when it has no network, and a verification that had to
// fetch a key would fail in the one case it exists for.
package tunnelstart

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
)

var (
	ErrUnverified = errors.New("the tunnel configuration did not verify")
	ErrMisplaced  = errors.New("invalid tunnel start configuration")
)

// Runner starts a process and reports its identifier. It exists so a test can
// watch what would have been run without running it.
type Runner interface {
	Start(ctx context.Context, binary string, args ...string) (int, error)
	Stop(pid int) error
}

// Starter turns a signed version into a running tunnel.
type Starter struct {
	// ArtifactPath is the published version, on disk beside the host that runs
	// it rather than somewhere it must be fetched from.
	ArtifactPath string
	// PublicKey is pinned. A version signed by a key this host does not already
	// trust is not a version it will run, whatever else about it holds.
	PublicKey ed25519.PublicKey
	// Target is what this host is. A version meant for another host must not
	// run here merely because it verified.
	Target configversion.Target
	// ContentPath is where the verified bytes are written for the process to
	// read. It is rewritten from the artifact at every start, so a file edited
	// between starts is replaced rather than obeyed.
	ContentPath string
	Binary      string
	Runner      Runner
}

// Start verifies the version and starts the tunnel from it.
//
// The refusal names which check failed, because "it did not verify" is the
// answer that sent a reader looking at the wrong half of a system more than
// once in this repository's history.
func (starter *Starter) Start(ctx context.Context) (int, error) {
	if starter.Runner == nil || starter.Binary == "" ||
		starter.ArtifactPath == "" || starter.ContentPath == "" {
		return 0, ErrMisplaced
	}
	if !filepath.IsAbs(starter.ContentPath) {
		return 0, fmt.Errorf("%w: the content path must be absolute", ErrMisplaced)
	}
	encoded, err := os.ReadFile(starter.ArtifactPath)
	if err != nil {
		return 0, fmt.Errorf("%w: the version is not on disk: %v", ErrUnverified, err)
	}
	artifact, err := configversion.Decode(encoded)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrUnverified, configversion.Reason(err))
	}
	content, err := configversion.Verify(artifact, starter.PublicKey, starter.Target)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrUnverified, configversion.Reason(err))
	}

	if err := os.MkdirAll(filepath.Dir(starter.ContentPath), 0o700); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrMisplaced, err)
	}
	// Written whole and renamed: a process started on a half-written
	// configuration would fail in a way that looks like the configuration being
	// wrong rather than the writing being interrupted.
	staged := starter.ContentPath + ".staged"
	if err := os.WriteFile(staged, content, 0o600); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrMisplaced, err)
	}
	if err := os.Rename(staged, starter.ContentPath); err != nil {
		_ = os.Remove(staged)
		return 0, fmt.Errorf("%w: %v", ErrMisplaced, err)
	}

	return starter.Runner.Start(ctx, starter.Binary, "run", "-c", starter.ContentPath)
}

// Stop stops what Start started.
func (starter *Starter) Stop(pid int) error {
	if starter.Runner == nil {
		return ErrMisplaced
	}
	return starter.Runner.Stop(pid)
}

// ExecRunner starts the real process.
type ExecRunner struct{}

func (ExecRunner) Start(ctx context.Context, binary string, args ...string) (int, error) {
	command := exec.CommandContext(ctx, binary, args...)
	if err := command.Start(); err != nil {
		return 0, err
	}
	return command.Process.Pid, nil
}

func (ExecRunner) Stop(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}
