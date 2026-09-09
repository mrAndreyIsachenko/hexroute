package pritunlrescue

import (
	"context"
	"errors"
	"testing"
)

// The condition that asks and the condition that approves are exclusive.
//
// The user runtime asks for a restart when the session reports itself connected
// with an address while that address is not on any interface — a session that
// is up and carrying nothing. That is the trigger the specification describes
// and the only one the original design had.
//
// This runtime approves only a service that is loaded and not running. A
// blackholed session has a service that is running: that is what makes it a
// blackhole rather than an outage.
//
// So the request arrives exactly when this check refuses it. Measured on the
// host on 2026-09-09 the other reachable states are no better: a service booted
// out makes the probe error, and one killed under KeepAlive reports
// `spawn scheduled` for about eight seconds and then `running`, never
// `not running`.
func TestABlackholedSessionCannotSatisfyTheStalenessCheck(t *testing.T) {
	// What the machine reports while a session is blackholed: the service is
	// there and running, which is why the session is up at all.
	running := "system/com.example.client.service = {\n\tstate = running\n\tpid = 4211\n}\n"
	if loadedAndNotRunning(running) {
		t.Fatal("a running service read as stale; the test's premise is wrong")
	}

	handler, err := NewHandler(501, &agreementVerifier{
		generation: 4, outerReady: true,
		// The verifier reads the same output the machine gives.
		stale: loadedAndNotRunning(running),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	request, err := NewRequest("11111111-1111-4111-8111-111111111111", 4)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = handler.Evaluate(context.Background(), 501, request)
	if !errors.Is(err, ErrServiceNotStale) {
		t.Fatalf("Evaluate() = %v, want %v", err, ErrServiceNotStale)
	}
	// Stated plainly, because the shape of this is the finding: the two halves
	// of one capability are asking about different faults, and no induction
	// reconciles them. Either the trigger changes or the check does.
}

type agreementVerifier struct {
	generation uint64
	outerReady bool
	stale      bool
}

func (v *agreementVerifier) Generation() uint64 { return v.generation }

func (v *agreementVerifier) OuterReady(context.Context) (bool, error) {
	return v.outerReady, nil
}

func (v *agreementVerifier) PritunlServiceStale(context.Context) (bool, error) {
	return v.stale, nil
}
