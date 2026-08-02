package policycontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
)

type recordingCandidateStore struct {
	domain        policy.Domain
	input         policystore.PrepareCandidateInput
	installed     policy.InstalledCompatibility
	publicKey     ed25519.PublicKey
	preparedAt    time.Time
	receipt       policystore.PrepareReceipt
	err           error
	calls         int
	commitResult  policystore.CommitCandidateResult
	commitErr     error
	stageCalls    int
	activateCalls int
	confirmCalls  int
	abortErr      error
	abortCalls    int
	recoverResult policystore.RevalidatedActive
	recoverErr    error
	recoverCalls  int
	pendingIntent policystore.CommitIntent
	pendingErr    error
}

func (store *recordingCandidateStore) Domain() policy.Domain { return store.domain }

func (store *recordingCandidateStore) PrepareCandidate(
	input policystore.PrepareCandidateInput,
	installed policy.InstalledCompatibility,
	publicKey ed25519.PublicKey,
	preparedAt time.Time,
) (policystore.PrepareReceipt, error) {
	store.calls++
	store.input = input
	store.installed = installed
	store.publicKey = append(ed25519.PublicKey(nil), publicKey...)
	store.preparedAt = preparedAt
	return store.receipt, store.err
}

func (store *recordingCandidateStore) StageCommitCandidate(
	input policystore.PrepareCandidateInput,
	committedAt time.Time,
) (policystore.StagedCandidate, error) {
	store.stageCalls++
	store.input = input
	store.preparedAt = committedAt
	return policystore.StagedCandidate{PolicySchema: store.commitResult.PolicySchema}, store.commitErr
}

func (store *recordingCandidateStore) ActivateCandidate(
	input policystore.PrepareCandidateInput,
	committedAt time.Time,
) (policystore.CommitCandidateResult, error) {
	store.activateCalls++
	store.input = input
	store.preparedAt = committedAt
	return store.commitResult, store.commitErr
}

func (store *recordingCandidateStore) ConfirmCandidate(
	input policystore.PrepareCandidateInput,
	committedAt time.Time,
) (policystore.CommitCandidateResult, error) {
	store.confirmCalls++
	store.input = input
	store.preparedAt = committedAt
	result := store.commitResult
	if result.Pointer.ConfirmedAt == "" {
		result.Pointer.ConfirmedAt = committedAt.UTC().Format(time.RFC3339Nano)
		store.commitResult.Pointer.ConfirmedAt = result.Pointer.ConfirmedAt
	}
	return result, store.commitErr
}

func (store *recordingCandidateStore) AbortCandidate(
	input policystore.PrepareCandidateInput,
	abortedAt time.Time,
) error {
	store.abortCalls++
	store.input = input
	store.preparedAt = abortedAt
	return store.abortErr
}

func (store *recordingCandidateStore) RecoverActive(
	policy.InstalledCompatibility,
	ed25519.PublicKey,
	time.Time,
) (policystore.RevalidatedActive, error) {
	store.recoverCalls++
	if store.recoverErr != nil {
		return policystore.RevalidatedActive{}, store.recoverErr
	}
	if store.recoverResult.Generation.Bundle == 0 {
		if store.activateCalls == 0 {
			return policystore.RevalidatedActive{}, policystore.ErrRecordNotFound
		}
		pointer := store.commitResult.Pointer
		return policystore.RevalidatedActive{
			Domain: pointer.Domain,
			Generation: policystore.Generation{
				Bundle: pointer.BundleGeneration, Policy: pointer.PolicyGeneration,
			},
			ManifestSHA256: pointer.ManifestSHA256,
			PayloadSHA256:  pointer.PayloadSHA256,
			ActivatedAt:    pointer.ActivatedAt,
			ConfirmedAt:    pointer.ConfirmedAt,
			Manifest:       policy.Manifest{PolicySchema: store.commitResult.PolicySchema},
			Intent: policystore.CommitIntent{
				TransactionID:        store.input.TransactionID,
				BundleGeneration:     store.input.BundleGeneration,
				RootPolicyGeneration: store.input.RootPolicyGeneration,
				UserPolicyGeneration: store.input.UserPolicyGeneration,
				ManifestSHA256:       store.input.ManifestSHA256,
				RootPayloadSHA256:    store.input.RootPayloadSHA256,
				UserPayloadSHA256:    store.input.UserPayloadSHA256,
				ApprovalSHA256:       store.input.ApprovalSHA256,
			},
		}, nil
	}
	return store.recoverResult, nil
}

