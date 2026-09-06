package ingressagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// configPermissions keeps the runtime configuration readable by the service
// user and nothing wider. The file names transports and their parameters.
const configPermissions = 0o640

// FileApplier writes the configuration where the runtime reads it and asks the
// runtime to use it.
//
// Writing and reloading are one step here because a configuration on disk that
// the running process has not read is neither the old version nor the new one,
// and nothing downstream could tell which is in service.
type FileApplier struct {
	path    string
	command string
	args    []string
}

// NewFileApplier builds an applier. The reload command is given rather than
// discovered: a host that guessed at how to restart its own runtime would be
// guessing about the only step that makes a version take effect.
func NewFileApplier(path string, command string, args []string) (*FileApplier, error) {
	if !validAbsolutePath(path) || !validAbsolutePath(command) {
		return nil, fmt.Errorf("%w: applier paths", ErrAgent)
	}
	return &FileApplier{
		path:    path,
		command: command,
		args:    append([]string(nil), args...),
	}, nil
}

// Apply replaces the configuration and reloads the runtime.
func (applier *FileApplier) Apply(ctx context.Context, content []byte) error {
	if applier == nil || ctx == nil || len(content) == 0 {
		return fmt.Errorf("%w: nothing to apply", ErrApply)
	}
	if err := writeAtomic(applier.path, content, configPermissions); err != nil {
		return fmt.Errorf("%w: %w", ErrApply, err)
	}
	command := exec.CommandContext(ctx, applier.command, applier.args...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	// The environment is emptied rather than inherited: what reloads a
	// transport should not depend on what happened to be set when the timer
	// fired.
	command.Env = []string{}
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: %w", ErrApply, err)
	}
	return nil
}

func writeAtomic(path string, content []byte, permissions os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() {
		temporary.Close()
		_ = os.Remove(name)
	}()
	if err := temporary.Chmod(permissions); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
