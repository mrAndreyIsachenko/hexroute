package policycontrol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/policystore"
)

type CandidateStore interface {
	Domain() policy.Domain
	PrepareCandidate(
		policystore.PrepareCandidateInput,
		policy.InstalledCompatibility,
		ed25519.PublicKey,
		time.Time,
	) (policystore.PrepareReceipt, error)
}

type Handler struct {
	mu     sync.Mutex
	domain policy.Domain
	store  CandidateStore
	config RuntimeConfig
	now    func() time.Time
	status policy.Status
}

func NewHandler(
	store CandidateStore,
	config RuntimeConfig,
	now func() time.Time,
) (*Handler, error) {
	if store == nil || !store.Domain().Valid() || config.Validate() != nil ||
		config.Installed.Domain != store.Domain() || now == nil {
		return nil, ErrInvalidConfig
	}
	config.PinnedPublicKey = append(ed25519.PublicKey(nil), config.PinnedPublicKey...)
	config.Installed.TrustedCompilerSHA256 = append(
		[]string(nil),
		config.Installed.TrustedCompilerSHA256...,
	)
	return &Handler{
		domain: store.Domain(), store: store, config: config, now: now,
		status: noPolicyStatus(store.Domain()),
	}, nil
}

func NewUnavailableHandler(domain policy.Domain) (*Handler, error) {
	if !domain.Valid() {
		return nil, ErrInvalidConfig
	}
	return &Handler{domain: domain, status: noPolicyStatus(domain)}, nil
}

func (handler *Handler) HandleIPC(ctx context.Context, request ipc.Request) ipc.Response {
	response := ipc.Response{Version: ipc.ProtocolVersion, RequestID: request.RequestID}
	if handler == nil || ctx == nil {
		response.Error = ipc.ErrorInternal
		return response
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if ctx.Err() != nil {
		response.Error = ipc.ErrorInternal
		return response
	}

	switch request.Action {
	case ipc.ActionPolicyStatus:
		status := handler.status
		response.OK = true
		response.PolicyStatus = &ipc.PolicyStatusResult{Status: status}
	case ipc.ActionPreparePolicy:
		return handler.prepare(request, response)
	case ipc.ActionCommitPolicy, ipc.ActionAbortPolicy:
		response.Error = ipc.ErrorInvalidRequest
	default:
		response.Error = ipc.ErrorInvalidRequest
	}
	return response
}

func (handler *Handler) prepare(
	request ipc.Request,
	response ipc.Response,
) ipc.Response {
	if request.PreparePolicy == nil {
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
	identity := request.PreparePolicy.Transaction
	if handler.store == nil {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	receipt, err := handler.store.PrepareCandidate(
		policystore.PrepareCandidateInput{
			TransactionID:        identity.TransactionID,
			BundleGeneration:     identity.BundleGeneration,
			RootPolicyGeneration: identity.RootPolicyGeneration,
			UserPolicyGeneration: identity.UserPolicyGeneration,
			ManifestSHA256:       identity.ManifestSHA256,
			RootPayloadSHA256:    identity.RootPayloadSHA256,
			UserPayloadSHA256:    identity.UserPayloadSHA256,
			ApprovalSHA256:       identity.ApprovalSHA256,
		},
		handler.config.Installed,
		handler.config.PinnedPublicKey,
		handler.now(),
	)
	if err != nil {
		return handler.prepareFailure(identity, err, response)
	}
	handler.status = policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: receipt.Domain,
		State: policy.PolicyPrepared, BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256, Reason: policy.ReasonNone,
	}
	response.OK = true
	response.PreparePolicy = &ipc.PreparePolicyResult{
		TransactionID: receipt.TransactionID, Domain: receipt.Domain,
		BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256,
		PayloadSHA256:    receipt.PayloadSHA256,
		ApprovalSHA256:   receipt.ApprovalSHA256,
	}
	return response
}

func (handler *Handler) prepareFailure(
	identity ipc.PolicyTransactionIdentity,
	cause error,
	response ipc.Response,
) ipc.Response {
	state, reason, report := classifyPrepareFailure(cause)
	if report {
		policyGeneration := identity.RootPolicyGeneration
		if handler.domain == policy.DomainUser {
			policyGeneration = identity.UserPolicyGeneration
		}
		handler.status = policy.Status{
			Schema: policy.PolicyStatusSchema, Domain: handler.domain,
			State: state, BundleGeneration: identity.BundleGeneration,
			PolicyGeneration: policyGeneration,
			ManifestSHA256:   identity.ManifestSHA256, Reason: reason,
		}
		response.Error = ipc.ErrorPrecondition
		return response
	}
	response.Error = ipc.ErrorInternal
	return response
}

func classifyPrepareFailure(cause error) (policy.PolicyState, policy.PolicyReason, bool) {
	switch {
	case errors.Is(cause, policy.ErrRestartRequired):
		return policy.PolicyRestartRequired, policy.ReasonStaticMismatch, true
	case errors.Is(cause, policy.ErrUnsupportedPolicy),
		errors.Is(cause, policy.ErrPolicyDowngrade),
		errors.Is(cause, policy.ErrUntrustedCompiler):
		return policy.PolicyRejected, policy.ReasonUnsupportedSchema, true
	case errors.Is(cause, policy.ErrPolicyDomainMismatch):
		return policy.PolicyRejected, policy.ReasonDomainMismatch, true
	case errors.Is(cause, policyapproval.ErrApprovalExpired):
		return policy.PolicyRejected, policy.ReasonClockAnomaly, true
	case errors.Is(cause, policyapproval.ErrApprovalSignature),
		errors.Is(cause, policyapproval.ErrSignerMismatch),
		errors.Is(cause, policyapproval.ErrInvalidApproval):
		return policy.PolicyRejected, policy.ReasonInvalidSignature, true
	case errors.Is(cause, policystore.ErrCandidateIdentity),
		errors.Is(cause, policystore.ErrGenerationNotFound),
		errors.Is(cause, policy.ErrInvalidCandidateBundle):
		return policy.PolicyRejected, policy.ReasonDigestMismatch, true
	default:
		return "", "", false
	}
}

func noPolicyStatus(domain policy.Domain) policy.Status {
	return policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: domain,
		State: policy.PolicyNone, Reason: policy.ReasonNoValidGeneration,
	}
}
