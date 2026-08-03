package operator

import (
	"errors"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/resumeplan"
)

type ResumeFunc func(uint64, control.Tick) (control.Snapshot, error)

type ResumePolicyEvaluator interface {
	EvaluateOperatorResume(
		policy.Domain,
		string,
		uint64,
		string,
	) policy.ActionAuthorizationDecision
}

type Controller struct {
	mu             sync.Mutex
	role           ipc.DaemonRole
	mode           ipc.RuntimeMode
	allowedTargets map[control.Component]struct{}
	snapshot       control.Snapshot
	lastReason     control.Reason
	resume         ResumeFunc
	resumePolicy   ResumePolicyEvaluator
	now            func() control.Tick
}

var ErrInvalidController = errors.New("invalid operator controller")

func NewController(
	role ipc.DaemonRole,
	mode ipc.RuntimeMode,
	targets []control.Component,
	snapshot control.Snapshot,
	reason control.Reason,
	resume ResumeFunc,
	now func() control.Tick,
) (*Controller, error) {
	if !validRole(role) ||
		!validMode(mode) ||
		len(targets) == 0 ||
		!validSnapshot(snapshot) ||
		!reason.Valid() ||
		now == nil {
		return nil, ErrInvalidController
	}
	allowed := make(map[control.Component]struct{}, len(targets))
	for _, target := range targets {
		if !targetAllowedForRole(role, target) {
			return nil, ErrInvalidController
		}
		allowed[target] = struct{}{}
	}
	return &Controller{
		role:           role,
		mode:           mode,
		allowedTargets: allowed,
		snapshot:       snapshot,
		lastReason:     reason,
		resume:         resume,
		now:            now,
	}, nil
}

// SetResumePolicyEvaluator enables observational policy evaluation. The
// decision cannot block or authorize the legacy resume path in shadow mode.
func (controller *Controller) SetResumePolicyEvaluator(evaluator ResumePolicyEvaluator) error {
	if controller == nil || evaluator == nil {
		return ErrInvalidController
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.resumePolicy = evaluator
	return nil
}

func (controller *Controller) Update(
	snapshot control.Snapshot,
	reason control.Reason,
) error {
	if controller == nil || !validSnapshot(snapshot) || !reason.Valid() {
		return ErrInvalidController
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if snapshot.Generation < controller.snapshot.Generation ||
		snapshot.LastTick < controller.snapshot.LastTick {
		return control.ErrStaleGeneration
	}
	controller.snapshot = snapshot
	controller.lastReason = reason
	return nil
}

func (controller *Controller) Handle(request ipc.Request) ipc.Response {
	response := ipc.Response{
		Version:   ipc.ProtocolVersion,
		RequestID: request.RequestID,
	}
	if controller == nil {
		response.Error = ipc.ErrorInternal
		return response
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()

	switch request.Action {
	case ipc.ActionStatus:
		status := controller.status()
		response.OK = true
		response.Status = &status
	case ipc.ActionExportDiagnostics:
		diagnostics := controller.diagnostics()
		response.OK = true
		response.Diagnostics = &diagnostics
	case ipc.ActionResumeTarget:
		return controller.handleResume(request, response)
	default:
		response.Error = ipc.ErrorInvalidRequest
	}
	return response
}

func (controller *Controller) handleResume(
	request ipc.Request,
	response ipc.Response,
) ipc.Response {
	if _, allowed := controller.allowedTargets[request.Target]; !allowed {
		response.Error = ipc.ErrorUnauthorized
		return response
	}
	if request.ExpectedGeneration != controller.snapshot.Generation {
		response.Error = ipc.ErrorStaleGeneration
		return response
	}
	if controller.snapshot.State != control.StateSafeMode || controller.resume == nil {
		response.Error = ipc.ErrorPrecondition
		return response
	}

	before := controller.snapshot
	at := controller.now()
	if controller.resumePolicy != nil {
		if plan, planErr := resumeplan.Build(request.Target, before, at); planErr == nil {
			controller.resumePolicy.EvaluateOperatorResume(
				domainForRole(controller.role),
				string(request.Target),
				request.ExpectedGeneration,
				plan.Digest(),
			)
		}
	}
	after, err := controller.resume(request.ExpectedGeneration, at)
	switch {
	case errors.Is(err, control.ErrStaleGeneration):
		response.Error = ipc.ErrorStaleGeneration
		return response
	case errors.Is(err, control.ErrResumePrecondition):
		response.Error = ipc.ErrorPrecondition
		return response
	case err != nil:
		response.Error = ipc.ErrorInternal
		return response
	}
	if !validSnapshot(after) ||
		after.State != control.StateDegraded ||
		after.Generation != before.Generation+1 {
		response.Error = ipc.ErrorInternal
		return response
	}
	controller.snapshot = after
	controller.lastReason = control.ReasonOperatorResume
	result := ipc.ResumeResult{
		Role:               controller.role,
		Target:             request.Target,
		PreviousState:      before.State,
		State:              after.State,
		PreviousGeneration: before.Generation,
		Generation:         after.Generation,
	}
	response.OK = true
	response.Resume = &result
	return response
}

func domainForRole(role ipc.DaemonRole) policy.Domain {
	if role == ipc.RoleRoot {
		return policy.DomainRoot
	}
	return policy.DomainUser
}

func (controller *Controller) status() ipc.Status {
	return ipc.Status{
		Role:       controller.role,
		Mode:       controller.mode,
		State:      controller.snapshot.State,
		Generation: controller.snapshot.Generation,
		SafeMode:   controller.snapshot.State == control.StateSafeMode,
	}
}

func (controller *Controller) diagnostics() ipc.Diagnostics {
	return ipc.Diagnostics{
		Status:              controller.status(),
		ConsecutiveFailures: controller.snapshot.ConsecutiveFailures,
		Attempts:            controller.snapshot.Attempts,
		LastTick:            controller.snapshot.LastTick,
		RecoveringSince:     controller.snapshot.RecoveringSince,
		NextActionAt:        controller.snapshot.NextActionAt,
		SafeUntil:           controller.snapshot.SafeUntil,
		LastReason:          controller.lastReason,
	}
}

func validSnapshot(snapshot control.Snapshot) bool {
	return snapshot.SchemaVersion == control.SnapshotSchemaVersion &&
		snapshot.State.Valid() &&
		snapshot.LastTick >= 0 &&
		snapshot.RecoveringSince >= 0 &&
		snapshot.NextActionAt >= 0 &&
		snapshot.SafeUntil >= 0
}

func validRole(role ipc.DaemonRole) bool {
	return role == ipc.RoleRoot || role == ipc.RoleUser
}

func validMode(mode ipc.RuntimeMode) bool {
	return mode == ipc.ModeObserveOnly || mode == ipc.ModeActive
}

func targetAllowedForRole(role ipc.DaemonRole, target control.Component) bool {
	switch role {
	case ipc.RoleRoot:
		switch target {
		case control.ComponentNetwork,
			control.ComponentTunnel,
			control.ComponentRoutes,
			control.ComponentCodex,
			control.ComponentTelegram:
			return true
		}
	case ipc.RoleUser:
		return target == control.ComponentPritunl
	}
	return false
}
