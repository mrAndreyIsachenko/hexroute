package pritunlrescue

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

// Every refusal names the check that made it.
//
// Two different preconditions returned one error: the outer path not being
// ready and the service not being stale. Root then collapsed those, the
// unauthorized peer, the stale generation and the invalid request into a single
// precondition_failed, and the user runtime could record only that it had been
// refused.
//
// On 2026-09-09 that cost the diagnosis: root refused a rescue and finding out
// why meant reading the source, because neither log named a cause.
func TestEachRefusalNamesItsOwnCheck(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		verifier *stubVerifier
		want     error
	}{
		{
			name:     "the outer path is not ready",
			verifier: &stubVerifier{generation: 4, outerReady: false, stale: true},
			want:     ErrOuterNotReady,
		},
		{
			name:     "the service is not stale",
			verifier: &stubVerifier{generation: 4, outerReady: true, stale: false},
			want:     ErrServiceNotStale,
		},
		{
			name:     "the generation is not the one in force",
			verifier: &stubVerifier{generation: 5, outerReady: true, stale: true},
			want:     control.ErrStaleGeneration,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler, err := NewHandler(501, testCase.verifier)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			request, err := NewRequest("11111111-1111-4111-8111-111111111111", 4)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_, err = handler.Evaluate(context.Background(), 501, request)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Evaluate() = %v, want %v", err, testCase.want)
			}
		})
	}
}

// The two preconditions stay preconditions.
//
// Naming them must not lose what they have in common: a caller that only wants
// to know whether the situation was refused rather than which check refused it
// keeps working.
func TestANamedPreconditionIsStillAPrecondition(t *testing.T) {
	for _, err := range []error{ErrOuterNotReady, ErrServiceNotStale} {
		if !errors.Is(err, ErrPrecondition) {
			t.Fatalf("%v is not a precondition failure", err)
		}
	}
	if errors.Is(ErrOuterNotReady, ErrServiceNotStale) {
		t.Fatal("the two preconditions are indistinguishable from each other")
	}
}

type stubVerifier struct {
	generation uint64
	outerReady bool
	stale      bool
}

func (verifier *stubVerifier) Generation() uint64 { return verifier.generation }

func (verifier *stubVerifier) OuterReady(context.Context) (bool, error) {
	return verifier.outerReady, nil
}

func (verifier *stubVerifier) PritunlServiceStale(context.Context) (bool, error) {
	return verifier.stale, nil
}

var _ RootVerifier = (*stubVerifier)(nil)
var _ = ipc.ActionRescuePritunlService