func (store *recordingCandidateStore) RecoverPendingCommit() (policystore.CommitIntent, error) {
	if store.pendingErr != nil {
		return policystore.CommitIntent{}, store.pendingErr
	}
	if store.pendingIntent.TransactionID == "" {
		return policystore.CommitIntent{}, policystore.ErrRecordNotFound
	}
	return store.pendingIntent, nil
}

func TestHandlerPublishesStatusAndMapsPrepareToDomainStore(t *testing.T) {
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
			static := syntheticStaticConfig(domain, publicKey)
			runtime, err := static.Runtime(domain)
			if err != nil {
				t.Fatal(err)
			}
			identity := syntheticIPCIdentity()
			policyGeneration := identity.RootPolicyGeneration
			payloadDigest := identity.RootPayloadSHA256
			if domain == policy.DomainUser {
				policyGeneration = identity.UserPolicyGeneration
				payloadDigest = identity.UserPayloadSHA256
			}
			store := &recordingCandidateStore{
				domain: domain,
				receipt: policystore.PrepareReceipt{
					Schema:        policystore.PrepareReceiptSchema,
					TransactionID: identity.TransactionID, Domain: domain,
					BundleGeneration: identity.BundleGeneration,
					PolicyGeneration: policyGeneration,
					ManifestSHA256:   identity.ManifestSHA256,
					PayloadSHA256:    payloadDigest, ApprovalSHA256: identity.ApprovalSHA256,
					PreparedAt: "2030-01-01T00:30:00Z",
				},
			}
			preparedAt := time.Date(2030, time.January, 1, 0, 30, 0, 0, time.UTC)
			handler, err := NewHandler(store, runtime, func() time.Time { return preparedAt })
			if err != nil {
				t.Fatal(err)
			}

			initial := handler.HandleIPC(context.Background(), ipc.Request{
				Version: ipc.ProtocolVersion, RequestID: "status-before",
				Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
			})
			if !initial.OK || initial.PolicyStatus == nil ||
				initial.PolicyStatus.Status.State != policy.PolicyNone {
				t.Fatalf("initial status = %+v", initial)
			}
			response := handler.HandleIPC(context.Background(), ipc.Request{
				Version: ipc.ProtocolVersion, RequestID: "prepare",
				Action:        ipc.ActionPreparePolicy,
				PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
			})
			if !response.OK || response.PreparePolicy == nil || store.calls != 1 ||
				store.input.BundleGeneration != identity.BundleGeneration ||
				!reflect.DeepEqual(store.installed, runtime.Installed) ||
				!bytes.Equal(store.publicKey, publicKey) || !store.preparedAt.Equal(preparedAt) {
				t.Fatalf("response=%+v store=%+v", response, store)
			}
			status := handler.HandleIPC(context.Background(), ipc.Request{
				Version: ipc.ProtocolVersion, RequestID: "status-after",
				Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
			})
			if !status.OK || status.PolicyStatus.Status.State != policy.PolicyPrepared ||
				status.PolicyStatus.Status.Domain != domain ||
				status.PolicyStatus.Status.PolicyGeneration != policyGeneration {
				t.Fatalf("prepared status = %+v", status)
			}
		})
	}
}

func TestHandlerRejectsUnavailableAndUnauthorizedLifecycleOperations(t *testing.T) {
	handler, err := NewUnavailableHandler(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	prepare := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "prepare-unavailable",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
	})
	if prepare.Error != ipc.ErrorPrecondition {
		t.Fatalf("unavailable prepare = %+v", prepare)
	}
	for _, action := range []ipc.Action{ipc.ActionCommitPolicy, ipc.ActionAbortPolicy} {
		request := ipc.Request{Version: ipc.ProtocolVersion, RequestID: "not-yet-authorized", Action: action}
		if action == ipc.ActionCommitPolicy {
			request.CommitPolicy = &ipc.CommitPolicyRequest{
				Transaction: identity, Phase: ipc.CommitPolicyStage,
			}
		} else {
			request.AbortPolicy = &ipc.AbortPolicyRequest{Transaction: identity}
		}
		response := handler.HandleIPC(context.Background(), request)
		if response.Error != ipc.ErrorPrecondition {
			t.Fatalf("%s response = %+v", action, response)
		}
	}
}

