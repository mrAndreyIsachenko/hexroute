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
	domain     policy.Domain
	input      policystore.PrepareCandidateInput
	installed  policy.InstalledCompatibility
	publicKey  ed25519.PublicKey
	preparedAt time.Time
	receipt    policystore.PrepareReceipt
	err        error
	calls      int
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
		if response.Error != ipc.ErrorInvalidRequest {
			t.Fatalf("%s response = %+v", action, response)
		}
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
