package observe

import (
	"bufio"
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

// LineReader is a runner that can read a listing without holding all of it, so
// that a listing's size cannot refuse an observation.
//
// It exists because the cap above refused one. The cap bounds memory, which is
// right, but refusing the whole listing when it is reached makes an observation
// impossible rather than expensive — and the listing in question is the process
// table, which any user on the machine can enlarge at will. Measured
// 2026-10-05, it passed 256 KiB and this runtime could neither find its tunnel
// nor declare it absent for twenty minutes, while holding the claim that kept
// the previous owner from starting one.
//
// A line longer than the keeper's own bound is skipped rather than refused, for
// the same reason: somebody else's arguments are not this observation's
// problem. Measured 2026-09-14, two unrelated processes with command lines of
// 5,758 and 6,109 bytes refused the whole listing once already.
type LineReader interface {
	// OutputLines runs the command and returns only the lines keep answered
	// for, joined as the command printed them.
	OutputLines(
		ctx context.Context,
		keep func(line []byte) bool,
		name string,
		args ...string,
	) ([]byte, error)
}

// MaxObservationLine is the longest line an observation keeps. It is far past
// any command line this runtime looks for and far short of what a listing can
// hold.
const MaxObservationLine = 64 * 1024

func (runner ExecRunner) OutputLines(
	ctx context.Context,
	keep func(line []byte) bool,
	name string,
	args ...string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Stderr = io.Discard
	pipe, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	kept := &bytes.Buffer{}
	reader := bufio.NewReaderSize(pipe, 64*1024)
	overflow := false
	for {
		line, err := readLine(reader)
		if len(line) > 0 && keep(line) {
			if kept.Len()+len(line)+1 > runner.lineLimit() {
				// Refused rather than truncated. A listing with lines missing
				// can answer that something is absent when it is there, and
				// this observation's two answers — running and absent — are
				// what a cause is decided from. Filtering is what keeps this
				// out of reach; reaching it anyway is not an answer.
				overflow = true
			} else {
				kept.Write(line)
				kept.WriteByte('\n')
			}
		}
		if err != nil {
			break
		}
	}
	// Drained before waiting, so the command is never blocked on a pipe nobody
	// is reading.
	_, _ = io.Copy(io.Discard, pipe)
	if err := command.Wait(); err != nil {
		return nil, err
	}
	if overflow {
		return nil, ErrOutputTooLarge
	}
	return kept.Bytes(), nil
}

func (runner ExecRunner) lineLimit() int {
	if runner.MaxOutput > 0 {
		return runner.MaxOutput
	}
	return MaxCommandOutput
}

// readLine returns one line without its terminator, skipping a line longer than
// what an observation keeps rather than refusing the listing it is part of.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) <= MaxObservationLine {
			line = append(line, chunk...)
		} else {
			line = line[:0]
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimRight(line, "\n"), err
	}
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
