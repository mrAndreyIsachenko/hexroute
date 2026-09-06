// Package pritunlclient reconnects a Pritunl session.
//
// It is the one place in this system that submits a Pritunl credential, and it
// submits it through the client's password-read path. The legacy watchdog
// concatenated the PIN and the one-time code into a command-line argument,
// where the process table exposed both to every local process for the life of
// the call.
package pritunlclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/credentials"
	"github.com/mrAndreyIsachenko/hexroute/internal/otp"
)

const (
	// minWindowSeconds is how much of a code's life must remain for submitting
	// it to be worth doing. A code that expires between being read and being
	// checked fails in a way indistinguishable from a wrong secret, which is
	// the worst diagnosis this path can produce.
	minWindowSeconds = 5
	// reconnectTimeout bounds one attempt.
	reconnectTimeout = 45 * time.Second
	// maxSubmission bounds what may be written to the client's input.
	maxSubmission = 256
)

var (
	// ErrInvalidClient is a misconfigured client.
	ErrInvalidClient = errors.New("invalid Pritunl client configuration")
	// ErrWindowTooShort is a code with too little life left to submit.
	ErrWindowTooShort = errors.New("one-time-code window is too short to submit")
	// ErrReconnect is a reconnect the client refused or could not complete. It
	// never carries the client's output: what was submitted may be echoed
	// there, and an error is the part of a failure that reaches a log.
	ErrReconnect = errors.New("Pritunl reconnect failed")
)

var (
	profilePattern = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
	modePattern    = regexp.MustCompile(`^(ovpn|wg)$`)
)

// Runner runs the client with something on its input.
//
// It exists as an interface for one reason: a test has to be able to see
// exactly what reached the arguments, the environment and the input.
type Runner interface {
	RunWithInput(ctx context.Context, input []byte, name string, args ...string) error
}

// Config is where the client is and which session it manages.
type Config struct {
	Path      string
	ProfileID string
	Mode      string
}

// Client reconnects one session.
type Client struct {
	config Config
	runner Runner
	now    func() time.Time
}

// New builds a client. Nothing is defaulted: a reconnect aimed at a profile
// nobody named is a reconnect of whatever happens to be first.
func New(config Config, runner Runner) (*Client, error) {
	if runner == nil || !validAbsolutePath(config.Path) ||
		!profilePattern.MatchString(config.ProfileID) ||
		!modePattern.MatchString(config.Mode) {
		return nil, ErrInvalidClient
	}
	return &Client{config: config, runner: runner, now: time.Now}, nil
}

// Reconnect submits the credential and asks the client to start the session.
//
// The PIN and the code are assembled in one buffer that is cleared before this
// returns, and that buffer goes to the client's input. Neither value becomes an
// argument, an environment entry, or part of an error.
func (client *Client) Reconnect(ctx context.Context, source credentials.Source) error {
	if client == nil || ctx == nil || source == nil {
		return fmt.Errorf("%w: no client", ErrInvalidClient)
	}
	at := client.now().UTC()
	remaining, err := otp.SecondsRemaining(at, otp.Period)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReconnect, err)
	}
	if remaining < minWindowSeconds {
		return fmt.Errorf("%w: %d seconds left", ErrWindowTooShort, remaining)
	}

	pritunl, err := source.ReadPritunl(ctx)
	if err != nil {
		return fmt.Errorf("%w: credentials unavailable", ErrReconnect)
	}
	defer pritunl.Close()

	submission := make([]byte, 0, maxSubmission)
	defer clear(submission)
	if err := pritunl.UsePIN(func(pin []byte) error {
		if len(pin) == 0 || len(pin) > maxSubmission/2 {
			return ErrInvalidClient
		}
		submission = append(submission, pin...)
		return nil
	}); err != nil {
		return fmt.Errorf("%w: PIN unavailable", ErrReconnect)
	}
	if err := pritunl.UseTOTPSeed(func(encoded []byte) error {
		seed, err := otp.DecodeSeed(encoded)
		if err != nil {
			return err
		}
		defer clear(seed)
		code, err := otp.Code(seed, at)
		if err != nil {
			return err
		}
		submission = append(submission, code...)
		return nil
	}); err != nil {
		return fmt.Errorf("%w: one-time code unavailable", ErrReconnect)
	}
	if len(submission) == 0 || len(submission) > maxSubmission {
		return fmt.Errorf("%w: nothing to submit", ErrReconnect)
	}

	runContext, cancel := context.WithTimeout(ctx, reconnectTimeout)
	defer cancel()
	// -r is the client's own password-read path: it takes the value from this
	// process's input instead of from an argument.
	if err := client.runner.RunWithInput(
		runContext, submission, client.config.Path,
		"start", client.config.ProfileID, "-m", client.config.Mode, "-r",
	); err != nil {
		return fmt.Errorf("%w: the client did not start the session", ErrReconnect)
	}
	return nil
}

// ExecRunner runs the real client.
type ExecRunner struct{}

// RunWithInput starts the command with the value on its input and an empty
// environment, and discards its output.
//
// The output is discarded rather than captured because the client may echo
// what it was given, and a captured echo is one careless wrapping away from an
// error message and then a log.
func (ExecRunner) RunWithInput(
	ctx context.Context,
	input []byte,
	name string,
	args ...string,
) error {
	if ctx == nil || len(input) == 0 || len(input) > maxSubmission ||
		!validAbsolutePath(name) {
		return ErrInvalidClient
	}
	for _, argument := range args {
		if strings.ContainsAny(argument, "\x00\r\n") {
			return ErrInvalidClient
		}
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = bytes.NewReader(input)
	command.Stdout = nil
	command.Stderr = nil
	command.Env = []string{}
	return command.Run()
}

func validAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n") && net.ParseIP(path) == nil
}
