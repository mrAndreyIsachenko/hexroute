package control

type State string

const (
	StateSuspended  State = "SUSPENDED"
	StateHealthy    State = "HEALTHY"
	StateDegraded   State = "DEGRADED"
	StateRecovering State = "RECOVERING"
	StateSafeMode   State = "SAFE_MODE"
)

func (state State) Valid() bool {
	switch state {
	case StateSuspended, StateHealthy, StateDegraded, StateRecovering, StateSafeMode:
		return true
	default:
		return false
	}
}

type Component string

const (
	ComponentNetwork  Component = "network"
	ComponentTunnel   Component = "tunnel"
	ComponentRoutes   Component = "routes"
	ComponentPritunl  Component = "pritunl"
	ComponentCodex    Component = "codex"
	ComponentTelegram Component = "telegram"
	ComponentRuntime  Component = "runtime"
)

type Health string

const (
	HealthUnknown   Health = "unknown"
	HealthReady     Health = "ready"
	HealthFailed    Health = "failed"
	HealthSuspended Health = "suspended"
)

type Observation struct {
	Component     Component `json:"component"`
	Health        Health    `json:"health"`
	Sequence      uint64    `json:"sequence"`
	MonotonicTick Tick      `json:"monotonic_tick"`
	Reason        Reason    `json:"reason"`
}

type Reason string

const (
	ReasonNone                 Reason = "none"
	ReasonProbeSucceeded       Reason = "probe_succeeded"
	ReasonProbeFailed          Reason = "probe_failed"
	ReasonFailureThreshold     Reason = "failure_threshold"
	ReasonRecoveryAllowed      Reason = "recovery_allowed"
	ReasonRecoveryBudget       Reason = "recovery_budget_exhausted"
	ReasonVerificationPassed   Reason = "verification_passed"
	ReasonCooldownElapsed      Reason = "cooldown_elapsed"
	ReasonDependenciesReady    Reason = "dependencies_ready"
	ReasonDependenciesNotReady Reason = "dependencies_not_ready"
	ReasonIntentionalSleep     Reason = "intentional_sleep"
	ReasonOperatorResume       Reason = "operator_resume"
)

// An observation cycle's own causes. A reason beside a health result has to
// name what failed, and the eleven places a root cycle can fail are not probes:
// reporting them all as a failed probe sent a reader looking for one that had
// not run. These names are what the cycle kept, not what its state implies.
const (
	ReasonPowerUnreadable        Reason = "power_unreadable"
	ReasonProcessUnreadable      Reason = "process_unreadable"
	ReasonTunnelAbsent           Reason = "tunnel_absent"
	ReasonPhysicalNetworkUnready Reason = "physical_network_unready"
	ReasonTUNUnreadable          Reason = "tun_unreadable"
	ReasonManagedTUNAbsent       Reason = "managed_tun_absent"
	ReasonRouteUnreadable        Reason = "route_unreadable"
	ReasonEndpointUnreadable     Reason = "endpoint_unreadable"
	ReasonOuterPathAbsent        Reason = "outer_path_absent"
	ReasonPlanRefused            Reason = "plan_refused"
)

func (reason Reason) Valid() bool {
	switch reason {
	case ReasonNone,
		ReasonProbeSucceeded,
		ReasonProbeFailed,
		ReasonFailureThreshold,
		ReasonRecoveryAllowed,
		ReasonRecoveryBudget,
		ReasonVerificationPassed,
		ReasonCooldownElapsed,
		ReasonDependenciesReady,
		ReasonDependenciesNotReady,
		ReasonIntentionalSleep,
		ReasonOperatorResume,
		ReasonPowerUnreadable,
		ReasonProcessUnreadable,
		ReasonTunnelAbsent,
		ReasonPhysicalNetworkUnready,
		ReasonTUNUnreadable,
		ReasonManagedTUNAbsent,
		ReasonRouteUnreadable,
		ReasonEndpointUnreadable,
		ReasonPlanRefused:
		return true
	default:
		return false
	}
}

type Readiness string

const (
	ReadinessUnknown Readiness = "unknown"
	ReadinessReady   Readiness = "ready"
	ReadinessBlocked Readiness = "blocked"
)

type Dependencies struct {
	PhysicalNetwork Readiness `json:"physical_network"`
	Tunnel          Readiness `json:"tunnel"`
	ScopedRoutes    Readiness `json:"scoped_routes"`
}

func (dependencies Dependencies) OuterReady() bool {
	return dependencies.PhysicalNetwork == ReadinessReady &&
		dependencies.Tunnel == ReadinessReady &&
		dependencies.ScopedRoutes == ReadinessReady
}

type ActionKind string

const (
	ActionRestart           ActionKind = "restart"
	ActionApplyScopedRoutes ActionKind = "apply_scoped_routes"
	ActionSelectIngress     ActionKind = "select_ingress"
)

type ActionTarget string

const (
	TargetSingBox        ActionTarget = "sing_box"
	TargetScopedRoutes   ActionTarget = "scoped_routes"
	TargetIngress        ActionTarget = "ingress_selector"
	TargetPritunlService ActionTarget = "pritunl_service"
)

type Action struct {
	Kind       ActionKind   `json:"kind"`
	Target     ActionTarget `json:"target"`
	Generation uint64       `json:"generation"`
	Reason     Reason       `json:"reason"`
}
