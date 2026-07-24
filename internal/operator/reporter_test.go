package operator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

func TestRejectionLoggerUsesAllowlistedReasonWithoutErrorText(t *testing.T) {
	var output bytes.Buffer
	logger, err := logging.New(&output, logging.ComponentDaemon)
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	reporter, err := NewRejectionLogger(logger)
	if err != nil {
		t.Fatalf("NewRejectionLogger() error: %v", err)
	}

	reporter.ReportIPCRejection(ipc.ErrUnauthorizedPeer)
	reporter.ReportIPCRejection(ipc.ErrFrameTooLarge)
	reporter.ReportIPCRejection(
		assertSecretError("HEXROUTE_CANARY_VLESS_NOT_UUID"),
	)

	logged := output.String()
	if !strings.Contains(logged, `"reason":"unauthorized_peer"`) ||
		!strings.Contains(logged, `"reason":"oversized_request"`) ||
		!strings.Contains(logged, `"reason":"malformed_request"`) ||
		strings.Contains(logged, "HEXROUTE_CANARY_VLESS_NOT_UUID") {
		t.Fatalf("rejection log = %q", logged)
	}
}

type assertSecretError string

func (err assertSecretError) Error() string {
	return string(err)
}
