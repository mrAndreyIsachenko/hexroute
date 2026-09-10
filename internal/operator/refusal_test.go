package operator

import (
	"context"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

type recordingRefusals struct {
	mutationRefused int
	unanswered      int
}

func (recorder *recordingRefusals) ReportMutationRefused()   { recorder.mutationRefused++ }
func (recorder *recordingRefusals) ReportRequestUnanswered() { recorder.unanswered++ }

// A refusal made above the act is still a refusal, and until this test existed
// the only two that are made there left nothing behind. A rescue refused on
// this basis on 2026-09-10 could not be attributed from either runtime's log.
func TestMutationGateRecordsItsRefusal(t *testing.T) {
	for _, action := range []ipc.Action{
		ipc.ActionRescuePritunlService,
		ipc.ActionResumeTarget,
	} {
		t.Run(string(action), func(t *testing.T) {
			mutating := &countingMutationHandler{}
			refusals := &recordingRefusals{}
			dispatcher, err := NewDispatcher(
				&countingReadHandler{},
				mutating,
				&countingPolicyHandler{allowed: false},
				refusals,
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("NewDispatcher() error: %v", err)
			}
			response := dispatcher.HandleIPC(context.Background(), ipc.Request{
				Version:   ipc.ProtocolVersion,
				RequestID: "11111111-1111-4111-8111-111111111111",
				Action:    action,
				Target:    control.ComponentPritunl,
			})
			if response.Error != ipc.ErrorPrecondition {
				t.Fatalf("error = %q, want %q", response.Error, ipc.ErrorPrecondition)
			}
			if mutating.calls != 0 {
				t.Fatalf("the act was evaluated behind a closed gate")
			}
			if refusals.mutationRefused != 1 {
				t.Fatalf(
					"the gate refused and recorded %d times, want 1",
					refusals.mutationRefused,
				)
			}
		})
	}
}

// A request nobody took up was not refused by anything. It has to be told from
// a refusal, and the runtime that failed to answer is the one that knows.
func TestUnansweredRequestIsRecorded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	refusals := &recordingRefusals{}
	broker, err := NewBroker(context.Background(), refusals)
	if err != nil {
		t.Fatalf("NewBroker() error: %v", err)
	}
	// Nothing reads Requests(), which is the runtime busy elsewhere.
	response := broker.HandleIPC(ctx, ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "22222222-2222-4222-8222-222222222222",
		Action:    ipc.ActionRescuePritunlService,
		Target:    control.ComponentPritunl,
	})
	if response.Error != ipc.ErrorInternal {
		t.Fatalf("error = %q, want %q", response.Error, ipc.ErrorInternal)
	}
	if refusals.unanswered != 1 {
		t.Fatalf(
			"an unanswered request was recorded %d times, want 1",
			refusals.unanswered,
		)
	}
	if refusals.mutationRefused != 0 {
		t.Fatalf("an unanswered request was recorded as a refusal")
	}
}

// The reply side has the same hole: an envelope taken up and then abandoned is
// as unanswered as one never taken.
func TestAbandonedEnvelopeIsRecorded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	refusals := &recordingRefusals{}
	broker, err := NewBroker(context.Background(), refusals)
	if err != nil {
		t.Fatalf("NewBroker() error: %v", err)
	}
	go func() {
		// Taken up and never answered.
		<-broker.Requests()
	}()
	response := broker.HandleIPC(ctx, ipc.Request{
		Version:   ipc.ProtocolVersion,
		RequestID: "33333333-3333-4333-8333-333333333333",
		Action:    ipc.ActionRescuePritunlService,
		Target:    control.ComponentPritunl,
	})
	if response.Error != ipc.ErrorInternal {
		t.Fatalf("error = %q, want %q", response.Error, ipc.ErrorInternal)
	}
	if refusals.unanswered != 1 {
		t.Fatalf(
			"an abandoned envelope was recorded %d times, want 1",
			refusals.unanswered,
		)
	}
}

func TestDispatcherRefusesToBuildWithoutSomewhereToWrite(t *testing.T) {
	if _, err := NewDispatcher(
		&countingReadHandler{},
		&countingMutationHandler{},
		&countingPolicyHandler{allowed: true},
		nil,
		nil,
		nil,
	); err == nil {
		t.Fatal("NewDispatcher() built a dispatcher that can refuse silently")
	}
	if _, err := NewBroker(context.Background(), nil); err == nil {
		t.Fatal("NewBroker() built a broker that can fail silently")
	}
}
