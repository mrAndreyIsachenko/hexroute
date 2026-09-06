package ingressagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// configPermissions keeps the runtime configuration readable by the service
// user and nothing wider. The file names transports and their parameters.
const configPermissions = 0o640

// generationPermissions leave the generation world readable: the observer runs
// as its own user and has to read it, and the version label a host is running
// is not a secret from anything already on the host.
const generationPermissions = 0o644

// generationValue is what may be written as a generation. It is the observer's
// own reference shape, so a label this host applies is a label the observer
// can report.
var generationValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// FileApplier writes the configuration where the runtime reads it and asks the
// runtime to use it.
//
// Writing and reloading are one step here because a configuration on disk that
// the running process has not read is neither the old version nor the new one,
// and nothing downstream could tell which is in service.
type FileApplier struct {
	path           string
	generationPath string
	command        string
	args           []string
}

// NewFileApplier builds an applier. The reload command is given rather than
// discovered: a host that guessed at how to restart its own runtime would be
// guessing about the only step that makes a version take effect.
func NewFileApplier(
	path string,
	generationPath string,
	command string,
	args []string,
) (*FileApplier, error) {
	if !validAbsolutePath(path) || !validAbsolutePath(generationPath) ||
		!validAbsolutePath(command) || path == generationPath {
		return nil, fmt.Errorf("%w: applier paths", ErrAgent)
	}
	return &FileApplier{
		path:           path,
		generationPath: generationPath,
		command:        command,
		args:           append([]string(nil), args...),
	}, nil
}

// Apply replaces the configuration, records which version it is, and reloads
// the runtime.
//
// The generation is written before the configuration. A host that crashed
// between the two would report a version it is not yet running, which is
// visible as a failure to prove; the reverse would report the version it was
// running before, which would prove the wrong thing.
func (applier *FileApplier) Apply(ctx context.Context, content []byte, generation string) error {
	if applier == nil || ctx == nil || len(content) == 0 ||
		!generationValue.MatchString(generation) {
		return fmt.Errorf("%w: nothing to apply", ErrApply)
	}
	if err := writeAtomic(applier.generationPath, []byte(generation+"\n"), generationPermissions); err != nil {
		return fmt.Errorf("%w: %w", ErrApply, err)
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
