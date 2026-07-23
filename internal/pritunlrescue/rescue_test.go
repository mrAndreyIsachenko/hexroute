package pritunlrescue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

type fakeVerifier struct {
	generation uint64
	outer      bool
	stale      bool
	err        error
	outerCalls uint32
	staleCalls uint32
}

func (verifier *fakeVerifier) Generation() uint64 {
	return verifier.generation
}

func (verifier *fakeVerifier) OuterReady(context.Context) (bool, error) {
	verifier.outerCalls++
	return verifier.outer, verifier.err
}

func (verifier *fakeVerifier) PritunlServiceStale(context.Context) (bool, error) {
	verifier.staleCalls++
	return verifier.stale, verifier.err
}

func TestRequestIsTypedAndCredentialFree(t *testing.T) {
	request, err := NewRequest("rescue-request-01", 17)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}
	if request.Action != ipc.ActionRescuePritunlService ||
		request.Target != control.ComponentPritunl ||
		request.ExpectedGeneration != 17 {
		t.Fatalf("NewRequest() = %+v", request)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	normalized := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"pin", "totp", "otp", "seed", "profile"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("request JSON contains forbidden credential field %q", forbidden)
		}
	}
}

func TestHandlerApprovesOnlyRevalidatedPritunlRestart(t *testing.T) {
	verifier := &fakeVerifier{
		generation: 17,
		outer:      true,
		stale:      true,
	}
	handler, err := NewHandler(501, verifier)
	if err != nil {
		t.Fatalf("NewHandler() error: %v", err)
	}
	request, _ := NewRequest("rescue-request-01", 17)

	decision, err := handler.Evaluate(context.Background(), 501, request)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !decision.Approved ||
		decision.Action.Kind != control.ActionRestart ||
		decision.Action.Target != control.TargetPritunlService ||
		decision.Action.Generation != 17 {
		t.Fatalf("Evaluate() = %+v", decision)
	}
	if verifier.outerCalls != 1 || verifier.staleCalls != 1 {
		t.Fatalf(
			"verification calls outer=%d stale=%d",
			verifier.outerCalls,
			verifier.staleCalls,
		)
	}
}

func TestHandlerRejectsUIDAndGenerationBeforeRootProbes(t *testing.T) {
	tests := []struct {
		name       string
		peerUID    uint32
		generation uint64
		want       error
	}{
		{
			name:       "unexpected UID",
			peerUID:    502,
			generation: 17,
			want:       ipc.ErrUnauthorizedPeer,
		},
		{
			name:       "stale generation",
			peerUID:    501,
			generation: 16,
			want:       control.ErrStaleGeneration,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &fakeVerifier{
				generation: 17,
				outer:      true,
				stale:      true,
			}
			handler, _ := NewHandler(501, verifier)
			request, _ := NewRequest("rescue-request-01", test.generation)

			if _, err := handler.Evaluate(
				context.Background(),
				test.peerUID,
				request,
			); !errors.Is(err, test.want) {
				t.Fatalf("Evaluate() error = %v, want %v", err, test.want)
			}
			if verifier.outerCalls != 0 || verifier.staleCalls != 0 {
				t.Fatalf(
					"rejected request ran probes outer=%d stale=%d",
					verifier.outerCalls,
					verifier.staleCalls,
				)
			}
		})
	}
}

func TestHandlerRequiresOuterReadinessAndConfirmedStaleService(t *testing.T) {
	tests := []struct {
		name           string
		outer          bool
		stale          bool
		wantStaleCalls uint32
	}{
		{
			name:           "outer not ready",
			outer:          false,
			stale:          true,
			wantStaleCalls: 0,
		},
		{
			name:           "service not stale",
			outer:          true,
			stale:          false,
			wantStaleCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &fakeVerifier{
				generation: 17,
				outer:      test.outer,
				stale:      test.stale,
			}
			handler, _ := NewHandler(501, verifier)
			request, _ := NewRequest("rescue-request-01", 17)

			decision, err := handler.Evaluate(context.Background(), 501, request)
			if !errors.Is(err, ErrPrecondition) || decision.Approved {
				t.Fatalf("Evaluate() = %+v, %v", decision, err)
			}
			if verifier.outerCalls != 1 || verifier.staleCalls != test.wantStaleCalls {
				t.Fatalf(
					"verification calls outer=%d stale=%d",
					verifier.outerCalls,
					verifier.staleCalls,
				)
			}
		})
	}
}

func TestHandlerRejectsOtherAllowlistedAction(t *testing.T) {
	verifier := &fakeVerifier{generation: 17, outer: true, stale: true}
	handler, _ := NewHandler(501, verifier)
	request := ipc.Request{
		Version:            ipc.ProtocolVersion,
		RequestID:          "status-request-01",
		Action:             ipc.ActionStatus,
		ExpectedGeneration: 17,
	}

	if _, err := handler.Evaluate(context.Background(), 501, request); !errors.Is(
		err,
		ErrInvalidRequest,
	) {
		t.Fatalf("Evaluate() error = %v, want %v", err, ErrInvalidRequest)
	}
	if verifier.outerCalls != 0 || verifier.staleCalls != 0 {
		t.Fatal("wrong action ran Pritunl probes")
	}
}
