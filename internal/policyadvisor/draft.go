package policyadvisor

import (
	"errors"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"go.yaml.in/yaml/v3"
)

const (
	DraftSchema       = "hexroute.policy-advisor-draft.v1"
	TargetPlaceholder = "operator-supplied-target"
	MaxEvidenceCount  = 1_000_000
)

type DraftStatus string

const StatusUnsigned DraftStatus = "unsigned_draft"

type EvidenceReason string

const (
	ReasonRepeatedDenial       EvidenceReason = "repeated_denial"
	ReasonRepeatedSuppression  EvidenceReason = "repeated_suppression"
	ReasonGenerationDivergence EvidenceReason = "generation_divergence"
)

type SuggestedRule struct {
	Effect            policy.Effect     `json:"effect" yaml:"effect"`
	Domain            policy.Domain     `json:"domain" yaml:"domain"`
	Capability        policy.Capability `json:"capability" yaml:"capability"`
	TargetPlaceholder string            `json:"target_placeholder" yaml:"target_placeholder"`
}

// Draft is intentionally not an operator-policy source. It contains no live
// selector or executable target and must be reviewed and translated manually.
type Draft struct {
	Schema          string         `json:"schema" yaml:"schema"`
	Status          DraftStatus    `json:"status" yaml:"status"`
	DraftID         metadata.UUID  `json:"draft_id" yaml:"draft_id"`
	CreatedAt       string         `json:"created_at" yaml:"created_at"`
	Reason          EvidenceReason `json:"reason" yaml:"reason"`
	EvidenceCount   uint32         `json:"evidence_count" yaml:"evidence_count"`
	FirstObservedAt string         `json:"first_observed_at" yaml:"first_observed_at"`
	LastObservedAt  string         `json:"last_observed_at" yaml:"last_observed_at"`
	SuggestedRule   SuggestedRule  `json:"suggested_rule" yaml:"suggested_rule"`
}

var ErrInvalidDraft = errors.New("invalid policy advisor draft")

func (draft Draft) Validate() error {
	createdAt, createdOK := canonicalTime(draft.CreatedAt)
	firstAt, firstOK := canonicalTime(draft.FirstObservedAt)
	lastAt, lastOK := canonicalTime(draft.LastObservedAt)
	if draft.Schema != DraftSchema || draft.Status != StatusUnsigned ||
		metadataUUIDInvalid(draft.DraftID) || !draft.Reason.Valid() ||
		draft.EvidenceCount == 0 || draft.EvidenceCount > MaxEvidenceCount ||
		!createdOK || !firstOK || !lastOK || lastAt.Before(firstAt) ||
		createdAt.Before(lastAt) || draft.SuggestedRule.Validate() != nil {
		return ErrInvalidDraft
	}
	return nil
}

func (rule SuggestedRule) Validate() error {
	if (rule.Effect != policy.EffectAllow && rule.Effect != policy.EffectDeny) ||
		!rule.Domain.Valid() || rule.Capability != policy.CapabilityOperatorResume ||
		rule.TargetPlaceholder != TargetPlaceholder {
		return ErrInvalidDraft
	}
	return nil
}

func (reason EvidenceReason) Valid() bool {
	switch reason {
	case ReasonRepeatedDenial, ReasonRepeatedSuppression, ReasonGenerationDivergence:
		return true
	default:
		return false
	}
}

func EncodeYAML(draft Draft) ([]byte, error) {
	if draft.Validate() != nil {
		return nil, ErrInvalidDraft
	}
	encoded, err := yaml.Marshal(draft)
	if err != nil {
		return nil, ErrInvalidDraft
	}
	return encoded, nil
}

func metadataUUIDInvalid(value metadata.UUID) bool {
	_, err := metadata.ParseUUID(string(value))
	return err != nil
}

func canonicalTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.UTC().Format(time.RFC3339Nano) != value {
		return time.Time{}, false
	}
	return parsed, true
}
