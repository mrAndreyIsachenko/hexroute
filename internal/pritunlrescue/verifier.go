package pritunlrescue

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const launchctlCommand = "/bin/launchctl"

// ErrInvalidVerifier is a verifier that could not be built.
var ErrInvalidVerifier = errors.New("invalid Pritunl rescue verifier")

// serviceLabel is what may be asked about. It is narrow because the label ends
// up in an argument to launchctl, and because a rescue is for one named
// service rather than for whatever the caller can spell.
var serviceLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// LaunchdVerifier answers the root runtime's own questions about a rescue.
//
// It is the implementation the contract was written for and never had. The
// point of it is that root does not take the requester's word: the request
// says a service is stale, and this looks.
type LaunchdVerifier struct {
	runner     observe.Runner
	label      string
	generation func() uint64
	outerReady func(context.Context) (bool, error)
}

// NewLaunchdVerifier builds a verifier for one named service.
//
// Generation and outer readiness come from the root runtime, which already
// computes both; staleness is measured here, because nothing else measures it.
func NewLaunchdVerifier(
	runner observe.Runner,
	label string,
	generation func() uint64,
	outerReady func(context.Context) (bool, error),
) (*LaunchdVerifier, error) {
	if runner == nil || generation == nil || outerReady == nil ||
		!serviceLabel.MatchString(label) {
		return nil, ErrInvalidVerifier
	}
	return &LaunchdVerifier{
		runner: runner, label: label,
		generation: generation, outerReady: outerReady,
	}, nil
}

// Generation is the control-state generation the root runtime is on.
func (verifier *LaunchdVerifier) Generation() uint64 {
	if verifier == nil || verifier.generation == nil {
		return 0
	}
	return verifier.generation()
}

// OuterReady is whether the outer path is usable.
func (verifier *LaunchdVerifier) OuterReady(ctx context.Context) (bool, error) {
	if verifier == nil || verifier.outerReady == nil || ctx == nil {
		return false, ErrInvalidVerifier
	}
	return verifier.outerReady(ctx)
}

// PritunlServiceStale reports whether the service is loaded and not running.
//
// Loaded and not running is the whole definition, and it is deliberately
// narrow. A service that is running badly is not something a restart is known
// to fix, and a service that is not loaded at all is not this runtime's to
// install. Anything the probe cannot read is reported as not stale: refusing to
// restart on an unreadable answer costs a recovery, and restarting on one costs
// a session that was working.
func (verifier *LaunchdVerifier) PritunlServiceStale(ctx context.Context) (bool, error) {
	if verifier == nil || verifier.runner == nil || ctx == nil {
		return false, ErrInvalidVerifier
	}
	output, err := verifier.runner.Output(
		ctx, launchctlCommand, "print", "system/"+verifier.label,
	)
	if err != nil {
		// launchctl exits non-zero for a service that is not loaded. That is
		// not staleness, and it is not this runtime's problem to solve.
		return false, nil
	}
	return loadedAndNotRunning(string(output)), nil
}

// loadedAndNotRunning reads the one field that answers the question.
//
// Stale is exactly "not running", and not "anything other than running". A
// service that is starting says so in its own words, and restarting one
// mid-start interrupts the recovery already under way.
func loadedAndNotRunning(output string) bool {
	if len(output) == 0 || len(output) > 1<<20 {
		return false
	}
	for _, line := range strings.Split(output, "\n") {
		field := strings.TrimSpace(line)
		if state, ok := strings.CutPrefix(field, "state = "); ok {
			return strings.TrimSpace(state) == "not running"
		}
	}
	return false
}

var _ RootVerifier = (*LaunchdVerifier)(nil)
