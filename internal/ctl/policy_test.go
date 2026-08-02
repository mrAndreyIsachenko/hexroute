package ctl

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestPolicyStatusQueriesBothDomains(t *testing.T) {
	var calls []string
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		calls = append(calls, path+":"+string(request.Action))
		domain := policy.DomainRoot
		if path == "/safe/user.sock" {
			domain = policy.DomainUser
		}
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
			PolicyStatus: &ipc.PolicyStatusResult{
				Status: policy.Status{
					Schema: policy.PolicyStatusSchema, Domain: domain,
					State: policy.PolicyNone, Reason: policy.ReasonNoValidGeneration,
				},
				AuthorizationSuspension: policy.AuthorizationSuspension{
					Schema: policy.AuthorizationSuspensionSchema,
					Reason: policy.ReasonNone,
				},
			},
		}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"policy", "status"}, &stdout, &stderr, config); code != 0 {
		t.Fatalf("Run() code=%d stderr=%q", code, stderr.String())
	}
	if strings.Join(calls, ",") != "/safe/root.sock:policy_status,/safe/user.sock:policy_status" {
		t.Fatalf("calls = %#v", calls)
	}
	if strings.Count(stdout.String(), `"policy_status"`) != 2 {
		t.Fatalf("output = %s", stdout.String())
	}
}

func TestPolicyCommitAndRollbackRequireBothMatchingReceiptsBeforeCommit(t *testing.T) {
	for _, command := range []string{"commit", "rollback"} {
		t.Run(command, func(t *testing.T) {
			identity := syntheticPolicyIdentity()
			var calls []string
			config := testConfig(func(
				_ context.Context,
				path string,
				request ipc.Request,
			) (ipc.Response, error) {
				calls = append(calls, path+":"+string(request.Action))
				domain := policy.DomainRoot
				if path == "/safe/user.sock" {
					domain = policy.DomainUser
				}
				switch request.Action {
				case ipc.ActionPreparePolicy:
					return policyPrepareResponse(request, identity, domain), nil
				case ipc.ActionCommitPolicy:
					status := activePolicyStatus(identity, domain)
					if request.CommitPolicy.Phase == ipc.CommitPolicyStage {
						status.State = policy.PolicyPrepared
						status.ActivatedAt = ""
					} else if request.CommitPolicy.Phase == ipc.CommitPolicyActivate {
						status.State = policy.PolicyDomainMismatch
						status.Reason = policy.ReasonDomainMismatch
					}
					return ipc.Response{
						Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
						CommitPolicy: &ipc.CommitPolicyResult{
							TransactionID: identity.TransactionID,
							Phase:         request.CommitPolicy.Phase, Status: status,
						},
					}, nil
				default:
					t.Fatalf("unexpected request: %+v", request)
					return ipc.Response{}, nil
				}
			})
			var stdout, stderr bytes.Buffer
			if code := Run(policyArgs(command, identity), &stdout, &stderr, config); code != 0 {
				t.Fatalf("Run() code=%d stderr=%q", code, stderr.String())
			}
			want := strings.Join([]string{
				"/safe/root.sock:prepare_policy",
				"/safe/user.sock:prepare_policy",
				"/safe/root.sock:commit_policy",
				"/safe/user.sock:commit_policy",
				"/safe/root.sock:commit_policy",
				"/safe/user.sock:commit_policy",
				"/safe/root.sock:commit_policy",
				"/safe/user.sock:commit_policy",
			}, ",")
			if strings.Join(calls, ",") != want {
				t.Fatalf("calls = %#v", calls)
			}
		})
	}
}

func TestPolicyReceiptMismatchAbortsWithoutCommit(t *testing.T) {
	identity := syntheticPolicyIdentity()
	var calls []ipc.Action
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		calls = append(calls, request.Action)
		domain := policy.DomainRoot
		if path == "/safe/user.sock" {
			domain = policy.DomainUser
		}
		if request.Action == ipc.ActionPreparePolicy {
			response := policyPrepareResponse(request, identity, domain)
			if domain == policy.DomainUser {
				response.PreparePolicy.PayloadSHA256 = strings.Repeat("e", 64)
			}
			return response, nil
		}
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID,
			Error: ipc.ErrorPrecondition,
		}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := Run(policyArgs("commit", identity), &stdout, &stderr, config); code != 1 {
		t.Fatalf("Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	want := []ipc.Action{
		ipc.ActionPreparePolicy, ipc.ActionPreparePolicy,
		ipc.ActionAbortPolicy, ipc.ActionAbortPolicy,
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %#v", calls)
		}
	}
}

