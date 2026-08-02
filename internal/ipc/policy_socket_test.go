package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type policyDomainHandler struct {
	domain policy.Domain
	calls  atomic.Uint32
}

func (handler *policyDomainHandler) HandleIPC(
	_ context.Context,
	request Request,
) Response {
	handler.calls.Add(1)
	response := Response{Version: ProtocolVersion, RequestID: request.RequestID}
	if request.Action != ActionPreparePolicy || request.PreparePolicy == nil {
		response.Error = ErrorInvalidRequest
		return response
	}
	identity := request.PreparePolicy.Transaction
	generation := identity.RootPolicyGeneration
	payloadDigest := identity.RootPayloadSHA256
	if handler.domain == policy.DomainUser {
		generation = identity.UserPolicyGeneration
		payloadDigest = identity.UserPayloadSHA256
	}
	response.OK = true
	response.PreparePolicy = &PreparePolicyResult{
		TransactionID: identity.TransactionID,
		Domain:        handler.domain, BundleGeneration: identity.BundleGeneration,
		PolicyGeneration: generation, ManifestSHA256: identity.ManifestSHA256,
		PayloadSHA256: payloadDigest, ApprovalSHA256: identity.ApprovalSHA256,
	}
	return response
}

func TestPolicySocketsDeriveReceiptIdentityFromDaemonDomain(t *testing.T) {
	transaction := syntheticPolicyTransaction()
	tests := []struct {
		name             string
		domain           policy.Domain
		policyGeneration uint64
		payloadSHA256    string
	}{
		{
			name: "root socket", domain: policy.DomainRoot,
			policyGeneration: transaction.RootPolicyGeneration,
			payloadSHA256:    transaction.RootPayloadSHA256,
		},
		{
			name: "user socket", domain: policy.DomainUser,
			policyGeneration: transaction.UserPolicyGeneration,
			payloadSHA256:    transaction.UserPayloadSHA256,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &policyDomainHandler{domain: test.domain}
			path, stop := startPolicyTestServer(
				t,
				uint32(os.Getuid()),
				handler,
				&recordingReporter{},
			)
			defer stop()

			response, err := (Client{Path: path}).Do(context.Background(), Request{
				Version: ProtocolVersion, RequestID: "prepare-domain", Action: ActionPreparePolicy,
				PreparePolicy: &PreparePolicyRequest{Transaction: transaction},
			})
			if err != nil {
				t.Fatalf("Client.Do() error: %v", err)
			}
			result := response.PreparePolicy
			if !response.OK || result == nil || result.Domain != test.domain ||
				result.PolicyGeneration != test.policyGeneration ||
				result.PayloadSHA256 != test.payloadSHA256 || handler.calls.Load() != 1 {
				t.Fatalf("response = %+v, calls = %d", response, handler.calls.Load())
			}
		})
	}
}

func TestPolicySocketsRejectUnauthorizedPeerBeforeDispatch(t *testing.T) {
	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		t.Run(string(domain), func(t *testing.T) {
			handler := &policyDomainHandler{domain: domain}
			reporter := &recordingReporter{}
			path, stop := startPolicyTestServer(
				t,
				uint32(os.Getuid()+1),
				handler,
				reporter,
			)
			defer stop()

			_, err := (Client{Path: path, Timeout: time.Second}).Do(
				context.Background(),
				Request{
					Version: ProtocolVersion, RequestID: "policy-unauthorized",
					Action: ActionPolicyStatus, PolicyStatus: &PolicyStatusRequest{},
				},
			)
			if err == nil {
				t.Fatal("unauthorized policy client received a response")
			}
			waitForReport(t, reporter, ErrUnauthorizedPeer)
			if handler.calls.Load() != 0 {
				t.Fatalf("handler calls = %d, want 0", handler.calls.Load())
			}
		})
	}
}

func TestPolicyTransactionIdentityRejectsInvalidUUIDGenerationsAndDigests(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PolicyTransactionIdentity)
	}{
		{
			name: "transaction UUID",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.TransactionID = metadata.UUID("not-a-uuid")
			},
		},
		{
			name: "bundle generation",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.BundleGeneration = 0
			},
		},
		{
			name: "root generation",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.RootPolicyGeneration = 0
			},
		},
		{
			name: "user generation",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.UserPolicyGeneration = 0
			},
		},
		{
			name: "manifest digest",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.ManifestSHA256 = strings.Repeat("A", 64)
			},
		},
		{
			name: "root payload digest",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.RootPayloadSHA256 = strings.Repeat("b", 63)
			},
		},
		{
			name: "user payload digest",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.UserPayloadSHA256 = strings.Repeat("z", 64)
			},
		},
		{
			name: "approval digest",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.ApprovalSHA256 = ""
			},
		},
	}

	for _, operation := range []Action{ActionPreparePolicy, ActionCommitPolicy, ActionAbortPolicy} {
		for _, test := range tests {
			t.Run(string(operation)+"/"+test.name, func(t *testing.T) {
				identity := syntheticPolicyTransaction()
				test.mutate(&identity)
				request := policyRequestForAction(operation, identity)
				if err := request.Validate(); !errors.Is(err, ErrInvalidPolicyMessage) {
					t.Fatalf("Validate() error = %v, want %v", err, ErrInvalidPolicyMessage)
				}
			})
		}
	}
}

