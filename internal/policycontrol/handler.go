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
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
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
	StageCommitCandidate(
		policystore.PrepareCandidateInput,
		time.Time,
	) (policystore.StagedCandidate, error)
	ActivateCandidate(
		policystore.PrepareCandidateInput,
		time.Time,
	) (policystore.CommitCandidateResult, error)
	ConfirmCandidate(
		policystore.PrepareCandidateInput,
		time.Time,
	) (policystore.CommitCandidateResult, error)
	AbortCandidate(policystore.PrepareCandidateInput, time.Time) error
	RecoverActive(
		policy.InstalledCompatibility,
		ed25519.PublicKey,
		time.Time,
	) (policystore.RevalidatedActive, error)
	RecoverPendingCommit() (policystore.CommitIntent, error)
}

type Handler struct {
	mu                      sync.Mutex
	domain                  policy.Domain
	store                   CandidateStore
	config                  RuntimeConfig
	now                     func() time.Time
	monotonicNow            func() time.Duration
	clockGuard              *policyclock.Guard
	status                  policy.Status
	authorizationSuspension policy.AuthorizationSuspension
	existingState           *policy.ExistingStateStatus

	preparedIdentity   ipc.PolicyTransactionIdentity
	previousStatus     policy.Status
	previousSuspension policy.AuthorizationSuspension
	hasPrepared        bool
	activeIdentity     ipc.PolicyTransactionIdentity
	activeReceipt      policystore.PrepareReceipt
	activePayload      policy.DomainPayload
	hasActive          bool
}

func NewHandler(
	store CandidateStore,
	config RuntimeConfig,
	now func() time.Time,
) (*Handler, error) {
	started := time.Now()
	return newHandlerWithClock(store, config, now, func() time.Duration {
		return time.Since(started)
	})
}

func newHandlerWithClock(
	store CandidateStore,
	config RuntimeConfig,
	now func() time.Time,
	monotonicNow func() time.Duration,
) (*Handler, error) {
	if store == nil || !store.Domain().Valid() || config.Validate() != nil ||
		config.Installed.Domain != store.Domain() || now == nil || monotonicNow == nil {
		return nil, ErrInvalidConfig
	}
	clockGuard, err := policyclock.NewGuard(policyclock.DefaultMaxWallSkew)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	config.PinnedPublicKey = append(ed25519.PublicKey(nil), config.PinnedPublicKey...)
	config.Installed.TrustedCompilerSHA256 = append(
		[]string(nil),
		config.Installed.TrustedCompilerSHA256...,
	)
	handler := &Handler{
		domain: store.Domain(), store: store, config: config, now: now,
		monotonicNow: monotonicNow, clockGuard: clockGuard,
		status:                  noPolicyStatus(store.Domain()),
		authorizationSuspension: clearAuthorizationSuspension(),
	}
	currentTime, err := handler.checkedNowLocked()
	if err != nil {
		return nil, ErrInvalidConfig
	}
	active, err := store.RecoverActive(config.Installed, config.PinnedPublicKey, currentTime)
	if errors.Is(err, policystore.ErrRecordNotFound) {
		pending, pendingErr := store.RecoverPendingCommit()
		if errors.Is(pendingErr, policystore.ErrRecordNotFound) {
			return handler, nil
		}
		if pendingErr != nil {
			return nil, pendingErr
		}
		if err := handler.restorePending(pending, currentTime); err != nil {
			return nil, err
		}
		return handler, nil
	}
	if err != nil {
		if reason, ok := classifyActiveFailure(err); ok {
			handler.suspendAuthorizationLocked(reason, currentTime)
			return handler, nil
		}
		return nil, err
	}
	handler.restoreActive(active, currentTime)
	return handler, nil
}

