package notification

import (
	"context"
	"errors"
	"os/exec"
)

const (
	osascriptPath      = "/usr/bin/osascript"
	notificationScript = `on run argv
	if (count of argv) is not 2 then error "invalid notification"
	display notification (item 2 of argv) with title (item 1 of argv)
end run`
)

type commandRunner interface {
	Run(context.Context, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, path string, args ...string) error {
	if ctx == nil || path != osascriptPath {
		return ErrNotificationDelivery
	}
	command := exec.CommandContext(ctx, path, args...)
	if err := command.Run(); err != nil {
		return ErrNotificationDelivery
	}
	return nil
}

type MacOSNotifier struct {
	runner commandRunner
}

var ErrNotificationDelivery = errors.New("local notification delivery failed")

func NewDefaultMacOSNotifier() (*MacOSNotifier, error) {
	return newMacOSNotifier(execRunner{})
}

func newMacOSNotifier(runner commandRunner) (*MacOSNotifier, error) {
	if runner == nil {
		return nil, ErrNotificationDelivery
	}
	return &MacOSNotifier{runner: runner}, nil
}

func (notifier *MacOSNotifier) Deliver(
	ctx context.Context,
	template Template,
) error {
	if notifier == nil || notifier.runner == nil || ctx == nil {
		return ErrNotificationDelivery
	}
	title, body, ok := content(template)
	if !ok {
		return ErrNotificationDelivery
	}
	if err := notifier.runner.Run(
		ctx,
		osascriptPath,
		"-e",
		notificationScript,
		title,
		body,
	); err != nil {
		return ErrNotificationDelivery
	}
	return nil
}

func content(template Template) (string, string, bool) {
	switch template {
	case TemplateAccessContinuity:
		return "Hexroute: action required",
			"Access continuity is degraded. Inspect local Hexroute status.",
			true
	case TemplatePritunlSafeMode:
		return "Hexroute: Pritunl paused",
			"Automatic Pritunl recovery entered safe mode. Inspect before resuming.",
			true
	case TemplateTelegramCluster:
		return "Hexroute: Telegram unavailable",
			"All Telegram proxy paths are unavailable. External alert delivery is pending.",
			true
	case TemplateSecurityFailure:
		return "Hexroute: security check failed",
			"A local safety validation failed. Inspect local Hexroute diagnostics.",
			true
	case TemplateRuntimeFailure:
		return "Hexroute: runtime attention required",
			"A critical local runtime incident requires inspection.",
			true
	case TemplateExternalPending:
		return "Hexroute: external alert pending",
			"A critical incident cannot reach an external alert path. Inspect local status.",
			true
	default:
		return "", "", false
	}
}
