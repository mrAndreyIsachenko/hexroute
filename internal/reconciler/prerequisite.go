package reconciler

import (
	"errors"
	"regexp"
)

const (
	PrerequisiteSchema = "hexroute.reconciler-prerequisites.v1"

	AtomicPolicyEvidenceSchema = "hexroute.policy-qualification-evidence.v1"
)

type PrerequisiteID string

const (
	PrerequisiteAtomicPolicyGenerations            PrerequisiteID = "add-atomic-policy-generations"
	PrerequisiteObservableConnectivityStateMachine PrerequisiteID = "add-observable-connectivity-state-machine"
)

type PrerequisiteStatus string

const (
	PrerequisiteComplete   PrerequisiteStatus = "complete"
	PrerequisiteCollecting PrerequisiteStatus = "collecting"
	PrerequisitePlanned    PrerequisiteStatus = "planned"
	PrerequisiteInvalid    PrerequisiteStatus = "invalid"
)

type GateReason string

const (
	GateReasonNone                   GateReason = "none"
	GateReasonMissingPrerequisite    GateReason = "missing_prerequisite"
	GateReasonIncompletePrerequisite GateReason = "incomplete_prerequisite"
	GateReasonInvalidPrerequisite    GateReason = "invalid_prerequisite"
	GateReasonUnsyncedBaseline       GateReason = "unsynchronized_baseline"
	GateReasonGenerationMismatch     GateReason = "generation_mismatch"
)

type PrerequisiteRequirement struct {
	ID                   PrerequisiteID `json:"id"`
	ChangeReference      string         `json:"change_reference"`
	BaselineSpecPath     string         `json:"baseline_spec_path"`
	EvidenceSchema       string         `json:"evidence_schema"`
	QualificationSummary string         `json:"qualification_summary"`
}

type GenerationBinding struct {
	BundleGeneration     uint64 `json:"bundle_generation"`
	RootPolicyGeneration uint64 `json:"root_policy_generation"`
	UserPolicyGeneration uint64 `json:"user_policy_generation"`
	ManifestSHA256       string `json:"manifest_sha256"`
}

type PrerequisiteEvidence struct {
	ID                       PrerequisiteID     `json:"id"`
	Status                   PrerequisiteStatus `json:"status"`
	Valid                    bool               `json:"valid"`
	BaselineSynchronized     bool               `json:"baseline_synchronized"`
	Binding                  GenerationBinding  `json:"binding"`
	EvidenceSHA256           string             `json:"evidence_sha256"`
	BaselineRevisionSHA256   string             `json:"baseline_revision_sha256"`
	QualificationArtifactRef string             `json:"qualification_artifact_ref"`
}

type Gate struct {
	ready        bool
	reason       GateReason
	prerequisite PrerequisiteID
}

var (
	ErrInvalidPrerequisiteInput = errors.New("invalid reconciliation prerequisite input")
	digestPattern               = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func RequiredPrerequisites() []PrerequisiteRequirement {
	return []PrerequisiteRequirement{
		{
			ID:                   PrerequisiteAtomicPolicyGenerations,
			ChangeReference:      "openspec/changes/archive/2026-08-15-add-atomic-policy-generations",
			BaselineSpecPath:     "openspec/specs/atomic-policy-generations/spec.md",
			EvidenceSchema:       AtomicPolicyEvidenceSchema,
			QualificationSummary: "72h eligible window, two sleep/wake cycles, one reboot and controlled fault outcomes",
		},
		{
			ID:                   PrerequisiteObservableConnectivityStateMachine,
			ChangeReference:      "openspec/changes/add-observable-connectivity-state-machine",
			BaselineSpecPath:     "openspec/specs/observable-connectivity-state-machine/spec.md",
			EvidenceSchema:       "hexroute.observable-connectivity-qualification.v1",
			QualificationSummary: "complete shadow qualification and synchronized baseline requirements",
		},
	}
}

func EvaluatePrerequisites(expected GenerationBinding, evidence []PrerequisiteEvidence) (Gate, error) {
	if !expected.valid() {
		return Gate{}, ErrInvalidPrerequisiteInput
	}
	byID := make(map[PrerequisiteID]PrerequisiteEvidence, len(evidence))
	for _, item := range evidence {
		if !item.ID.valid() {
			return Gate{}, ErrInvalidPrerequisiteInput
		}
		if _, exists := byID[item.ID]; exists {
			return Gate{}, ErrInvalidPrerequisiteInput
		}
		byID[item.ID] = item
	}
	for _, required := range RequiredPrerequisites() {
		item, exists := byID[required.ID]
		if !exists {
			return blocked(GateReasonMissingPrerequisite, required.ID), nil
		}
		if !item.Valid || item.Status == PrerequisiteInvalid || !validDigest(item.EvidenceSHA256) ||
			!validDigest(item.BaselineRevisionSHA256) || item.QualificationArtifactRef == "" {
			return blocked(GateReasonInvalidPrerequisite, required.ID), nil
		}
		if item.Status != PrerequisiteComplete {
			return blocked(GateReasonIncompletePrerequisite, required.ID), nil
		}
		if !item.BaselineSynchronized {
			return blocked(GateReasonUnsyncedBaseline, required.ID), nil
		}
		if item.Binding != expected {
			return blocked(GateReasonGenerationMismatch, required.ID), nil
		}
	}
	return Gate{ready: true, reason: GateReasonNone}, nil
}

func (gate Gate) Ready() bool {
	return gate.ready
}

func (gate Gate) Reason() GateReason {
	return gate.reason
}

func (gate Gate) Prerequisite() PrerequisiteID {
	return gate.prerequisite
}

func blocked(reason GateReason, prerequisite PrerequisiteID) Gate {
	return Gate{reason: reason, prerequisite: prerequisite}
}

func (binding GenerationBinding) valid() bool {
	return binding.BundleGeneration > 0 &&
		binding.RootPolicyGeneration > 0 &&
		binding.UserPolicyGeneration > 0 &&
		validDigest(binding.ManifestSHA256)
}

func (id PrerequisiteID) valid() bool {
	return id == PrerequisiteAtomicPolicyGenerations ||
		id == PrerequisiteObservableConnectivityStateMachine
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value)
}