func TestHandlerRevalidatesBeforeCommitAndRestoresStatusOnAbort(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainRoot, publicKey).Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	receipt := policystore.PrepareReceipt{
		Schema:        policystore.PrepareReceiptSchema,
		TransactionID: identity.TransactionID, Domain: policy.DomainRoot,
		BundleGeneration: identity.BundleGeneration,
		PolicyGeneration: identity.RootPolicyGeneration,
		ManifestSHA256:   identity.ManifestSHA256,
		PayloadSHA256:    identity.RootPayloadSHA256,
		ApprovalSHA256:   identity.ApprovalSHA256,
		PreparedAt:       "2030-01-01T00:30:00Z",
	}
	activeAt := "2030-01-01T00:31:00Z"
	store := &recordingCandidateStore{
		domain: policy.DomainRoot, receipt: receipt,
		commitResult: policystore.CommitCandidateResult{
			PolicySchema: runtime.Installed.CurrentPolicySchema,
			Pointer: policystore.ActivePointer{
				TransactionID: identity.TransactionID, Domain: policy.DomainRoot,
				BundleGeneration: identity.BundleGeneration,
				PolicyGeneration: identity.RootPolicyGeneration,
				ManifestSHA256:   identity.ManifestSHA256,
				PayloadSHA256:    identity.RootPayloadSHA256,
				ActivatedAt:      activeAt,
			},
		},
	}
	now := time.Date(2030, time.January, 1, 0, 31, 0, 0, time.UTC)
	handler, err := NewHandler(store, runtime, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	prepareRequest := ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "prepare-lifecycle",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
	}
	if response := handler.HandleIPC(context.Background(), prepareRequest); !response.OK {
		t.Fatalf("prepare response = %+v", response)
	}
	stage := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "stage-lifecycle",
		Action: ipc.ActionCommitPolicy,
		CommitPolicy: &ipc.CommitPolicyRequest{
			Transaction: identity, Phase: ipc.CommitPolicyStage,
		},
	})
	if !stage.OK || stage.CommitPolicy == nil || store.stageCalls != 1 || handler.MutationAllowed() {
		t.Fatalf("stage response=%+v store=%+v", stage, store)
	}
	activate := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "activate-lifecycle",
		Action: ipc.ActionCommitPolicy,
		CommitPolicy: &ipc.CommitPolicyRequest{
			Transaction: identity, Phase: ipc.CommitPolicyActivate,
		},
	})
	if !activate.OK || activate.CommitPolicy == nil || store.calls != 3 ||
		store.activateCalls != 1 ||
		activate.CommitPolicy.Status.State != policy.PolicyDomainMismatch ||
		activate.CommitPolicy.Status.ActivatedAt != activeAt || handler.MutationAllowed() {
		t.Fatalf("activate response=%+v store=%+v", activate, store)
	}
	confirm := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "confirm-lifecycle",
		Action: ipc.ActionCommitPolicy,
		CommitPolicy: &ipc.CommitPolicyRequest{
			Transaction: identity, Phase: ipc.CommitPolicyConfirm,
		},
	})
	if !confirm.OK || confirm.CommitPolicy == nil || store.confirmCalls != 1 ||
		confirm.CommitPolicy.Status.State != policy.PolicyActive || !handler.MutationAllowed() {
		t.Fatalf("confirm response=%+v store=%+v", confirm, store)
	}

	abortStore := &recordingCandidateStore{domain: policy.DomainRoot, receipt: receipt}
	abortHandler, err := NewHandler(abortStore, runtime, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if response := abortHandler.HandleIPC(context.Background(), prepareRequest); !response.OK {
		t.Fatalf("abort prepare response = %+v", response)
	}
	abort := abortHandler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "abort-lifecycle",
		Action:      ipc.ActionAbortPolicy,
		AbortPolicy: &ipc.AbortPolicyRequest{Transaction: identity},
	})
	if !abort.OK || abort.AbortPolicy == nil || abortStore.abortCalls != 1 ||
		abort.AbortPolicy.Status.State != policy.PolicyNone {
		t.Fatalf("abort response=%+v store=%+v", abort, abortStore)
	}
}