func TestPolicyActivationFailureLeavesForwardRecoveryWithoutAbort(t *testing.T) {
	identity := syntheticPolicyIdentity()
	failUserActivation := true
	var calls []string
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		domain := policy.DomainRoot
		if path == "/safe/user.sock" {
			domain = policy.DomainUser
		}
		phase := ""
		if request.CommitPolicy != nil {
			phase = ":" + string(request.CommitPolicy.Phase)
		}
		calls = append(calls, path+":"+string(request.Action)+phase)
		if request.Action == ipc.ActionPreparePolicy {
			return policyPrepareResponse(request, identity, domain), nil
		}
		if request.CommitPolicy.Phase == ipc.CommitPolicyActivate &&
			domain == policy.DomainUser && failUserActivation {
			return ipc.Response{
				Version: ipc.ProtocolVersion, RequestID: request.RequestID,
				Error: ipc.ErrorInternal,
			}, nil
		}
		status := activePolicyStatus(identity, domain)
		if request.CommitPolicy.Phase == ipc.CommitPolicyStage {
			status.State = policy.PolicyPrepared
			status.ActivatedAt = ""
		} else if request.CommitPolicy.Phase == ipc.CommitPolicyActivate {
			status.State = policy.PolicyDomainMismatch
			status.Reason = policy.ReasonDomainMismatch
		}
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
			CommitPolicy: &ipc.CommitPolicyResult{
				TransactionID: identity.TransactionID,
				Phase:         request.CommitPolicy.Phase, Status: status,
			},
		}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := Run(policyArgs("commit", identity), &stdout, &stderr, config); code != 1 {
		t.Fatalf("first Run() code=%d", code)
	}
	for _, call := range calls {
		if strings.Contains(call, "abort_policy") || strings.Contains(call, ":confirm") {
			t.Fatalf("post-activation failure triggered rollback/finalize: %#v", calls)
		}
	}

	failUserActivation = false
	calls = nil
	stdout.Reset()
	stderr.Reset()
	if code := Run(policyArgs("commit", identity), &stdout, &stderr, config); code != 0 {
		t.Fatalf("retry Run() code=%d stderr=%q", code, stderr.String())
	}
	if len(calls) != 8 || !strings.HasSuffix(calls[len(calls)-1], ":confirm") {
		t.Fatalf("recovery calls = %#v", calls)
	}
}

func TestPolicyAbortTargetsBothDomainsAndInvalidArgumentsAreRedacted(t *testing.T) {
	identity := syntheticPolicyIdentity()
	var calls int
	config := testConfig(func(
		_ context.Context,
		path string,
		request ipc.Request,
	) (ipc.Response, error) {
		calls++
		domain := policy.DomainRoot
		if path == "/safe/user.sock" {
			domain = policy.DomainUser
		}
		return ipc.Response{
			Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
			AbortPolicy: &ipc.AbortPolicyResult{
				TransactionID: identity.TransactionID,
				Status: policy.Status{
					Schema: policy.PolicyStatusSchema, Domain: domain,
					State: policy.PolicyNone, Reason: policy.ReasonNoValidGeneration,
				},
			},
		}, nil
	})
	var stdout, stderr bytes.Buffer
	if code := Run(policyArgs("abort", identity), &stdout, &stderr, config); code != 0 || calls != 2 {
		t.Fatalf("Run() code=%d calls=%d stderr=%q", code, calls, stderr.String())
	}

	canary := "HEXROUTE_CANARY_PRIVATE_KEY_PATH"
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"policy", "commit", "--transaction-id", canary}, &stdout, &stderr, config); code != 2 {
		t.Fatalf("invalid Run() code=%d", code)
	}
	if strings.Contains(stdout.String(), canary) || strings.Contains(stderr.String(), canary) {
		t.Fatalf("invalid arguments leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func syntheticPolicyIdentity() ipc.PolicyTransactionIdentity {
	return ipc.PolicyTransactionIdentity{
		TransactionID:    "123e4567-e89b-42d3-a456-426614174000",
		BundleGeneration: 7, RootPolicyGeneration: 5, UserPolicyGeneration: 6,
		ManifestSHA256:    strings.Repeat("a", 64),
		RootPayloadSHA256: strings.Repeat("b", 64),
		UserPayloadSHA256: strings.Repeat("c", 64),
		ApprovalSHA256:    strings.Repeat("d", 64),
	}
}

func policyArgs(command string, identity ipc.PolicyTransactionIdentity) []string {
	return []string{
		"policy", command,
		"--transaction-id", string(identity.TransactionID),
		"--bundle-generation", "7",
		"--root-generation", "5",
		"--user-generation", "6",
		"--manifest-sha256", identity.ManifestSHA256,
		"--root-payload-sha256", identity.RootPayloadSHA256,
		"--user-payload-sha256", identity.UserPayloadSHA256,
		"--approval-sha256", identity.ApprovalSHA256,
	}
}

func policyPrepareResponse(
	request ipc.Request,
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
) ipc.Response {
	policyGeneration := identity.RootPolicyGeneration
	payloadSHA256 := identity.RootPayloadSHA256
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
		payloadSHA256 = identity.UserPayloadSHA256
	}
	return ipc.Response{
		Version: ipc.ProtocolVersion, RequestID: request.RequestID, OK: true,
		PreparePolicy: &ipc.PreparePolicyResult{
			TransactionID: identity.TransactionID, Domain: domain,
			BundleGeneration: identity.BundleGeneration,
			PolicyGeneration: policyGeneration,
			ManifestSHA256:   identity.ManifestSHA256,
			PayloadSHA256:    payloadSHA256,
			ApprovalSHA256:   identity.ApprovalSHA256,
		},
	}
}

func activePolicyStatus(
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
) policy.Status {
	policyGeneration := identity.RootPolicyGeneration
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
	}
	return policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: domain, State: policy.PolicyActive,
		BundleGeneration: identity.BundleGeneration,
		PolicyGeneration: policyGeneration,
		ManifestSHA256:   identity.ManifestSHA256,
		ActivatedAt:      "2030-01-01T00:00:00Z", Reason: policy.ReasonNone,
	}
}
