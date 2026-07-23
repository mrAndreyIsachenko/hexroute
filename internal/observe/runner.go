package observe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

const MaxCommandOutput = 256 * 1024

var ErrOutputTooLarge = errors.New("observation command output exceeds limit")

// Runner is intentionally output-only. Observation adapters do not receive a
// process mutation interface.
type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct {
	MaxOutput int
}

func (runner ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	limit := runner.MaxOutput
	if limit <= 0 {
		limit = MaxCommandOutput
	}

	output := &cappedBuffer{limit: limit}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	if output.overflow {
		return nil, ErrOutputTooLarge
	}
	return output.Bytes(), nil
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return originalLength, nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		value = value[:remaining]
	}
	_, _ = buffer.buffer.Write(value)
	return originalLength, nil
}

func (buffer *cappedBuffer) Bytes() []byte {
	return buffer.buffer.Bytes()
}
