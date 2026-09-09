package rootdaemon

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue"
)

// A refusal says which side of the boundary refused it.
//
// Six different outcomes arrived as precondition_failed: no handler, an invalid
// request, an unauthorized peer, a generation that is not the one in force, an
// outer path that is not ready, a service that is not stale, and no signed
// authority. The user runtime could record only that it was refused, and on
// 2026-09-09 finding out why meant reading the source.
//
// The protocol already has codes for most of these. Using them costs nothing
// and puts the answer in a log the operator can read without a password.
func TestRootNamesWhatItRefused(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		evaluate error
		want     ipc.ErrorCode
	}{
		{
			name:     "the generation is not the one in force",
			evaluate: control.ErrStaleGeneration,
			want:     ipc.ErrorStaleGeneration,
		},
		{
			name:     "the outer path is not ready",
			evaluate: pritunlrescue.ErrOuterNotReady,
			want:     ipc.ErrorPrecondition,
		},
		{
			name:     "the service is not stale",
			evaluate: pritunlrescue.ErrServiceNotStale,
			want:     ipc.ErrorPrecondition,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// Through the handler, not the helper: the mapping is only worth
			// anything if the response actually carries it.
			rescuer := &pritunlRescuer{
				handler: handlerRefusing(t, testCase.evaluate),
				peerUID: 501,
				authorize: func(uint64, string) policy.ActionAuthorizationDecision {
					return policy.ActionAuthorizationDecision{Allowed: true}
				},
			}
			got := rescuer.handle(context.Background(), rescueRequest(t)).Error
			if got != testCase.want {
				t.Fatalf("error code = %q, want %q", got, testCase.want)
			}
		})
	}
}

// The two preconditions share one code, so the reason is the only thing that
// can tell them apart — and on 2026-09-09 telling them apart was the whole
// question: root refused, and whether it was the outer path or the service's
// state took reading the source to find out.
func TestThePreconditionsStayDistinct(t *testing.T) {
	outer := rescueEvaluationReason(pritunlrescue.ErrOuterNotReady)
	stale := rescueEvaluationReason(pritunlrescue.ErrServiceNotStale)
	if outer == stale {
		t.Fatalf("both preconditions record %q; the code cannot tell them "+
			"apart either, so nothing can", outer)
	}
	if outer == "" || stale == "" {
		t.Fatal("a precondition was recorded with no reason")
	}
}

var _ = errors.Is
var _ = policy.ActionAuthorizationDecision{}
var _ = context.Background

// A refusal on authority reaches the response as one, through the handler.
func TestAnUnsignedActIsRefusedOnAuthority(t *testing.T) {
	rescuer := &pritunlRescuer{
		handler: handlerRefusing(t, nil),
		peerUID: 501,
		authorize: func(uint64, string) policy.ActionAuthorizationDecision {
			return policy.ActionAuthorizationDecision{Allowed: false}
		},
	}
	response := rescuer.handle(context.Background(), rescueRequest(t))
	if response.Error != ipc.ErrorUnauthorized {
		t.Fatalf("an unsigned act was refused as %q, want %q",
			response.Error, ipc.ErrorUnauthorized)
	}
}

func rescueRequest(t *testing.T) ipc.Request {
	t.Helper()
	request, err := pritunlrescue.NewRequest(
		"11111111-1111-4111-8111-111111111111", 4)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return request
}

// handlerRefusing builds a real handler whose verifier produces the named
// refusal, or approves when there is none to produce.
func handlerRefusing(t *testing.T, refusal error) *pritunlrescue.Handler {
	t.Helper()
	verifier := &refusingVerifier{generation: 4, outerReady: true, stale: true}
	switch {
	case errors.Is(refusal, control.ErrStaleGeneration):
		verifier.generation = 5
	case errors.Is(refusal, pritunlrescue.ErrOuterNotReady):
		verifier.outerReady = false
	case errors.Is(refusal, pritunlrescue.ErrServiceNotStale):
		verifier.stale = false
	}
	handler, err := pritunlrescue.NewHandler(501, verifier)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

type refusingVerifier struct {
	generation uint64
	outerReady bool
	stale      bool
}

func (v *refusingVerifier) Generation() uint64 { return v.generation }

func (v *refusingVerifier) OuterReady(context.Context) (bool, error) {
	return v.outerReady, nil
}

func (v *refusingVerifier) PritunlServiceStale(context.Context) (bool, error) {
	return v.stale, nil
}

// The ground root gave is written down here, because the code cannot carry it.
//
// Both preconditions cross the boundary as one code. Which of them refused is
// knowable only on this side, and on 2026-09-09 it was the question that
// mattered: root said no, and finding out whether it was the outer path or the
// service's state took reading the source.
func TestRootWritesDownWhichPreconditionRefused(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		refusal error
		reason  string
	}{
		{
			name:    "the outer path",
			refusal: pritunlrescue.ErrOuterNotReady,
			reason:  "outer_path_not_ready",
		},
		{
			name:    "the service's state",
			refusal: pritunlrescue.ErrServiceNotStale,
			reason:  "service_not_stale",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var recorded logging.Reason
			rescuer := &pritunlRescuer{
				handler: handlerRefusing(t, testCase.refusal),
				peerUID: 501,
				authorize: func(uint64, string) policy.ActionAuthorizationDecision {
					return policy.ActionAuthorizationDecision{Allowed: true}
				},
				refusal: func(reason logging.Reason) { recorded = reason },
			}
			response := rescuer.handle(context.Background(), rescueRequest(t))
			if response.Error != ipc.ErrorPrecondition {
				t.Fatalf("code = %q, want %q", response.Error, ipc.ErrorPrecondition)
			}
			if string(recorded) != testCase.reason {
				t.Fatalf("recorded %q, want %q", recorded, testCase.reason)
			}
		})
	}
}