func TestHandlerReportsBoundedPrepareFailureWithoutStoreDetails(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingCandidateStore{
		domain: policy.DomainUser,
		err:    errors.New("HEXROUTE_CANARY_PRIVATE_STORE_PATH"),
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "internal-failure",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: syntheticIPCIdentity()},
	})
	if response.Error != ipc.ErrorInternal || strings.Contains(string(response.Error), "CANARY") {
		t.Fatalf("response = %+v", response)
	}
}

func TestHandlerRejectsASecondPreparedTransaction(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{6}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainRoot, publicKey).Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	store := &recordingCandidateStore{
		domain: policy.DomainRoot,
		receipt: policystore.PrepareReceipt{
			Schema:        policystore.PrepareReceiptSchema,
			TransactionID: identity.TransactionID, Domain: policy.DomainRoot,
			BundleGeneration: identity.BundleGeneration,
			PolicyGeneration: identity.RootPolicyGeneration,
			ManifestSHA256:   identity.ManifestSHA256,
			PayloadSHA256:    identity.RootPayloadSHA256,
			ApprovalSHA256:   identity.ApprovalSHA256,
			PreparedAt:       "2030-01-01T00:30:00Z",
		},
	}
	handler, err := NewHandler(store, runtime, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	first := ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "prepare-first",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
	}
	if response := handler.HandleIPC(context.Background(), first); !response.OK {
		t.Fatalf("first prepare = %+v", response)
	}
	secondIdentity := identity
	secondIdentity.TransactionID = "223e4567-e89b-42d3-a456-426614174000"
	second := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "prepare-second",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: secondIdentity},
	})
	if second.Error != ipc.ErrorPrecondition || store.calls != 1 {
		t.Fatalf("second prepare=%+v calls=%d", second, store.calls)
	}
}

func TestHandlerRestoresConfirmedAndUnconfirmedActiveStatusAtStartup(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	for _, test := range []struct {
		name        string
		confirmedAt string
		state       policy.PolicyState
		allowed     bool
	}{
		{name: "unconfirmed", state: policy.PolicyDomainMismatch},
		{name: "confirmed", confirmedAt: "2030-01-01T00:31:00Z", state: policy.PolicyActive, allowed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingCandidateStore{
				domain: policy.DomainUser,
				recoverResult: policystore.RevalidatedActive{
					Domain: policy.DomainUser,
					Generation: policystore.Generation{
						Bundle: identity.BundleGeneration, Policy: identity.UserPolicyGeneration,
					},
					ManifestSHA256: identity.ManifestSHA256,
					PayloadSHA256:  identity.UserPayloadSHA256,
					ActivatedAt:    "2030-01-01T00:30:00Z", ConfirmedAt: test.confirmedAt,
					Manifest: policy.Manifest{PolicySchema: runtime.Installed.CurrentPolicySchema},
					Intent: policystore.CommitIntent{
						TransactionID:        identity.TransactionID,
						BundleGeneration:     identity.BundleGeneration,
						RootPolicyGeneration: identity.RootPolicyGeneration,
						UserPolicyGeneration: identity.UserPolicyGeneration,
						ManifestSHA256:       identity.ManifestSHA256,
						RootPayloadSHA256:    identity.RootPayloadSHA256,
						UserPayloadSHA256:    identity.UserPayloadSHA256,
						ApprovalSHA256:       identity.ApprovalSHA256,
					},
				},
			}
			handler, err := NewHandler(store, runtime, func() time.Time {
				return time.Date(2030, time.January, 1, 0, 32, 0, 0, time.UTC)
			})
			if err != nil {
				t.Fatal(err)
			}
			response := handler.HandleIPC(context.Background(), ipc.Request{
				Version: ipc.ProtocolVersion, RequestID: "recovered-status",
				Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
			})
			if !response.OK || response.PolicyStatus.Status.State != test.state ||
				handler.MutationAllowed() != test.allowed {
				t.Fatalf("status=%+v allowed=%t", response, handler.MutationAllowed())
			}
			if response.PolicyStatus.AuthorizationSuspension.Suspended != !test.allowed ||
				(!test.allowed && response.PolicyStatus.AuthorizationSuspension.Reason != policy.ReasonDomainMismatch) {
				t.Fatalf("authorization suspension = %+v", response.PolicyStatus.AuthorizationSuspension)
			}
			if test.confirmedAt != "" {
				prepare := handler.HandleIPC(context.Background(), ipc.Request{
					Version: ipc.ProtocolVersion, RequestID: "retry-prepare",
					Action:        ipc.ActionPreparePolicy,
					PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
				})
				stage := handler.HandleIPC(context.Background(), ipc.Request{
					Version: ipc.ProtocolVersion, RequestID: "retry-stage",
					Action: ipc.ActionCommitPolicy,
					CommitPolicy: &ipc.CommitPolicyRequest{
						Transaction: identity, Phase: ipc.CommitPolicyStage,
					},
				})
				if !prepare.OK || !stage.OK || stage.CommitPolicy.Status.State != policy.PolicyActive ||
					!handler.MutationAllowed() {
					t.Fatalf("confirmed retry prepare=%+v stage=%+v", prepare, stage)
				}
			}
		})
	}
}

