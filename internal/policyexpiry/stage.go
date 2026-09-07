// Package policyexpiry decides when the end of a policy generation's validity
// should be announced.
//
// It exists as pure logic over a timestamp because the thing being decided is a
// deadline, and a deadline is the one part of this that can be reasoned about
// without a store, a daemon or a clock of its own.
package policyexpiry

import "time"

type Stage string

const (
	// StageNone is the ordinary case: the generation has more time than the
	// ceremony needs.
	StageNone Stage = "none"
	// StageWarning is the ceremony's lead time. Renewing means a working signed
	// signer application, the operator physically present for user presence,
	// root for the root store, and a full compile, diff, replay, sign, install
	// and activate across both domains. That is an evening, it cannot be done
	// remotely or unattended, and so the warning has to survive a trip.
	StageWarning Stage = "warning"
	// StageUrgent is close enough that other plans move.
	StageUrgent Stage = "urgent"
	// StageLapsed is after the fact. It is announced because nothing else does:
	// an expired generation raises no suspension, so without this the machine
	// would sit quietly with no active policy.
	StageLapsed Stage = "lapsed"
)

const (
	WarningThreshold = 7 * 24 * time.Hour
	UrgentThreshold  = 48 * time.Hour
)

// StageAt answers which announcement a generation's remaining validity calls
// for. Thresholds are absolute rather than a fraction of the generation's own
// window: what they measure is how long renewal takes, which has nothing to do
// with how long the generation was signed for. A three-day emergency generation
// would warn a day in under a proportional rule, which is no warning at all.
func StageAt(expiresAt string, now time.Time) (Stage, bool) {
	if expiresAt == "" || now.IsZero() {
		return StageNone, false
	}
	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return StageNone, false
	}
	remaining := expiry.Sub(now.UTC())
	switch {
	case remaining <= 0:
		return StageLapsed, true
	case remaining <= UrgentThreshold:
		return StageUrgent, true
	case remaining <= WarningThreshold:
		return StageWarning, true
	default:
		return StageNone, true
	}
}

// IncidentID names the announcement. The stage is part of the identity rather
// than of the body because delivery is deduplicated by incident, generation and
// status: crossing a threshold announces once for that generation, and crossing
// the next one is a different announcement rather than a repeat of the same one.
func (stage Stage) IncidentID() string {
	switch stage {
	case StageWarning:
		return "policy-expiry-warning"
	case StageUrgent:
		return "policy-expiry-urgent"
	case StageLapsed:
		return "policy-expiry-lapsed"
	default:
		return ""
	}
}

func (stage Stage) Announces() bool { return stage.IncidentID() != "" }
