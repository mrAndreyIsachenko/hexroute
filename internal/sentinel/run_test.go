package sentinel

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

type fixedCycler struct {
	summary Summary
}

func (cycler fixedCycler) Observe(context.Context, control.Tick) Summary {
	return cycler.summary
}

func TestRunCheckValidatesObserveOnlyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sentinel-observe.json")
	if err := os.WriteFile(path, []byte(validConfig), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--check", "--config", path}, &stdout, &stderr)
	if code != 0 ||
		stderr.Len() != 0 ||
		!strings.Contains(stdout.String(), `"event":"startup_check"`) ||
		!strings.Contains(stdout.String(), `"mutation_authority":"none"`) {
		t.Fatalf(
			"Run() code=%d stdout=%q stderr=%q",
			code,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestObserveLoopReportsEvidenceWithoutRestartProposal(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentSentinel)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	cycler := fixedCycler{summary: Summary{
		HeartbeatFound: true,
		Decision: Decision{
			ObserveOnly:    true,
			HeartbeatStale: true,
			DataPathBroken: true,
			EvidenceReady:  true,
			Action:         ActionNone,
		},
	}}

	if err := observeLoop(
		context.Background(),
		time.Minute,
		true,
		cycler,
		logger,
	); err != nil {
		t.Fatalf("observeLoop() error: %v", err)
	}
	logged := output.String()
	if !strings.Contains(logged, `"event":"sentinel_restart_evidence"`) ||
		!strings.Contains(logged, `"result":"reported"`) ||
		strings.Contains(logged, `"result":"proposed"`) {
		t.Fatalf("observeLoop() output = %q", logged)
	}
}