func TestHandlerRestoresPendingCommitAsMismatchAtStartup(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainRoot, publicKey).Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	receipt := policystore.PrepareReceipt{
		Schema:        policystore.PrepareReceiptSchema,
		TransactionID: identity.TransactionID, Domain: policy.DomainRoot,
		BundleGeneration: identity.BundleGeneration,
		PolicyGeneration: identity.RootPolicyGeneration,
		ManifestSHA256:   identity.ManifestSHA256,
		PayloadSHA256:    identity.RootPayloadSHA256,
		ApprovalSHA256:   identity.ApprovalSHA256,
		PreparedAt:       "2030-01-01T00:30:00Z",
	}
	store := &recordingCandidateStore{
		domain: policy.DomainRoot, receipt: receipt,
		pendingIntent: policystore.CommitIntent{
			TransactionID:        identity.TransactionID,
			BundleGeneration:     identity.BundleGeneration,
			RootPolicyGeneration: identity.RootPolicyGeneration,
			UserPolicyGeneration: identity.UserPolicyGeneration,
			ManifestSHA256:       identity.ManifestSHA256,
			RootPayloadSHA256:    identity.RootPayloadSHA256,
			UserPayloadSHA256:    identity.UserPayloadSHA256,
			ApprovalSHA256:       identity.ApprovalSHA256,
		},
	}
	handler, err := NewHandler(store, runtime, func() time.Time {
		return time.Date(2030, time.January, 1, 0, 31, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "pending-status",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !response.OK || response.PolicyStatus.Status.State != policy.PolicyDomainMismatch ||
		!response.PolicyStatus.AuthorizationSuspension.Suspended ||
		response.PolicyStatus.AuthorizationSuspension.Reason != policy.ReasonDomainMismatch ||
		handler.MutationAllowed() {
		t.Fatalf("pending status=%+v allowed=%t", response, handler.MutationAllowed())
	}
}

func TestHandlerSuspendsStartupAuthorizationWithBoundedReasons(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainRoot, publicKey).Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		cause  error
		reason policy.PolicyReason
	}{
		{name: "corruption", cause: policystore.ErrInvalidArtifact, reason: policy.ReasonCorruption},
		{name: "digest", cause: policystore.ErrActivePointerConsistency, reason: policy.ReasonDigestMismatch},
		{name: "signature", cause: policyapproval.ErrApprovalSignature, reason: policy.ReasonInvalidSignature},
		{name: "clock", cause: policystore.ErrActiveClockAnomaly, reason: policy.ReasonClockAnomaly},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingCandidateStore{domain: policy.DomainRoot, recoverErr: test.cause}
			now := time.Date(2030, time.January, 1, 0, 40, 0, 0, time.UTC)
			handler, err := NewHandler(store, runtime, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			response := handler.HandleIPC(context.Background(), ipc.Request{
				Version: ipc.ProtocolVersion, RequestID: "suspended-status",
				Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
			})
			suspension := response.PolicyStatus.AuthorizationSuspension
			if !response.OK || response.PolicyStatus.Status.State != policy.PolicyNone ||
				!suspension.Suspended || suspension.Reason != test.reason ||
				suspension.Since != "2030-01-01T00:40:00Z" || handler.MutationAllowed() {
				t.Fatalf("response=%+v allowed=%t", response, handler.MutationAllowed())
			}
		})
	}
}