func NewUnavailableHandler(domain policy.Domain) (*Handler, error) {
	if !domain.Valid() {
		return nil, ErrInvalidConfig
	}
	clockGuard, err := policyclock.NewGuard(policyclock.DefaultMaxWallSkew)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	started := time.Now()
	return &Handler{
		domain: domain, status: noPolicyStatus(domain), now: time.Now,
		monotonicNow:            func() time.Duration { return time.Since(started) },
		clockGuard:              clockGuard,
		authorizationSuspension: clearAuthorizationSuspension(),
	}, nil
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
		handler.refreshAuthorizationLocked()
		handler.refreshExistingStateLocked()
		status := handler.status
		response.OK = true
		response.PolicyStatus = &ipc.PolicyStatusResult{
			Status:                  status,
			AuthorizationSuspension: handler.authorizationSuspension,
			ExistingState:           cloneExistingState(handler.existingState),
		}
	case ipc.ActionPreparePolicy:
		return handler.prepare(request, response)
	case ipc.ActionCommitPolicy:
		return handler.commit(request, response)
	case ipc.ActionAbortPolicy:
		return handler.abort(request, response)
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
	now, err := handler.checkedNowLocked()
	if err != nil {
		return handler.clockFailure(response)
	}
	if handler.hasPrepared && handler.preparedIdentity != identity {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	if handler.hasActive && handler.activeIdentity == identity {
		response.OK = true
		result := prepareResult(handler.activeReceipt)
		response.PreparePolicy = &result
		return response
	}
	receipt, err := handler.store.PrepareCandidate(
		candidateInput(identity),
		handler.config.Installed,
		handler.config.PinnedPublicKey,
		now,
	)
	if err != nil {
		return handler.prepareFailure(identity, err, response)
	}
	if !handler.hasPrepared {
		handler.previousStatus = handler.status
		handler.previousSuspension = handler.authorizationSuspension
	}
	handler.preparedIdentity = identity
	handler.hasPrepared = true
	handler.status = policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: receipt.Domain,
		State: policy.PolicyPrepared, BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256, Reason: policy.ReasonNone,
	}
	response.OK = true
	result := prepareResult(receipt)
	response.PreparePolicy = &result
	return response
}

