package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingRunner struct {
	path string
	args []string
	err  error
}

func (runner *recordingRunner) Run(
	_ context.Context,
	path string,
	args ...string,
) error {
	runner.path = path
	runner.args = append([]string(nil), args...)
	return runner.err
}

func TestMacOSNotifierUsesFixedScriptAndContentWithoutShell(t *testing.T) {
	runner := &recordingRunner{}
	notifier, err := newMacOSNotifier(runner)
	if err != nil {
		t.Fatalf("NewMacOSNotifier() error: %v", err)
	}
	if err := notifier.Deliver(
		context.Background(),
		TemplatePritunlSafeMode,
	); err != nil {
		t.Fatalf("Deliver() error: %v", err)
	}
	if runner.path != osascriptPath ||
		len(runner.args) != 4 ||
		runner.args[0] != "-e" ||
		runner.args[1] != notificationScript ||
		runner.args[2] != "Hexroute: Pritunl paused" {
		t.Fatalf("runner path=%q args=%q", runner.path, runner.args)
	}
	joined := strings.Join(runner.args, " ")
	for _, forbidden := range []string{
		"HEXROUTE_CANARY_TOTP_SEED",
		"incident-synthetic",
		"profile_id",
		"server_name",
		"sh -c",
		"bash -c",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("notification arguments contain %q: %q", forbidden, joined)
		}
	}
}

func TestMacOSNotifierReturnsOnlyGenericDeliveryError(t *testing.T) {
	canary := "HEXROUTE_CANARY_REALITY_PRIVATE_KEY"
	runner := &recordingRunner{err: errors.New(canary)}
	notifier, err := newMacOSNotifier(runner)
	if err != nil {
		t.Fatalf("NewMacOSNotifier() error: %v", err)
	}
	err = notifier.Deliver(context.Background(), TemplateExternalPending)
	if !errors.Is(err, ErrNotificationDelivery) ||
		strings.Contains(err.Error(), canary) {
		t.Fatalf("Deliver() error = %v", err)
	}
}

func TestEveryTemplateHasFixedContent(t *testing.T) {
	templates := []Template{
		TemplateAccessContinuity,
		TemplatePritunlSafeMode,
		TemplateTelegramCluster,
		TemplateSecurityFailure,
		TemplateRuntimeFailure,
		TemplateExternalPending,
	}
	for _, template := range templates {
		title, body, ok := content(template)
		if !ok || title == "" || body == "" {
			t.Fatalf("content(%q) = %q %q %t", template, title, body, ok)
		}
	}
	if _, _, ok := content(Template("raw")); ok {
		t.Fatal("unknown template accepted")
	}
}