func TestHandlerClearsSuspensionOnlyAfterActiveRevalidation(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	store := &recordingCandidateStore{
		domain:     policy.DomainUser,
		recoverErr: policystore.ErrActivePointerConsistency,
	}
	now := time.Date(2030, time.January, 1, 0, 45, 0, 0, time.UTC)
	handler, err := NewHandler(store, runtime, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	handler.SuspendAuthorization(policy.ReasonIPCOwnership)
	if handler.MutationAllowed() {
		t.Fatal("mutation allowed before revalidation")
	}
	store.recoverErr = nil
	store.recoverResult = revalidatedActiveFixture(identity, policy.DomainUser, runtime, true)
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "revalidated-status",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !response.OK || response.PolicyStatus.Status.State != policy.PolicyActive ||
		response.PolicyStatus.AuthorizationSuspension.Suspended || !handler.MutationAllowed() {
		t.Fatalf("response=%+v allowed=%t", response, handler.MutationAllowed())
	}
}

func TestInvalidCandidatePreservesLastValidActivePolicy(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainRoot, publicKey).Runtime(policy.DomainRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	for _, cause := range []error{
		policyapproval.ErrApprovalSignature,
		policyapproval.ErrApprovalExpired,
		policy.ErrUnsupportedPolicy,
		policy.ErrInvalidCandidateBundle,
	} {
		store := &recordingCandidateStore{
			domain: policy.DomainRoot,
			recoverResult: revalidatedActiveFixture(
				identity, policy.DomainRoot, runtime, true,
			),
		}
		handler, err := NewHandler(store, runtime, func() time.Time {
			return time.Date(2030, time.January, 1, 0, 55, 0, 0, time.UTC)
		})
		if err != nil {
			t.Fatal(err)
		}
		candidate := identity
		candidate.TransactionID = "223e4567-e89b-42d3-a456-426614174000"
		candidate.BundleGeneration++
		candidate.RootPolicyGeneration++
		candidate.UserPolicyGeneration++
		store.err = cause
		response := handler.HandleIPC(context.Background(), ipc.Request{
			Version: ipc.ProtocolVersion, RequestID: "invalid-candidate",
			Action:        ipc.ActionPreparePolicy,
			PreparePolicy: &ipc.PreparePolicyRequest{Transaction: candidate},
		})
		status := handler.HandleIPC(context.Background(), ipc.Request{
			Version: ipc.ProtocolVersion, RequestID: "active-after-rejection",
			Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
		})
		if response.Error != ipc.ErrorPrecondition || !status.OK ||
			status.PolicyStatus.Status.State != policy.PolicyActive ||
			status.PolicyStatus.Status.BundleGeneration != identity.BundleGeneration ||
			status.PolicyStatus.Status.ManifestSHA256 != identity.ManifestSHA256 ||
			status.PolicyStatus.AuthorizationSuspension.Suspended ||
			!handler.MutationAllowed() || store.stageCalls != 0 ||
			store.activateCalls != 0 || store.confirmCalls != 0 || store.abortCalls != 0 {
			t.Fatalf("cause=%v response=%+v status=%+v store=%+v", cause, response, status, store)
		}
	}
}

func TestInvalidCandidateWithoutActivePolicyRemainsObserveOnlySafeMode(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{11}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingCandidateStore{domain: policy.DomainUser}
	handler, err := NewHandler(store, runtime, func() time.Time {
		return time.Date(2030, time.January, 1, 1, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	store.err = policyapproval.ErrApprovalSignature
	response := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "invalid-without-active",
		Action:        ipc.ActionPreparePolicy,
		PreparePolicy: &ipc.PreparePolicyRequest{Transaction: syntheticIPCIdentity()},
	})
	status := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "safe-mode-status",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if response.Error != ipc.ErrorPrecondition || !status.OK ||
		status.PolicyStatus.Status.State != policy.PolicyNone ||
		status.PolicyStatus.Status.Reason != policy.ReasonNoValidGeneration ||
		handler.MutationAllowed() || store.stageCalls != 0 || store.activateCalls != 0 ||
		store.confirmCalls != 0 || store.abortCalls != 0 {
		t.Fatalf("response=%+v status=%+v store=%+v", response, status, store)
	}
}

func TestSuspensionPreservesActiveGenerationAndDoesNotEnterActivationPaths(t *testing.T) {
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{12}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	runtime, err := syntheticStaticConfig(policy.DomainUser, publicKey).Runtime(policy.DomainUser)
	if err != nil {
		t.Fatal(err)
	}
	identity := syntheticIPCIdentity()
	store := &recordingCandidateStore{
		domain: policy.DomainUser,
		recoverResult: revalidatedActiveFixture(
			identity, policy.DomainUser, runtime, true,
		),
	}
	handler, err := NewHandler(store, runtime, func() time.Time {
		return time.Date(2030, time.January, 1, 1, 5, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	store.recoverErr = policystore.ErrInvalidArtifact
	if handler.MutationAllowed() {
		t.Fatal("mutation allowed after active evidence corruption")
	}
	status := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "preserved-active",
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	})
	if !status.OK || status.PolicyStatus.Status.State != policy.PolicyActive ||
		status.PolicyStatus.Status.BundleGeneration != identity.BundleGeneration ||
		!status.PolicyStatus.AuthorizationSuspension.Suspended ||
		status.PolicyStatus.AuthorizationSuspension.Reason != policy.ReasonCorruption ||
		store.stageCalls != 0 || store.activateCalls != 0 ||
		store.confirmCalls != 0 || store.abortCalls != 0 {
		t.Fatalf("status=%+v store=%+v", status, store)
	}
}

func revalidatedActiveFixture(
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
	runtime RuntimeConfig,
	confirmed bool,
) policystore.RevalidatedActive {
	policyGeneration := identity.RootPolicyGeneration
	payloadSHA256 := identity.RootPayloadSHA256
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
		payloadSHA256 = identity.UserPayloadSHA256
	}
	confirmedAt := ""
	if confirmed {
		confirmedAt = "2030-01-01T00:31:00Z"
	}
	return policystore.RevalidatedActive{
		Domain: domain,
		Generation: policystore.Generation{
			Bundle: identity.BundleGeneration, Policy: policyGeneration,
		},
		ManifestSHA256: identity.ManifestSHA256,
		PayloadSHA256:  payloadSHA256,
		ActivatedAt:    "2030-01-01T00:30:00Z", ConfirmedAt: confirmedAt,
		Manifest: policy.Manifest{PolicySchema: runtime.Installed.CurrentPolicySchema},
		Intent: policystore.CommitIntent{
			TransactionID:        identity.TransactionID,
			BundleGeneration:     identity.BundleGeneration,
			RootPolicyGeneration: identity.RootPolicyGeneration,
			UserPolicyGeneration: identity.UserPolicyGeneration,
			ManifestSHA256:       identity.ManifestSHA256,
			RootPayloadSHA256:    identity.RootPayloadSHA256,
			UserPayloadSHA256:    identity.UserPayloadSHA256,
			ApprovalSHA256:       identity.ApprovalSHA256,
		},
	}
}

func syntheticIPCIdentity() ipc.PolicyTransactionIdentity {
	return ipc.PolicyTransactionIdentity{
		TransactionID:    "123e4567-e89b-42d3-a456-426614174000",
		BundleGeneration: 1, RootPolicyGeneration: 1, UserPolicyGeneration: 1,
		ManifestSHA256:    strings.Repeat("a", 64),
		RootPayloadSHA256: strings.Repeat("b", 64),
		UserPayloadSHA256: strings.Repeat("c", 64),
		ApprovalSHA256:    strings.Repeat("d", 64),
	}
}
