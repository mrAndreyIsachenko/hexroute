package ipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const policyTransactionID metadata.UUID = "123e4567-e89b-42d3-a456-426614174000"

func TestPolicyRequestFrameRoundTrip(t *testing.T) {
	transaction := syntheticPolicyTransaction()
	requests := []Request{
		{
			Version: ProtocolVersion, RequestID: "policy-status-01", Action: ActionPolicyStatus,
			PolicyStatus: &PolicyStatusRequest{},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-prepare-01", Action: ActionPreparePolicy,
			PreparePolicy: &PreparePolicyRequest{Transaction: transaction},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-commit-01", Action: ActionCommitPolicy,
			CommitPolicy: &CommitPolicyRequest{Transaction: transaction, Phase: CommitPolicyStage},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-abort-01", Action: ActionAbortPolicy,
			AbortPolicy: &AbortPolicyRequest{Transaction: transaction},
		},
	}

	for _, request := range requests {
		t.Run(string(request.Action), func(t *testing.T) {
			var frame bytes.Buffer
			if err := WriteFrame(&frame, request); err != nil {
				t.Fatalf("WriteFrame() error: %v", err)
			}
			decoded, err := ReadRequest(&frame)
			if err != nil {
				t.Fatalf("ReadRequest() error: %v", err)
			}
			if !reflect.DeepEqual(decoded, request) {
				t.Fatalf("ReadRequest() = %+v, want %+v", decoded, request)
			}
		})
	}
}

func TestPolicyResponseFrameRoundTrip(t *testing.T) {
	status := syntheticPolicyStatus()
	responses := []Response{
		{
			Version: ProtocolVersion, RequestID: "policy-status-01", OK: true,
			PolicyStatus: &PolicyStatusResult{Status: status},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-prepare-01", OK: true,
			PreparePolicy: &PreparePolicyResult{
				TransactionID: policyTransactionID, Domain: policy.DomainRoot,
				BundleGeneration: 9, PolicyGeneration: 5,
				ManifestSHA256: strings.Repeat("a", 64),
				PayloadSHA256:  strings.Repeat("b", 64),
				ApprovalSHA256: strings.Repeat("d", 64),
			},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-commit-01", OK: true,
			CommitPolicy: &CommitPolicyResult{TransactionID: policyTransactionID, Phase: CommitPolicyConfirm, Status: status},
		},
		{
			Version: ProtocolVersion, RequestID: "policy-abort-01", OK: true,
			AbortPolicy: &AbortPolicyResult{TransactionID: policyTransactionID, Status: status},
		},
	}

	for _, response := range responses {
		t.Run(response.RequestID, func(t *testing.T) {
			var frame bytes.Buffer
			if err := WriteFrame(&frame, response); err != nil {
				t.Fatalf("WriteFrame() error: %v", err)
			}
			decoded, err := ReadResponse(&frame)
			if err != nil {
				t.Fatalf("ReadResponse() error: %v", err)
			}
			if !reflect.DeepEqual(decoded, response) {
				t.Fatalf("ReadResponse() = %+v, want %+v", decoded, response)
			}
		})
	}
}

func TestPolicyRequestUnionRejectsMismatchedOrUnboundedMessages(t *testing.T) {
	transaction := syntheticPolicyTransaction()
	tests := []Request{
		{
			Version: ProtocolVersion, RequestID: "missing-body", Action: ActionPreparePolicy,
		},
		{
			Version: ProtocolVersion, RequestID: "wrong-body", Action: ActionPreparePolicy,
			CommitPolicy: &CommitPolicyRequest{Transaction: transaction, Phase: CommitPolicyStage},
		},
		{
			Version: ProtocolVersion, RequestID: "multiple-bodies", Action: ActionCommitPolicy,
			PreparePolicy: &PreparePolicyRequest{Transaction: transaction},
			CommitPolicy:  &CommitPolicyRequest{Transaction: transaction, Phase: CommitPolicyStage},
		},
		{
			Version: ProtocolVersion, RequestID: "legacy-field", Action: ActionAbortPolicy,
			ExpectedGeneration: 4,
			AbortPolicy:        &AbortPolicyRequest{Transaction: transaction},
		},
		{
			Version: ProtocolVersion, RequestID: "missing-commit-phase", Action: ActionCommitPolicy,
			CommitPolicy: &CommitPolicyRequest{Transaction: transaction},
		},
	}
	for _, request := range tests {
		if err := request.Validate(); !errors.Is(err, ErrInvalidPolicyMessage) {
			t.Fatalf("%s error = %v, want %v", request.RequestID, err, ErrInvalidPolicyMessage)
		}
	}

	invalid := transaction
	invalid.ManifestSHA256 = "not-a-digest"
	request := Request{
		Version: ProtocolVersion, RequestID: "invalid-identity", Action: ActionPreparePolicy,
		PreparePolicy: &PreparePolicyRequest{Transaction: invalid},
	}
	if err := request.Validate(); !errors.Is(err, ErrInvalidPolicyMessage) {
		t.Fatalf("invalid identity error = %v, want %v", err, ErrInvalidPolicyMessage)
	}
}

func TestPolicyMessagesExposeOnlyBoundedIdentityFields(t *testing.T) {
	requests := []any{
		PreparePolicyRequest{Transaction: syntheticPolicyTransaction()},
		CommitPolicyRequest{Transaction: syntheticPolicyTransaction(), Phase: CommitPolicyStage},
		AbortPolicyRequest{Transaction: syntheticPolicyTransaction()},
	}
	for _, request := range requests {
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Marshal() error: %v", err)
		}
		for _, forbidden := range []string{`"path":`, `"source":`, `"payload":`, `"command":`} {
			if bytes.Contains(encoded, []byte(forbidden)) {
				t.Fatalf("policy message %s contains forbidden field %s", encoded, forbidden)
			}
		}
		if len(encoded) > MaxFrameBytes {
			t.Fatalf("policy message size = %d, max %d", len(encoded), MaxFrameBytes)
		}
	}
}

func syntheticPolicyTransaction() PolicyTransactionIdentity {
	return PolicyTransactionIdentity{
		TransactionID:        policyTransactionID,
		BundleGeneration:     9,
		RootPolicyGeneration: 5,
		UserPolicyGeneration: 7,
		ManifestSHA256:       strings.Repeat("a", 64),
		RootPayloadSHA256:    strings.Repeat("b", 64),
		UserPayloadSHA256:    strings.Repeat("c", 64),
		ApprovalSHA256:       strings.Repeat("d", 64),
	}
}

func syntheticPolicyStatus() policy.Status {
	return policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: policy.DomainRoot,
		State: policy.PolicyActive, BundleGeneration: 9, PolicyGeneration: 5,
		ManifestSHA256: strings.Repeat("a", 64),
		ActivatedAt:    "2026-08-02T12:00:00Z", Reason: policy.ReasonNone,
	}
}