func TestPolicySocketsRejectInvalidTransactionIdentityBeforeDispatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PolicyTransactionIdentity)
	}{
		{
			name: "transaction UUID",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.TransactionID = metadata.UUID("not-a-uuid")
			},
		},
		{
			name: "generation",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.BundleGeneration = 0
			},
		},
		{
			name: "digest",
			mutate: func(identity *PolicyTransactionIdentity) {
				identity.ManifestSHA256 = strings.Repeat("A", 64)
			},
		},
	}

	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		for _, test := range tests {
			t.Run(string(domain)+"/"+test.name, func(t *testing.T) {
				handler := &policyDomainHandler{domain: domain}
				reporter := &recordingReporter{}
				path, stop := startPolicyTestServer(
					t,
					uint32(os.Getuid()),
					handler,
					reporter,
				)
				defer stop()

				identity := syntheticPolicyTransaction()
				test.mutate(&identity)
				request := policyRequestForAction(ActionPreparePolicy, identity)
				payload, err := json.Marshal(request)
				if err != nil {
					t.Fatalf("Marshal() error: %v", err)
				}
				sendRawPolicyFrame(t, path, payload)
				waitForReport(t, reporter, ErrInvalidPolicyMessage)
				if handler.calls.Load() != 0 {
					t.Fatalf("handler calls = %d, want 0", handler.calls.Load())
				}
			})
		}
	}
}

func TestPolicySocketsRejectPathsPayloadsAndUnknownOperations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   error
	}{
		{
			name: "arbitrary path",
			mutate: func(document map[string]any) {
				document["path"] = "/tmp/attacker-policy.json"
			},
			want: ErrMalformedFrame,
		},
		{
			name: "policy payload",
			mutate: func(document map[string]any) {
				document["payload"] = map[string]any{"effect": "allow"}
			},
			want: ErrMalformedFrame,
		},
		{
			name: "unknown operation",
			mutate: func(document map[string]any) {
				document["action"] = "replace_policy_from_path"
			},
			want: ErrUnknownAction,
		},
	}

	for _, domain := range []policy.Domain{policy.DomainRoot, policy.DomainUser} {
		for _, test := range tests {
			t.Run(string(domain)+"/"+test.name, func(t *testing.T) {
				handler := &policyDomainHandler{domain: domain}
				reporter := &recordingReporter{}
				path, stop := startPolicyTestServer(
					t,
					uint32(os.Getuid()),
					handler,
					reporter,
				)
				defer stop()

				request := policyRequestForAction(ActionPreparePolicy, syntheticPolicyTransaction())
				payload := mutatePolicyRequestJSON(t, request, test.mutate)
				sendRawPolicyFrame(t, path, payload)
				waitForReport(t, reporter, test.want)
				if handler.calls.Load() != 0 {
					t.Fatalf("handler calls = %d, want 0", handler.calls.Load())
				}
			})
		}
	}
}

func policyRequestForAction(
	action Action,
	identity PolicyTransactionIdentity,
) Request {
	request := Request{Version: ProtocolVersion, RequestID: "policy-validation", Action: action}
	switch action {
	case ActionPreparePolicy:
		request.PreparePolicy = &PreparePolicyRequest{Transaction: identity}
	case ActionCommitPolicy:
		request.CommitPolicy = &CommitPolicyRequest{Transaction: identity, Phase: CommitPolicyStage}
	case ActionAbortPolicy:
		request.AbortPolicy = &AbortPolicyRequest{Transaction: identity}
	}
	return request
}

func startPolicyTestServer(
	t *testing.T,
	allowedUID uint32,
	handler Handler,
	reporter RejectionReporter,
) (string, func()) {
	t.Helper()
	path := shortSocketPath(t)
	server, err := Listen(path, uint32(os.Getuid()), allowedUID, handler, reporter)
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()
	return path, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve() error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Serve() did not stop after cancellation")
		}
	}
}

func mutatePolicyRequestJSON(
	t *testing.T,
	request Request,
	mutate func(map[string]any),
) []byte {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	mutate(document)
	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal(mutated) error: %v", err)
	}
	return encoded
}

func sendRawPolicyFrame(t *testing.T, path string, payload []byte) {
	t.Helper()
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("DialUnix() error: %v", err)
	}
	if _, err := connection.Write(rawFrame(payload)); err != nil {
		_ = connection.Close()
		t.Fatalf("write policy frame: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close policy connection: %v", err)
	}
}
