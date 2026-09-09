package pritunlrescue

import (
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

// A session that is up and carrying nothing is grounds this runtime can check.
//
// The two halves used to describe no reachable fault: the asking domain asks
// when the session reports itself connected while its address is on no
// interface — a service that is running — and this runtime approved only a
// service that was not running.
//
// It can answer the same question for itself. It sees interfaces, and whether
// an address is on one is a fact rather than a conclusion, so confirming it
// keeps the second opinion a second opinion.
func TestABlackholeIsApprovedWhenThisRuntimeSeesItToo(t *testing.T) {
	verifier := &blackholeVerifier{
		generation: 4, outerReady: true,
		stale:  false, // the service is running; that is what makes it a blackhole
		absent: true,  // and this runtime cannot find the address either
	}
	decision, err := evaluateWithAddress(t, verifier, "192.168.244.136")
	if err != nil {
		t.Fatalf("Evaluate() = %v, want an approval", err)
	}
	if !decision.Approved {
		t.Fatal("a blackhole this runtime confirmed was not approved")
	}
}

// What the asking domain says is not enough on its own.
func TestABlackholeThisRuntimeCannotSeeIsRefused(t *testing.T) {
	verifier := &blackholeVerifier{
		generation: 4, outerReady: true,
		stale:  false,
		absent: false, // the address is on an interface: the session is carrying
	}
	_, err := evaluateWithAddress(t, verifier, "192.168.244.136")
	if !errors.Is(err, ErrServiceNotStale) {
		t.Fatalf("Evaluate() = %v, want %v", err, ErrServiceNotStale)
	}
}

// The older question still stands on its own.
func TestAStaleServiceIsStillApprovedWithoutEvidence(t *testing.T) {
	verifier := &blackholeVerifier{generation: 4, outerReady: true, stale: true}
	decision, err := evaluateWithAddress(t, verifier, "")
	if err != nil {
		t.Fatalf("Evaluate() = %v, want an approval", err)
	}
	if !decision.Approved {
		t.Fatal("a stale service was not approved")
	}
}

// The outer path still comes first: restarting into a path that is not there
// repairs nothing, whatever the session looks like.
func TestABlackholeIsNotApprovedWhileTheOuterPathIsDown(t *testing.T) {
	verifier := &blackholeVerifier{
		generation: 4, outerReady: false, stale: false, absent: true,
	}
	_, err := evaluateWithAddress(t, verifier, "192.168.244.136")
	if !errors.Is(err, ErrOuterNotReady) {
		t.Fatalf("Evaluate() = %v, want %v", err, ErrOuterNotReady)
	}
}

func evaluateWithAddress(
	t *testing.T,
	verifier RootVerifier,
	address string,
) (Decision, error) {
	t.Helper()
	handler, err := NewHandler(501, verifier)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	request, err := NewRequest("11111111-1111-4111-8111-111111111111", 4, address)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if address != "" && request.RescuePritunlService == nil {
		t.Fatal("the address was not carried in the request")
	}
	return handler.Evaluate(context.Background(), 501, request)
}

type blackholeVerifier struct {
	generation uint64
	outerReady bool
	stale      bool
	absent     bool
}

func (v *blackholeVerifier) Generation() uint64 { return v.generation }

func (v *blackholeVerifier) OuterReady(context.Context) (bool, error) {
	return v.outerReady, nil
}

func (v *blackholeVerifier) PritunlServiceStale(context.Context) (bool, error) {
	return v.stale, nil
}

func (v *blackholeVerifier) TunnelAddressAbsent(
	_ context.Context,
	address string,
) (bool, error) {
	if address == "" {
		return false, ipc.ErrInvalidRescueMessage
	}
	return v.absent, nil
}

// An address this runtime cannot parse is refused, not reported absent.
//
// Absent is what grounds a restart. Defaulting to it would let a malformed
// request obtain one, which is the opposite of what confirming is for.
func TestAnUnparseableAddressIsRefusedRatherThanCalledAbsent(t *testing.T) {
	verifier, err := NewLaunchdVerifier(
		refusingRunner{}, "com.example.client.service",
		func() uint64 { return 4 },
		func(context.Context) (bool, error) { return true, nil },
	)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	for _, address := range []string{"", "not-an-address", "::1", "192.168.244"} {
		absent, err := verifier.TunnelAddressAbsent(context.Background(), address)
		if !errors.Is(err, ipc.ErrInvalidRescueMessage) {
			t.Fatalf("%q returned %v, want %v", address, err, ipc.ErrInvalidRescueMessage)
		}
		if absent {
			t.Fatalf("%q was reported absent, which is what grounds a restart", address)
		}
	}
}

// Being unable to look is not the same as having looked and found nothing.
func TestAFailedLookIsNotAnAbsence(t *testing.T) {
	verifier, err := NewLaunchdVerifier(
		refusingRunner{err: errors.New("ifconfig did not run")},
		"com.example.client.service",
		func() uint64 { return 4 },
		func(context.Context) (bool, error) { return true, nil },
	)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	absent, err := verifier.TunnelAddressAbsent(
		context.Background(), "192.168.244.136")
	if err == nil {
		t.Fatal("a command that did not run reported an answer")
	}
	if absent {
		t.Fatal("a command that did not run reported the address absent")
	}
}

type refusingRunner struct{ err error }

func (runner refusingRunner) Output(
	context.Context,
	string,
	...string,
) ([]byte, error) {
	if runner.err != nil {
		return nil, runner.err
	}
	return []byte(""), nil
}
