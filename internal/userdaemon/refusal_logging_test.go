package userdaemon

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan"
)

// A refusal must be reportable.
//
// outcomeResult turns a refusal into ResultRejected, and the logger refuses a
// rejected event that carries no reason — "rejected events require exactly one
// reason". The call site passed none, so writing down that root had said no
// returned an error, the observe loop returned it, and the daemon exited.
//
// It was unreachable until a service that is gone became a reason to ask: the
// only earlier route to a rescue request was a session reporting itself
// connected while carrying no traffic. Inducing the documented precondition on
// 2026-09-09 reached it, root refused, and the daemon restarted every forty
// seconds for as long as the service stayed down — the supervisor dying on the
// one outcome it exists to record.
func TestARefusedRecoveryCanBeWrittenDown(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		outcome recoveryOutcome
		result  string
	}{
		{name: "root refused the request", outcome: recoveryRefused, result: "rejected"},
		{name: "the act failed", outcome: recoveryFailed, result: "degraded"},
		{name: "nothing is equipped to act", outcome: recoveryUnequipped, result: "degraded"},
		{name: "the act was performed", outcome: recoveryDone, result: "ok"},
		{name: "it is only proposed", outcome: recoveryProposed, result: "proposed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			logger, err := logging.New(out, logging.ComponentUser)
			if err != nil {
				t.Fatalf("logger: %v", err)
			}
			gate := logging.NewChangeGate()
			summary := Summary{
				Outcome: testCase.outcome,
				Plan: pritunlplan.Plan{
					State:  control.StateDegraded,
					Action: pritunlplan.ActionRequestRescue,
					Reason: pritunlplan.ReasonServiceNotRunning,
				},
				Failures: 1,
			}

			if err := emitSummary(logger, gate, summary); err != nil {
				t.Fatalf("recording outcome %q returned %v — the observe loop "+
					"returns this, and the daemon exits", testCase.outcome, err)
			}

			var found bool
			for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				var event map[string]any
				if line == "" || json.Unmarshal([]byte(line), &event) != nil {
					continue
				}
				if event["event"] != string(logging.EventPritunlReconnect) {
					continue
				}
				found = true
				if event["result"] != testCase.result {
					t.Fatalf("result = %v, want %q", event["result"], testCase.result)
				}
				if event["result"] == "rejected" && event["reason"] == "" {
					t.Fatal("a rejected event was written with no reason")
				}
			}
			if !found {
				t.Fatal("the outcome was not recorded at all")
			}
		})
	}
}
