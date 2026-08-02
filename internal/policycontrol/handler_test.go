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
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
)

type recordingCandidateStore struct {
	domain       policy.Domain
	input        policystore.PrepareCandidateInput
	installed    policy.InstalledCompatibility
	publicKey    ed25519.PublicKey
	preparedAt   time.Time
	receipt      policystore.PrepareReceipt
	err          error
	calls        int
	commitResult policystore.CommitCandidateResult
	commitErr    error
	commitCalls  int
	abortErr     error
	abortCalls   int
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

func (store *recordingCandidateStore) CommitCandidate(
	input policystore.PrepareCandidateInput,
	committedAt time.Time,
) (policystore.CommitCandidateResult, error) {
	store.commitCalls++
	store.input = input
	store.preparedAt = committedAt
	return store.commitResult, store.commitErr
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
			request.CommitPolicy = &ipc.CommitPolicyRequest{Transaction: identity}
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
	commit := handler.HandleIPC(context.Background(), ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: "commit-lifecycle",
		Action:       ipc.ActionCommitPolicy,
		CommitPolicy: &ipc.CommitPolicyRequest{Transaction: identity},
	})
	if !commit.OK || commit.CommitPolicy == nil || store.calls != 2 || store.commitCalls != 1 ||
		commit.CommitPolicy.Status.State != policy.PolicyActive ||
		commit.CommitPolicy.Status.ActivatedAt != activeAt {
		t.Fatalf("commit response=%+v store=%+v", commit, store)
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
