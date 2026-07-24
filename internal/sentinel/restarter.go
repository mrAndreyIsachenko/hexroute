package sentinel

import (
	"context"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const (
	launchctlCommand  = "/bin/launchctl"
	rootLaunchdTarget = "system/com.hexroute.observe.hexrouted"
)

type MacOSRootRestarter struct {
	runner observe.Runner
}

func NewMacOSRootRestarter(runner observe.Runner) (*MacOSRootRestarter, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}
	return &MacOSRootRestarter{runner: runner}, nil
}

func (restarter *MacOSRootRestarter) RestartHexrouted(ctx context.Context) error {
	if restarter == nil || restarter.runner == nil {
		return ErrInvalidRecovery
	}
	_, err := restarter.runner.Output(
		ctx,
		launchctlCommand,
		"kickstart",
		"-k",
		rootLaunchdTarget,
	)
	return err
}
