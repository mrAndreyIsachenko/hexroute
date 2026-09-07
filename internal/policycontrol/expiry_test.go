package policycontrol

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
)

func expiryRuntime(t *testing.T) (RuntimeConfig, ed25519.PublicKey) {
	t.Helper()
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, publicKey
}

func lapsedLineage() policystore.Lineage {
	return policystore.Lineage{
		Domain:        policy.DomainUser,
		Generation:    policystore.Generation{Bundle: 2, Policy: 1},
		PayloadSHA256: strings.Repeat("c", 64),
		PolicySchema:  1,
		StaticSHA256:  strings.Repeat("a", 64),
		ConfirmedAt:   "2026-08-08T21:51:48.843547Z",
		Expired:       true,
	}
}

// TestExpiredGenerationIsNotAFault is the regression for the state this machine
// spent a month in: the generation ended, and the daemon called it a clock
// anomaly on a machine whose clock was correct.
func TestExpiredGenerationIsNotAFault(t *testing.T) {
	runtime, _ := expiryRuntime(t)
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policyapproval.ErrApprovalExpired,
		lineage:    lapsedLineage(),
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatalf("NewHandler() over a lapsed store: %v", err)
	}
	if handler.status.State != policy.PolicyNone || handler.status.Reason != policy.ReasonExpired {
		t.Fatalf("status = %+v, want state none with reason expired", handler.status)
	}
	if handler.authorizationSuspension.Suspended {
		t.Fatalf("expiry raised a suspension: %+v", handler.authorizationSuspension)
	}
	if handler.MutationAllowed() {
		t.Fatal("mutations allowed with no active generation")
	}
	decision := handler.AuthorizePritunlRecovery(
		policy.DomainUser, "pritunl", 1, strings.Repeat("d", 64),
	)
	if decision.Allowed {
		t.Fatal("a lapsed generation authorized an action")
	}
}

// TestExpiredGenerationStillNamesTheParent is why the lapsed chain is adopted
// rather than discarded: without it the operator would have to hand-write the
// current generation into a root-owned configuration file.
func TestExpiredGenerationStillNamesTheParent(t *testing.T) {
	runtime, _ := expiryRuntime(t)
	lineage := lapsedLineage()
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policyapproval.ErrApprovalExpired,
		lineage:    lineage,
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	installed := handler.config.Installed
	if installed.CurrentBundleGeneration != lineage.Generation.Bundle ||
		installed.CurrentPolicyGeneration != lineage.Generation.Policy ||
		installed.CurrentPayloadSHA256 != lineage.PayloadSHA256 ||
		installed.CurrentPolicySchema != lineage.PolicySchema {
		t.Fatalf("installed compatibility = %+v, want it derived from the store", installed)
	}
}

// TestLapsedIsDistinctFromNeverHavingHadAGeneration keeps the two apart because
// one of them means there is a chain to build on.
func TestLapsedIsDistinctFromNeverHavingHadAGeneration(t *testing.T) {
	runtime, _ := expiryRuntime(t)
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policystore.ErrRecordNotFound,
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if handler.status.Reason != policy.ReasonNoValidGeneration {
		t.Fatalf("reason = %q, want no_valid_generation for an empty store", handler.status.Reason)
	}
}

// TestDamagedEvidenceStillSuspendsWhileExpired asserts the relaxation is about
// expiry alone: a store that also fails to prove its chain is a fault again.
func TestDamagedEvidenceStillSuspendsWhileExpired(t *testing.T) {
	runtime, _ := expiryRuntime(t)
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policyapproval.ErrApprovalExpired,
		lineageErr: policystore.ErrActivePointerConsistency,
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !handler.authorizationSuspension.Suspended {
		t.Fatal("an unprovable chain was accepted as a mere lapse")
	}
}

// TestLapsedHandlerKeepsRevalidating is the trap that removing the suspension
// created: refreshAuthorizationLocked used to return early for anything neither
// active nor suspended, so a lapsed daemon would have stopped looking and never
// noticed its successor.
func TestLapsedHandlerKeepsRevalidating(t *testing.T) {
	runtime, _ := expiryRuntime(t)
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policyapproval.ErrApprovalExpired,
		lineage:    lapsedLineage(),
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	before := store.recoverCalls
	handler.MutationAllowed()
	if store.recoverCalls <= before {
		t.Fatal("a lapsed daemon stopped revalidating; its successor would never be seen")
	}
}
