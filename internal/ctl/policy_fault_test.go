package ctl

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type policyFault struct {
	role               ipc.DaemonRole
	action             ipc.Action
	phase              ipc.CommitPolicyPhase
	applyBeforeFailure bool
	fired              bool
}

type faultDomainState struct {
	prepared  bool
	staged    bool
	active    bool
	confirmed bool
	aborts    int
}

type policyFaultHarness struct {
	identity ipc.PolicyTransactionIdentity
	fault    policyFault
	root     faultDomainState
	user     faultDomainState
}

func TestPolicyCoordinatorConvergesAcrossDeterministicCrashBoundaries(t *testing.T) {
	tests := []struct {
		name                string
		fault               policyFault
		activationStarted   bool
		failedRoleActivated bool
	}{
		{
			name:  "root process terminates before prepare",
			fault: policyFault{role: ipc.RoleRoot, action: ipc.ActionPreparePolicy},
		},
		{
			name:  "user process terminates between prepares",
			fault: policyFault{role: ipc.RoleUser, action: ipc.ActionPreparePolicy},
		},
		{
			name:  "root process terminates while staging intent",
			fault: policyFault{role: ipc.RoleRoot, action: ipc.ActionCommitPolicy, phase: ipc.CommitPolicyStage},
		},
		{
			name:  "user process terminates while staging intent",
			fault: policyFault{role: ipc.RoleUser, action: ipc.ActionCommitPolicy, phase: ipc.CommitPolicyStage},
		},
		{
			name:              "root process terminates between domain commits",
			fault:             policyFault{role: ipc.RoleRoot, action: ipc.ActionCommitPolicy, phase: ipc.CommitPolicyActivate},
			activationStarted: true,
		},
		{
			name:              "user process terminates between domain commits",
			fault:             policyFault{role: ipc.RoleUser, action: ipc.ActionCommitPolicy, phase: ipc.CommitPolicyActivate},
			activationStarted: true,
		},
		{
			name: "root response is lost after active pointer replacement",
			fault: policyFault{
				role: ipc.RoleRoot, action: ipc.ActionCommitPolicy,
				phase: ipc.CommitPolicyActivate, applyBeforeFailure: true,
			},
			activationStarted: true, failedRoleActivated: true,
		},
		{
			name: "user response is lost after active pointer replacement",
			fault: policyFault{
				role: ipc.RoleUser, action: ipc.ActionCommitPolicy,
				phase: ipc.CommitPolicyActivate, applyBeforeFailure: true,
			},
			activationStarted: true, failedRoleActivated: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := &policyFaultHarness{
				identity: syntheticPolicyIdentity(), fault: test.fault,
			}
			config := testConfig(harness.roundTrip)
			var stdout, stderr bytes.Buffer
			if code := Run(policyArgs("commit", harness.identity), &stdout, &stderr, config); code != 1 {
				t.Fatalf("faulted Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !harness.fault.fired {
				t.Fatal("configured fault did not fire")
			}
			if !test.activationStarted {
				if harness.root.active || harness.user.active {
					t.Fatalf("pre-activation fault advanced pointer: root=%+v user=%+v", harness.root, harness.user)
				}
			} else if !harness.root.active && !harness.user.active {
				t.Fatalf("between-commit fault exposed no forward recovery state: root=%+v user=%+v", harness.root, harness.user)
			}
			failedState := harness.state(test.fault.role)
			if test.failedRoleActivated != failedState.active {
				t.Fatalf("failed role active=%t, want %t", failedState.active, test.failedRoleActivated)
			}
			if (harness.root.active && harness.root.aborts != 0) ||
				(harness.user.active && harness.user.aborts != 0) {
				t.Fatalf("active pointer received rollback attempt: root=%+v user=%+v", harness.root, harness.user)
			}

			stdout.Reset()
			stderr.Reset()
			if code := Run(policyArgs("commit", harness.identity), &stdout, &stderr, config); code != 0 {
				t.Fatalf("recovery Run() code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !harness.root.confirmed || !harness.user.confirmed ||
				!harness.root.active || !harness.user.active {
				t.Fatalf("recovery did not converge: root=%+v user=%+v", harness.root, harness.user)
			}
		})
	}
}

func (harness *policyFaultHarness) roundTrip(
	_ context.Context,
	path string,
	request ipc.Request,
) (ipc.Response, error) {
	role := ipc.RoleRoot
	if path == "/safe/user.sock" {
		role = ipc.RoleUser
	}
	if harness.matchesFault(role, request) && !harness.fault.applyBeforeFailure {
		harness.fault.fired = true
		return ipc.Response{}, errors.New("synthetic process termination")
	}
	response := harness.apply(role, request)
	if harness.matchesFault(role, request) {
		harness.fault.fired = true
		return ipc.Response{}, errors.New("synthetic response loss")
	}
	return response, nil
}

func (harness *policyFaultHarness) matchesFault(
	role ipc.DaemonRole,
	request ipc.Request,
) bool {
	if harness.fault.fired || harness.fault.role != role || harness.fault.action != request.Action {
		return false
	}
	if request.Action != ipc.ActionCommitPolicy {
		return true
	}
	return request.CommitPolicy != nil && request.CommitPolicy.Phase == harness.fault.phase
}

func (harness *policyFaultHarness) apply(
	role ipc.DaemonRole,
	request ipc.Request,
) ipc.Response {
	state := harness.state(role)
	domain := roleDomain(role)
	response := ipc.Response{Version: ipc.ProtocolVersion, RequestID: request.RequestID}
	switch request.Action {
	case ipc.ActionPreparePolicy:
		state.prepared = true
		response = policyPrepareResponse(request, harness.identity, domain)
	case ipc.ActionAbortPolicy:
		if !state.active {
			state.prepared = false
			state.staged = false
			state.aborts++
		}
		response.OK = true
		response.AbortPolicy = &ipc.AbortPolicyResult{
			TransactionID: harness.identity.TransactionID,
			Status: policy.Status{
				Schema: policy.PolicyStatusSchema, Domain: domain,
				State: policy.PolicyNone, Reason: policy.ReasonNoValidGeneration,
			},
		}
	case ipc.ActionCommitPolicy:
		if request.CommitPolicy == nil || (!state.prepared && !state.active) {
			response.Error = ipc.ErrorPrecondition
			return response
		}
		status := activePolicyStatus(harness.identity, domain)
		switch request.CommitPolicy.Phase {
		case ipc.CommitPolicyStage:
			state.staged = true
			status.State = policy.PolicyDomainMismatch
			status.ActivatedAt = ""
			status.Reason = policy.ReasonDomainMismatch
		case ipc.CommitPolicyActivate:
			if !state.staged {
				response.Error = ipc.ErrorPrecondition
				return response
			}
			state.active = true
			status.State = policy.PolicyDomainMismatch
			status.Reason = policy.ReasonDomainMismatch
		case ipc.CommitPolicyConfirm:
			if !state.active {
				response.Error = ipc.ErrorPrecondition
				return response
			}
			state.confirmed = true
		default:
			response.Error = ipc.ErrorInvalidRequest
			return response
		}
		response.OK = true
		response.CommitPolicy = &ipc.CommitPolicyResult{
			TransactionID: harness.identity.TransactionID,
			Phase:         request.CommitPolicy.Phase, Status: status,
		}
	default:
		response.Error = ipc.ErrorInvalidRequest
	}
	return response
}

func (harness *policyFaultHarness) state(role ipc.DaemonRole) *faultDomainState {
	if role == ipc.RoleUser {
		return &harness.user
	}
	return &harness.root
}