func (handler *Handler) commit(
	request ipc.Request,
	response ipc.Response,
) ipc.Response {
	if request.CommitPolicy == nil {
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
	identity := request.CommitPolicy.Transaction
	if handler.store == nil ||
		(!handler.hasPrepared || handler.preparedIdentity != identity) &&
			(!handler.hasActive || handler.activeIdentity != identity) {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	switch request.CommitPolicy.Phase {
	case ipc.CommitPolicyStage:
		return handler.stage(identity, response)
	case ipc.CommitPolicyActivate:
		return handler.activate(identity, response)
	case ipc.CommitPolicyConfirm:
		return handler.confirm(identity, response)
	default:
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
}

func (handler *Handler) stage(
	identity ipc.PolicyTransactionIdentity,
	response ipc.Response,
) ipc.Response {
	now, err := handler.checkedNowLocked()
	if err != nil {
		return handler.clockFailure(response)
	}
	if err := handler.revalidate(identity, now); err != nil {
		return handler.lifecycleFailure(err, response)
	}
	_, err = handler.store.StageCommitCandidate(candidateInput(identity), now)
	if err != nil {
		return handler.lifecycleFailure(err, response)
	}
	if !handler.hasActive || handler.activeIdentity != identity {
		handler.status = stagedMismatchStatus(identity, handler.domain)
		handler.suspendAuthorizationLocked(policy.ReasonDomainMismatch, now)
	}
	response.OK = true
	response.CommitPolicy = &ipc.CommitPolicyResult{
		TransactionID: identity.TransactionID, Phase: ipc.CommitPolicyStage,
		Status: handler.status,
	}
	return response
}

func (handler *Handler) activate(
	identity ipc.PolicyTransactionIdentity,
	response ipc.Response,
) ipc.Response {
	now, err := handler.checkedNowLocked()
	if err != nil {
		return handler.clockFailure(response)
	}
	if err := handler.revalidate(identity, now); err != nil {
		return handler.lifecycleFailure(err, response)
	}
	committed, err := handler.store.ActivateCandidate(candidateInput(identity), now)
	if err != nil {
		return handler.lifecycleFailure(err, response)
	}
	pointer := committed.Pointer
	handler.setInstalled(pointer, committed.PolicySchema)
	handler.status = statusFromPointer(pointer)
	handler.suspendAuthorizationLocked(policy.ReasonDomainMismatch, now)
	handler.hasPrepared = false
	handler.setActiveIdentity(identity, pointer)
	response.OK = true
	response.CommitPolicy = &ipc.CommitPolicyResult{
		TransactionID: identity.TransactionID, Phase: ipc.CommitPolicyActivate,
		Status: handler.status,
	}
	return response
}

func (handler *Handler) confirm(
	identity ipc.PolicyTransactionIdentity,
	response ipc.Response,
) ipc.Response {
	if !handler.hasActive || handler.activeIdentity != identity {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	now, err := handler.checkedNowLocked()
	if err != nil {
		return handler.clockFailure(response)
	}
	if err := handler.revalidate(identity, now); err != nil {
		return handler.lifecycleFailure(err, response)
	}
	committed, err := handler.store.ConfirmCandidate(candidateInput(identity), now)
	if err != nil {
		return handler.lifecycleFailure(err, response)
	}
	pointer := committed.Pointer
	handler.setInstalled(pointer, committed.PolicySchema)
	handler.status = policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: pointer.Domain,
		State: policy.PolicyActive, BundleGeneration: pointer.BundleGeneration,
		PolicyGeneration: pointer.PolicyGeneration,
		ManifestSHA256:   pointer.ManifestSHA256,
		ActivatedAt:      pointer.ActivatedAt, Reason: policy.ReasonNone,
	}
	handler.setActiveIdentity(identity, pointer)
	handler.authorizationSuspension = clearAuthorizationSuspension()
	response.OK = true
	response.CommitPolicy = &ipc.CommitPolicyResult{
		TransactionID: identity.TransactionID, Phase: ipc.CommitPolicyConfirm,
		Status: handler.status,
	}
	return response
}

func (handler *Handler) abort(
	request ipc.Request,
	response ipc.Response,
) ipc.Response {
	if request.AbortPolicy == nil {
		response.Error = ipc.ErrorInvalidRequest
		return response
	}
	identity := request.AbortPolicy.Transaction
	if handler.store == nil || !handler.hasPrepared || handler.preparedIdentity != identity {
		response.Error = ipc.ErrorPrecondition
		return response
	}
	if err := handler.store.AbortCandidate(candidateInput(identity), handler.now().UTC()); err != nil {
		return handler.lifecycleFailure(err, response)
	}
	handler.status = handler.previousStatus
	handler.authorizationSuspension = handler.previousSuspension
	handler.hasPrepared = false
	response.OK = true
	response.AbortPolicy = &ipc.AbortPolicyResult{
		TransactionID: identity.TransactionID,
		Status:        handler.status,
	}
	return response
}

func (handler *Handler) lifecycleFailure(cause error, response ipc.Response) ipc.Response {
	switch {
	case errors.Is(cause, policystore.ErrCandidateIdentity),
		errors.Is(cause, policystore.ErrRecordConflict),
		errors.Is(cause, policystore.ErrRecordNotFound),
		errors.Is(cause, policystore.ErrGenerationNotFound),
		errors.Is(cause, policystore.ErrStaleActivePointer),
		errors.Is(cause, policyapproval.ErrApprovalExpired),
		errors.Is(cause, policyapproval.ErrApprovalSignature),
		errors.Is(cause, policyapproval.ErrSignerMismatch),
		errors.Is(cause, policyapproval.ErrInvalidApproval),
		errors.Is(cause, policy.ErrInvalidCandidateBundle),
		errors.Is(cause, policy.ErrPolicyDowngrade),
		errors.Is(cause, policy.ErrUnsupportedPolicy),
		errors.Is(cause, policy.ErrUntrustedCompiler),
		errors.Is(cause, policy.ErrRestartRequired):
		response.Error = ipc.ErrorPrecondition
	default:
		response.Error = ipc.ErrorInternal
	}
	return response
}

func (handler *Handler) MutationAllowed() bool {
	if handler == nil {
		return false
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.refreshAuthorizationLocked()
	handler.refreshExistingStateLocked()
	if handler.authorizationSuspension.Suspended {
		return false
	}
	if handler.existingState != nil &&
		handler.existingState.State == policy.ExistingStateGrandfatheredNoncompliant {
		return false
	}
	if handler.store != nil && !handler.hasActive {
		return false
	}
	return handler.status.State != policy.PolicyDomainMismatch &&
		handler.status.State != policy.PolicyAuthorizationSuspended
}

// EvaluateOperatorResume performs side-effect-free shadow evaluation against
// the fully revalidated active policy. Enforcement remains outside this method.
func (handler *Handler) EvaluateOperatorResume(
	domain policy.Domain,
	target string,
	controlStateGeneration uint64,
	planSHA256 string,
) policy.ActionAuthorizationDecision {
	if handler == nil {
		return policy.ActionAuthorizationDecision{Reason: policy.ActionInvalidRequest}
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.refreshAuthorizationLocked()
	now, err := handler.checkedNowLocked()
	if err != nil {
		return policy.ActionAuthorizationDecision{Reason: policy.ActionAuthorizationSuspended}
	}
	return policy.EvaluateActionAuthorization(
		policy.ActionAuthorizationState{
			Status: handler.status, Suspension: handler.authorizationSuspension,
			Payload: handler.activePayload, ControlStateGeneration: controlStateGeneration,
		},
		policy.ActionAuthorizationRequest{
			Domain: domain, Capability: policy.CapabilityOperatorResume,
			BundleGeneration:       handler.status.BundleGeneration,
			DomainPolicyGeneration: handler.status.PolicyGeneration,
			ControlStateGeneration: controlStateGeneration,
			Target:                 target, PlanSHA256: planSHA256,
		},
		now,
	)
}

// ReportGrandfatheredNoncompliance records that established data-plane state
// no longer conforms to the active policy. It has no execution or disconnect
// path and only narrows mutation authority.
func (handler *Handler) ReportGrandfatheredNoncompliance(reconcileBy time.Time) error {
	if handler == nil || reconcileBy.IsZero() {
		return policy.ErrInvalidStatus
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	now, err := handler.checkedNowLocked()
	if err != nil {
		return err
	}
	if !handler.hasActive || handler.status.State != policy.PolicyActive {
		return policy.ErrInvalidStatus
	}
	if handler.existingState != nil {
		return policy.ErrInvalidStatus
	}
	status := policy.ExistingStateStatus{
		Schema: policy.ExistingStateStatusSchema, Domain: handler.domain,
		State:            policy.ExistingStateGrandfatheredNoncompliant,
		BundleGeneration: handler.activeReceipt.BundleGeneration,
		PolicyGeneration: handler.activeReceipt.PolicyGeneration,
		ReportedAt:       now.Format(time.RFC3339Nano),
		ReconcileBy:      reconcileBy.UTC().Format(time.RFC3339Nano),
	}
	if !now.Before(reconcileBy) {
		status.IncidentAt = now.Format(time.RFC3339Nano)
	}
	if status.Validate() != nil {
		return policy.ErrInvalidStatus
	}
	handler.existingState = &status
	return nil
}

// SuspendAuthorization narrows local authority without changing policy state or
// generation. Invalid reasons are ignored so callers cannot invent telemetry.
func (handler *Handler) SuspendAuthorization(reason policy.PolicyReason) {
	if handler == nil || !validSuspensionReason(reason) {
		return
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.suspendAuthorizationLocked(reason, handler.now())
}

func (handler *Handler) revalidate(
	identity ipc.PolicyTransactionIdentity,
	now time.Time,
) error {
	if handler.hasActive && handler.activeIdentity == identity {
		active, err := handler.store.RecoverActive(
			handler.config.Installed,
			handler.config.PinnedPublicKey,
			now,
		)
		if err != nil {
			if reason, ok := classifyActiveFailure(err); ok {
				handler.suspendAuthorizationLocked(reason, now)
			}
			return err
		}
		if identityFromIntent(active.Intent) != identity {
			return policystore.ErrRecordConflict
		}
		return nil
	}
	receipt, err := handler.store.PrepareCandidate(
		candidateInput(identity),
		handler.config.Installed,
		handler.config.PinnedPublicKey,
		now,
	)
	if err != nil {
		return err
	}
	if !receiptMatchesIdentity(receipt, identity, handler.domain) {
		return policystore.ErrRecordConflict
	}
	return nil
}

func (handler *Handler) restoreActive(
	active policystore.RevalidatedActive,
	observedAt time.Time,
) {
	identity := identityFromIntent(active.Intent)
	pointer := policystore.ActivePointer{
		TransactionID: identity.TransactionID, Domain: active.Domain,
		BundleGeneration: active.Generation.Bundle,
		PolicyGeneration: active.Generation.Policy,
		ManifestSHA256:   active.ManifestSHA256,
		PayloadSHA256:    active.PayloadSHA256,
		ApprovalSHA256:   identity.ApprovalSHA256,
		ActivatedAt:      active.ActivatedAt, ConfirmedAt: active.ConfirmedAt,
	}
	handler.setInstalled(pointer, active.Manifest.PolicySchema)
	handler.setActiveIdentity(identity, pointer)
	handler.activePayload = cloneDomainPayload(active.Payload)
	state := policy.PolicyDomainMismatch
	reason := policy.ReasonDomainMismatch
	if active.ConfirmedAt != "" {
		state = policy.PolicyActive
		reason = policy.ReasonNone
	}
	handler.status = policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: active.Domain, State: state,
		BundleGeneration: active.Generation.Bundle,
		PolicyGeneration: active.Generation.Policy,
		ManifestSHA256:   active.ManifestSHA256,
		ActivatedAt:      active.ActivatedAt, Reason: reason,
	}
	if active.ConfirmedAt == "" {
		handler.suspendAuthorizationLocked(policy.ReasonDomainMismatch, observedAt)
	} else {
		handler.authorizationSuspension = clearAuthorizationSuspension()
	}
}

func (handler *Handler) restorePending(
	intent policystore.CommitIntent,
	now time.Time,
) error {
	identity := identityFromIntent(intent)
	receipt, err := handler.store.PrepareCandidate(
		candidateInput(identity),
		handler.config.Installed,
		handler.config.PinnedPublicKey,
		now,
	)
	if err != nil {
		return err
	}
	if !receiptMatchesIdentity(receipt, identity, handler.domain) {
		return policystore.ErrRecordConflict
	}
	handler.previousStatus = handler.status
	handler.preparedIdentity = identity
	handler.hasPrepared = true
	handler.status = stagedMismatchStatus(identity, handler.domain)
	handler.suspendAuthorizationLocked(policy.ReasonDomainMismatch, now)
	return nil
}

func (handler *Handler) refreshAuthorizationLocked() {
	if handler.store == nil || (!handler.hasActive && !handler.authorizationSuspension.Suspended) {
		return
	}
	now, err := handler.checkedNowLocked()
	if err != nil {
		return
	}
	active, err := handler.store.RecoverActive(
		handler.config.Installed,
		handler.config.PinnedPublicKey,
		now,
	)
	if err != nil {
		if errors.Is(err, policystore.ErrRecordNotFound) && handler.hasActive {
			handler.suspendAuthorizationLocked(policy.ReasonCorruption, now)
			return
		}
		if reason, ok := classifyActiveFailure(err); ok {
			handler.suspendAuthorizationLocked(reason, now)
		}
		return
	}
	if handler.hasActive && identityFromIntent(active.Intent) != handler.activeIdentity {
		handler.suspendAuthorizationLocked(policy.ReasonDigestMismatch, now)
		return
	}
	handler.restoreActive(active, now)
}

func (handler *Handler) refreshExistingStateLocked() {
	if handler.existingState == nil || handler.existingState.IncidentAt != "" {
		return
	}
	now, err := handler.checkedNowLocked()
	if err != nil {
		return
	}
	reconcileBy, err := time.Parse(time.RFC3339Nano, handler.existingState.ReconcileBy)
	if err != nil || now.Before(reconcileBy) {
		return
	}
	handler.existingState.IncidentAt = now.Format(time.RFC3339Nano)
}

func (handler *Handler) checkedNowLocked() (time.Time, error) {
	if handler == nil || handler.now == nil || handler.monotonicNow == nil ||
		handler.clockGuard == nil {
		return time.Time{}, policyclock.ErrInvalidClock
	}
	now := handler.now().UTC()
	sample := policyclock.Sample{
		WallUTC:     now,
		MonotonicNS: handler.monotonicNow().Nanoseconds(),
	}
	if err := handler.clockGuard.Observe(sample); err != nil {
		handler.suspendAuthorizationLocked(policy.ReasonClockAnomaly, now)
		return time.Time{}, err
	}
	return now, nil
}

func (handler *Handler) clockFailure(response ipc.Response) ipc.Response {
	response.Error = ipc.ErrorPrecondition
	return response
}

func (handler *Handler) suspendAuthorizationLocked(reason policy.PolicyReason, at time.Time) {
	if !validSuspensionReason(reason) || at.IsZero() {
		return
	}
	since := at.UTC().Format(time.RFC3339Nano)
	if handler.authorizationSuspension.Suspended &&
		handler.authorizationSuspension.Since != "" {
		since = handler.authorizationSuspension.Since
	}
	handler.authorizationSuspension = policy.AuthorizationSuspension{
		Schema:    policy.AuthorizationSuspensionSchema,
		Suspended: true,
		Reason:    reason,
		Since:     since,
	}
}

func clearAuthorizationSuspension() policy.AuthorizationSuspension {
	return policy.AuthorizationSuspension{
		Schema: policy.AuthorizationSuspensionSchema,
		Reason: policy.ReasonNone,
	}
}

func validSuspensionReason(reason policy.PolicyReason) bool {
	switch reason {
	case policy.ReasonCorruption,
		policy.ReasonInvalidSignature,
		policy.ReasonDigestMismatch,
		policy.ReasonDomainMismatch,
		policy.ReasonClockAnomaly,
		policy.ReasonIPCOwnership:
		return true
	default:
		return false
	}
}

func classifyActiveFailure(cause error) (policy.PolicyReason, bool) {
	switch {
	case errors.Is(cause, policystore.ErrActiveClockAnomaly),
		errors.Is(cause, policyapproval.ErrApprovalExpired):
		return policy.ReasonClockAnomaly, true
	case errors.Is(cause, policyapproval.ErrApprovalSignature),
		errors.Is(cause, policyapproval.ErrSignerMismatch),
		errors.Is(cause, policyapproval.ErrInvalidApproval):
		return policy.ReasonInvalidSignature, true
	case errors.Is(cause, policystore.ErrActivePointerConsistency),
		errors.Is(cause, policy.ErrInvalidCandidateBundle):
		return policy.ReasonDigestMismatch, true
	case errors.Is(cause, policystore.ErrInvalidRecord),
		errors.Is(cause, policystore.ErrRecordConflict),
		errors.Is(cause, policystore.ErrGenerationNotFound),
		errors.Is(cause, policystore.ErrInvalidArtifact),
		errors.Is(cause, policystore.ErrInsecureArtifact),
		errors.Is(cause, policystore.ErrInsecureStore),
		errors.Is(cause, policystore.ErrStoreUnavailable),
		errors.Is(cause, policyapproval.ErrInvalidReview):
		return policy.ReasonCorruption, true
	default:
		return policy.ReasonNone, false
	}
}

func (handler *Handler) setInstalled(pointer policystore.ActivePointer, policySchema uint16) {
	handler.config.Installed.CurrentPolicySchema = policySchema
	handler.config.Installed.CurrentBundleGeneration = pointer.BundleGeneration
	handler.config.Installed.CurrentPolicyGeneration = pointer.PolicyGeneration
	handler.config.Installed.CurrentPayloadSHA256 = pointer.PayloadSHA256
}

func statusFromPointer(pointer policystore.ActivePointer) policy.Status {
	state := policy.PolicyDomainMismatch
	reason := policy.ReasonDomainMismatch
	if pointer.ConfirmedAt != "" {
		state = policy.PolicyActive
		reason = policy.ReasonNone
	}
	return policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: pointer.Domain, State: state,
		BundleGeneration: pointer.BundleGeneration,
		PolicyGeneration: pointer.PolicyGeneration,
		ManifestSHA256:   pointer.ManifestSHA256,
		ActivatedAt:      pointer.ActivatedAt, Reason: reason,
	}
}

func stagedMismatchStatus(
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
) policy.Status {
	policyGeneration := identity.RootPolicyGeneration
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
	}
	return policy.Status{
		Schema: policy.PolicyStatusSchema, Domain: domain,
		State:            policy.PolicyDomainMismatch,
		BundleGeneration: identity.BundleGeneration,
		PolicyGeneration: policyGeneration,
		ManifestSHA256:   identity.ManifestSHA256,
		Reason:           policy.ReasonDomainMismatch,
	}
}

func (handler *Handler) setActiveIdentity(
	identity ipc.PolicyTransactionIdentity,
	pointer policystore.ActivePointer,
) {
	if !handler.hasActive || handler.activeIdentity != identity {
		handler.activePayload = policy.DomainPayload{}
	}
	handler.activeIdentity = identity
	handler.activeReceipt = policystore.PrepareReceipt{
		Schema:        policystore.PrepareReceiptSchema,
		TransactionID: identity.TransactionID, Domain: pointer.Domain,
		BundleGeneration: pointer.BundleGeneration,
		PolicyGeneration: pointer.PolicyGeneration,
		ManifestSHA256:   pointer.ManifestSHA256,
		PayloadSHA256:    pointer.PayloadSHA256,
		ApprovalSHA256:   pointer.ApprovalSHA256,
		PreparedAt:       pointer.ActivatedAt,
	}
	handler.hasActive = true
	if handler.existingState != nil &&
		(handler.existingState.Domain != pointer.Domain ||
			handler.existingState.BundleGeneration != pointer.BundleGeneration ||
			handler.existingState.PolicyGeneration != pointer.PolicyGeneration) {
		handler.existingState = nil
	}
}

func cloneDomainPayload(source policy.DomainPayload) policy.DomainPayload {
	cloned := source
	cloned.Rules = append([]policy.Rule(nil), source.Rules...)
	for index := range cloned.Rules {
		selector := cloned.Rules[index].Selector
		if selector.Endpoint != nil {
			endpoint := *selector.Endpoint
			endpoint.Ports = append([]policy.PortRange(nil), endpoint.Ports...)
			selector.Endpoint = &endpoint
		}
		if selector.Route != nil {
			route := *selector.Route
			selector.Route = &route
		}
		if selector.Action != nil {
			action := *selector.Action
			selector.Action = &action
		}
		if selector.Credential != nil {
			credential := *selector.Credential
			selector.Credential = &credential
		}
		cloned.Rules[index].Selector = selector
	}
	cloned.Leases = append([]policy.AuthorizationLease(nil), source.Leases...)
	for index := range cloned.Leases {
		cloned.Leases[index].SelectorIDs = append(
			[]string(nil),
			source.Leases[index].SelectorIDs...,
		)
	}
	return cloned
}

func cloneExistingState(source *policy.ExistingStateStatus) *policy.ExistingStateStatus {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func identityFromIntent(intent policystore.CommitIntent) ipc.PolicyTransactionIdentity {
	return ipc.PolicyTransactionIdentity{
		TransactionID:        intent.TransactionID,
		BundleGeneration:     intent.BundleGeneration,
		RootPolicyGeneration: intent.RootPolicyGeneration,
		UserPolicyGeneration: intent.UserPolicyGeneration,
		ManifestSHA256:       intent.ManifestSHA256,
		RootPayloadSHA256:    intent.RootPayloadSHA256,
		UserPayloadSHA256:    intent.UserPayloadSHA256,
		ApprovalSHA256:       intent.ApprovalSHA256,
	}
}

func prepareResult(receipt policystore.PrepareReceipt) ipc.PreparePolicyResult {
	return ipc.PreparePolicyResult{
		TransactionID: receipt.TransactionID, Domain: receipt.Domain,
		BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration,
		ManifestSHA256:   receipt.ManifestSHA256,
		PayloadSHA256:    receipt.PayloadSHA256,
		ApprovalSHA256:   receipt.ApprovalSHA256,
	}
}

func candidateInput(identity ipc.PolicyTransactionIdentity) policystore.PrepareCandidateInput {
	return policystore.PrepareCandidateInput{
		TransactionID:        identity.TransactionID,
		BundleGeneration:     identity.BundleGeneration,
		RootPolicyGeneration: identity.RootPolicyGeneration,
		UserPolicyGeneration: identity.UserPolicyGeneration,
		ManifestSHA256:       identity.ManifestSHA256,
		RootPayloadSHA256:    identity.RootPayloadSHA256,
		UserPayloadSHA256:    identity.UserPayloadSHA256,
		ApprovalSHA256:       identity.ApprovalSHA256,
	}
}

func receiptMatchesIdentity(
	receipt policystore.PrepareReceipt,
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
) bool {
	policyGeneration := identity.RootPolicyGeneration
	payloadSHA256 := identity.RootPayloadSHA256
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
		payloadSHA256 = identity.UserPayloadSHA256
	}
	return receipt.TransactionID == identity.TransactionID && receipt.Domain == domain &&
		receipt.BundleGeneration == identity.BundleGeneration &&
		receipt.PolicyGeneration == policyGeneration &&
		receipt.ManifestSHA256 == identity.ManifestSHA256 &&
		receipt.PayloadSHA256 == payloadSHA256 &&
		receipt.ApprovalSHA256 == identity.ApprovalSHA256
}

func (handler *Handler) prepareFailure(
	identity ipc.PolicyTransactionIdentity,
	cause error,
	response ipc.Response,
) ipc.Response {
	_, _, report := classifyPrepareFailure(cause)
	if report {
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
