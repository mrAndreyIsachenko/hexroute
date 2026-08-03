package policyqualification

import (
	"errors"
	"time"
)

const MinimumEligibleDuration = 72 * time.Hour

type Evidence struct {
	EligibleDuration          time.Duration
	SleepWakeCycles           uint32
	RebootObserved            bool
	InvalidSignatureInjected  bool
	SelectorConflictInjected  bool
	StaleGenerationInjected   bool
	CrossDomainCrashInjected  bool
	UnexplainedSafetyMismatch uint32
}

// Gate can only become complete through validation of the full qualification
// evidence. Its zero value always fails closed.
type Gate struct {
	complete bool
}

var ErrIncompleteEvidence = errors.New("incomplete policy enforcement qualification")

func Validate(evidence Evidence) (Gate, error) {
	if evidence.EligibleDuration < MinimumEligibleDuration ||
		evidence.SleepWakeCycles < 2 || !evidence.RebootObserved ||
		!evidence.InvalidSignatureInjected || !evidence.SelectorConflictInjected ||
		!evidence.StaleGenerationInjected || !evidence.CrossDomainCrashInjected ||
		evidence.UnexplainedSafetyMismatch != 0 {
		return Gate{}, ErrIncompleteEvidence
	}
	return Gate{complete: true}, nil
}

func (gate Gate) Complete() bool {
	return gate.complete
}
