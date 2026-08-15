package reconciler

import (
	"errors"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type SyntheticOwnership string

const (
	SyntheticOwned     SyntheticOwnership = "owned"
	SyntheticForeign   SyntheticOwnership = "foreign"
	SyntheticAmbiguous SyntheticOwnership = "ambiguous"
)

type SyntheticFaultPoint string

const (
	SyntheticFaultObserve    SyntheticFaultPoint = "observe"
	SyntheticFaultCompare    SyntheticFaultPoint = "compare"
	SyntheticFaultApply      SyntheticFaultPoint = "apply"
	SyntheticFaultVerify     SyntheticFaultPoint = "verify"
	SyntheticFaultCompensate SyntheticFaultPoint = "compensate"
	SyntheticFaultCleanup    SyntheticFaultPoint = "cleanup"
)

type SyntheticResource struct {
	ID             string             `json:"id"`
	Operation      OperationClass     `json:"operation"`
	StateSHA256    string             `json:"state_sha256"`
	Ownership      SyntheticOwnership `json:"ownership"`
	OwnerActionID  metadata.UUID      `json:"owner_action_id,omitempty"`
	OwnerAttemptID metadata.UUID      `json:"owner_attempt_id,omitempty"`
	Protected      bool               `json:"protected,omitempty"`
}

type SyntheticState struct {
	Resources []SyntheticResource `json:"resources"`
}

type SyntheticDesiredResource struct {
	ID          string         `json:"id"`
	Operation   OperationClass `json:"operation"`
	InputSHA256 string         `json:"input_sha256"`
	StateSHA256 string         `json:"state_sha256"`
}

type SyntheticDesiredState struct {
	Fresh      bool                       `json:"fresh"`
	Authorized bool                       `json:"authorized"`
	Owner      AttemptBinding             `json:"owner"`
	Resources  []SyntheticDesiredResource `json:"resources"`
}

type SyntheticPlanStep struct {
	ID                 string         `json:"id"`
	ResourceID         string         `json:"resource_id"`
	Operation          OperationClass `json:"operation"`
	Owner              AttemptBinding `json:"owner"`
	InputSHA256        string         `json:"input_sha256"`
	BeforeSHA256       string         `json:"before_sha256"`
	AppliedSHA256      string         `json:"applied_sha256"`
	VerificationSHA256 string         `json:"verification_sha256"`
	CompensationSHA256 string         `json:"compensation_sha256"`
}

type SyntheticConflict struct {
	ResourceID string         `json:"resource_id"`
	Operation  OperationClass `json:"operation"`
	Reason     Reason         `json:"reason"`
}

type SyntheticDiff struct {
	Steps     []SyntheticPlanStep `json:"steps"`
	Conflicts []SyntheticConflict `json:"conflicts"`
	Noop      bool                `json:"noop"`
}

type SyntheticAdapter interface {
	Observe() (SyntheticState, error)
	SemanticCompare(SyntheticDesiredState) (SyntheticDiff, error)
	Apply(SyntheticPlanStep) (SyntheticState, error)
	Verify(SyntheticPlanStep) error
	Compensate(SyntheticPlanStep) (SyntheticState, error)
	Cleanup(AttemptBinding) error
}

type MemorySyntheticAdapter struct {
	state map[string]SyntheticResource
}

type CrashFixtureSyntheticAdapter struct {
	inner  *MemorySyntheticAdapter
	faults map[SyntheticFaultPoint]bool
}

var (
	ErrSyntheticAdapter      = errors.New("invalid synthetic adapter")
	ErrSyntheticConflict     = errors.New("synthetic state conflict")
	ErrSyntheticVerification = errors.New("synthetic verification failed")
	ErrSyntheticFault        = errors.New("synthetic crash fixture fault")
)

func NewMemorySyntheticAdapter(resources []SyntheticResource) (*MemorySyntheticAdapter, error) {
	state, err := syntheticResourceMap(resources)
	if err != nil {
		return nil, err
	}
	return &MemorySyntheticAdapter{state: state}, nil
}

func NewCrashFixtureSyntheticAdapter(
	resources []SyntheticResource,
	faults []SyntheticFaultPoint,
) (*CrashFixtureSyntheticAdapter, error) {
	inner, err := NewMemorySyntheticAdapter(resources)
	if err != nil {
		return nil, err
	}
	owned := make(map[SyntheticFaultPoint]bool, len(faults))
	for _, fault := range faults {
		if !fault.Valid() {
			return nil, ErrSyntheticAdapter
		}
		owned[fault] = true
	}
	return &CrashFixtureSyntheticAdapter{inner: inner, faults: owned}, nil
}

func DiffSyntheticState(current SyntheticState, desired SyntheticDesiredState) (SyntheticDiff, error) {
	currentByKey, err := syntheticResourceMap(current.Resources)
	if err != nil {
		return SyntheticDiff{}, err
	}
	if desired.validate() != nil {
		return SyntheticDiff{}, ErrSyntheticAdapter
	}
	desiredByKey := make(map[string]SyntheticDesiredResource, len(desired.Resources))
	for _, resource := range desired.Resources {
		key := syntheticResourceKey(resource.ID, resource.Operation)
		if _, exists := desiredByKey[key]; exists {
			return SyntheticDiff{}, ErrSyntheticAdapter
		}
		desiredByKey[key] = resource
	}
	var diff SyntheticDiff
	keys := make([]string, 0, len(desiredByKey))
	for key := range desiredByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		want := desiredByKey[key]
		have, exists := currentByKey[key]
		if !exists {
			diff.Steps = append(diff.Steps, newSyntheticPlanStep(index, want, desired.Owner, syntheticMissingStateSHA256()))
			continue
		}
		if conflict, blocked := classifySyntheticResource(have, desired.Owner); blocked {
			diff.Conflicts = append(diff.Conflicts, SyntheticConflict{
				ResourceID: want.ID, Operation: want.Operation, Reason: conflict,
			})
			continue
		}
		if have.StateSHA256 == want.StateSHA256 {
			continue
		}
		diff.Steps = append(diff.Steps, newSyntheticPlanStep(index, want, desired.Owner, have.StateSHA256))
	}
	for key, have := range currentByKey {
		if _, exists := desiredByKey[key]; exists {
			continue
		}
		reason, _ := classifySyntheticResource(have, desired.Owner)
		if reason == ReasonAccepted {
			reason = ReasonLineage
		}
		diff.Conflicts = append(diff.Conflicts, SyntheticConflict{
			ResourceID: have.ID, Operation: have.Operation, Reason: reason,
		})
	}
	diff.Noop = len(diff.Steps) == 0 && len(diff.Conflicts) == 0
	if err := diff.validate(); err != nil {
		return SyntheticDiff{}, err
	}
	return diff, nil
}

func (adapter *MemorySyntheticAdapter) Observe() (SyntheticState, error) {
	if adapter == nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	return SyntheticState{Resources: syntheticResources(adapter.state)}, nil
}

func (adapter *MemorySyntheticAdapter) SemanticCompare(desired SyntheticDesiredState) (SyntheticDiff, error) {
	current, err := adapter.Observe()
	if err != nil {
		return SyntheticDiff{}, err
	}
	return DiffSyntheticState(current, desired)
}

func (adapter *MemorySyntheticAdapter) Apply(step SyntheticPlanStep) (SyntheticState, error) {
	if adapter == nil || step.validate() != nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	adapter.state[syntheticResourceKey(step.ResourceID, step.Operation)] = SyntheticResource{
		ID:             step.ResourceID,
		Operation:      step.Operation,
		StateSHA256:    step.AppliedSHA256,
		Ownership:      SyntheticOwned,
		OwnerActionID:  step.Owner.ActionID,
		OwnerAttemptID: step.Owner.AttemptID,
	}
	return adapter.Observe()
}

func (adapter *MemorySyntheticAdapter) Verify(step SyntheticPlanStep) error {
	if adapter == nil || step.validate() != nil {
		return ErrSyntheticAdapter
	}
	resource, exists := adapter.state[syntheticResourceKey(step.ResourceID, step.Operation)]
	if !exists || resource.StateSHA256 != step.AppliedSHA256 ||
		resource.Ownership != SyntheticOwned ||
		resource.OwnerActionID != step.Owner.ActionID ||
		resource.OwnerAttemptID != step.Owner.AttemptID {
		return ErrSyntheticVerification
	}
	return nil
}

func (adapter *MemorySyntheticAdapter) Compensate(step SyntheticPlanStep) (SyntheticState, error) {
	if adapter == nil || step.validate() != nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	key := syntheticResourceKey(step.ResourceID, step.Operation)
	if step.BeforeSHA256 == syntheticMissingStateSHA256() {
		delete(adapter.state, key)
		return adapter.Observe()
	}
	adapter.state[key] = SyntheticResource{
		ID:             step.ResourceID,
		Operation:      step.Operation,
		StateSHA256:    step.BeforeSHA256,
		Ownership:      SyntheticOwned,
		OwnerActionID:  step.Owner.ActionID,
		OwnerAttemptID: step.Owner.AttemptID,
	}
	return adapter.Observe()
}

func (adapter *MemorySyntheticAdapter) Cleanup(owner AttemptBinding) error {
	if adapter == nil || owner.validate() != nil {
		return ErrSyntheticAdapter
	}
	return nil
}

func (adapter *CrashFixtureSyntheticAdapter) Observe() (SyntheticState, error) {
	if adapter == nil || adapter.inner == nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultObserve] {
		return SyntheticState{}, ErrSyntheticFault
	}
	return adapter.inner.Observe()
}

func (adapter *CrashFixtureSyntheticAdapter) SemanticCompare(desired SyntheticDesiredState) (SyntheticDiff, error) {
	if adapter == nil || adapter.inner == nil {
		return SyntheticDiff{}, ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultCompare] {
		return SyntheticDiff{}, ErrSyntheticFault
	}
	return adapter.inner.SemanticCompare(desired)
}

func (adapter *CrashFixtureSyntheticAdapter) Apply(step SyntheticPlanStep) (SyntheticState, error) {
	if adapter == nil || adapter.inner == nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultApply] {
		return SyntheticState{}, ErrSyntheticFault
	}
	return adapter.inner.Apply(step)
}

func (adapter *CrashFixtureSyntheticAdapter) Verify(step SyntheticPlanStep) error {
	if adapter == nil || adapter.inner == nil {
		return ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultVerify] {
		return ErrSyntheticFault
	}
	return adapter.inner.Verify(step)
}

func (adapter *CrashFixtureSyntheticAdapter) Compensate(step SyntheticPlanStep) (SyntheticState, error) {
	if adapter == nil || adapter.inner == nil {
		return SyntheticState{}, ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultCompensate] {
		return SyntheticState{}, ErrSyntheticFault
	}
	return adapter.inner.Compensate(step)
}

func (adapter *CrashFixtureSyntheticAdapter) Cleanup(owner AttemptBinding) error {
	if adapter == nil || adapter.inner == nil {
		return ErrSyntheticAdapter
	}
	if adapter.faults[SyntheticFaultCleanup] {
		return ErrSyntheticFault
	}
	return adapter.inner.Cleanup(owner)
}

func (step SyntheticPlanStep) TranslationStep() TranslationStep {
	return TranslationStep{
		ID:                 step.ID,
		Operation:          step.Operation,
		InputSHA256:        step.InputSHA256,
		BeforeSHA256:       step.BeforeSHA256,
		AppliedSHA256:      step.AppliedSHA256,
		VerificationSHA256: step.VerificationSHA256,
		CompensationSHA256: step.CompensationSHA256,
	}
}

func newSyntheticPlanStep(
	index int,
	resource SyntheticDesiredResource,
	owner AttemptBinding,
	beforeSHA256 string,
) SyntheticPlanStep {
	return SyntheticPlanStep{
		ID:                 "synthetic.step" + string(rune('a'+index)),
		ResourceID:         resource.ID,
		Operation:          resource.Operation,
		Owner:              owner,
		InputSHA256:        resource.InputSHA256,
		BeforeSHA256:       beforeSHA256,
		AppliedSHA256:      resource.StateSHA256,
		VerificationSHA256: policy.SHA256Hex([]byte("verify:" + resource.ID + ":" + string(resource.Operation) + ":" + resource.StateSHA256)),
		CompensationSHA256: policy.SHA256Hex([]byte("compensate:" + resource.ID + ":" + beforeSHA256)),
	}
}

func classifySyntheticResource(resource SyntheticResource, owner AttemptBinding) (Reason, bool) {
	if resource.Protected {
		return ReasonPolicy, true
	}
	switch resource.Ownership {
	case SyntheticForeign:
		return ReasonOwnership, true
	case SyntheticAmbiguous:
		return ReasonOwnership, true
	case SyntheticOwned:
		if resource.OwnerActionID != owner.ActionID || resource.OwnerAttemptID != owner.AttemptID {
			return ReasonOwnership, true
		}
		return ReasonAccepted, false
	default:
		return ReasonSchema, true
	}
}

func syntheticResourceMap(resources []SyntheticResource) (map[string]SyntheticResource, error) {
	if len(resources) > MaxResources {
		return nil, ErrSyntheticAdapter
	}
	out := make(map[string]SyntheticResource, len(resources))
	for _, resource := range resources {
		if resource.validate() != nil {
			return nil, ErrSyntheticAdapter
		}
		key := syntheticResourceKey(resource.ID, resource.Operation)
		if _, exists := out[key]; exists {
			return nil, ErrSyntheticAdapter
		}
		out[key] = resource
	}
	return out, nil
}

func syntheticResources(resources map[string]SyntheticResource) []SyntheticResource {
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]SyntheticResource, 0, len(keys))
	for _, key := range keys {
		out = append(out, resources[key])
	}
	return out
}

func syntheticResourceKey(id string, operation OperationClass) string {
	return id + "\x00" + string(operation)
}

func syntheticMissingStateSHA256() string {
	return policy.SHA256Hex([]byte("hexroute.synthetic.missing-state.v1"))
}

func (desired SyntheticDesiredState) validate() error {
	if !desired.Fresh || !desired.Authorized || desired.Owner.validate() != nil ||
		len(desired.Resources) > MaxResources {
		return ErrSyntheticAdapter
	}
	for _, resource := range desired.Resources {
		if resource.validate() != nil {
			return ErrSyntheticAdapter
		}
	}
	return nil
}

func (resource SyntheticDesiredResource) validate() error {
	if !validIdentifier(resource.ID) ||
		!resource.Operation.valid() ||
		!validDigest(resource.InputSHA256) ||
		!validDigest(resource.StateSHA256) {
		return ErrSyntheticAdapter
	}
	return nil
}

func (resource SyntheticResource) validate() error {
	if !validIdentifier(resource.ID) ||
		!resource.Operation.valid() ||
		!validDigest(resource.StateSHA256) ||
		!resource.Ownership.Valid() {
		return ErrSyntheticAdapter
	}
	if resource.Ownership == SyntheticOwned {
		if !validUUID(resource.OwnerActionID) ||
			!validUUID(resource.OwnerAttemptID) ||
			resource.OwnerActionID == resource.OwnerAttemptID {
			return ErrSyntheticAdapter
		}
	}
	return nil
}

func (step SyntheticPlanStep) validate() error {
	if !validIdentifier(step.ID) ||
		!validIdentifier(step.ResourceID) ||
		!step.Operation.valid() ||
		step.Owner.validate() != nil ||
		!validDigest(step.InputSHA256) ||
		!validDigest(step.BeforeSHA256) ||
		!validDigest(step.AppliedSHA256) ||
		!validDigest(step.VerificationSHA256) ||
		!validDigest(step.CompensationSHA256) {
		return ErrSyntheticAdapter
	}
	return nil
}

func (diff SyntheticDiff) validate() error {
	if len(diff.Steps) > MaxPlanSteps || len(diff.Conflicts) > MaxResources {
		return ErrSyntheticAdapter
	}
	seen := make(map[string]struct{}, len(diff.Steps))
	for _, step := range diff.Steps {
		if step.validate() != nil {
			return ErrSyntheticAdapter
		}
		key := syntheticResourceKey(step.ResourceID, step.Operation)
		if _, exists := seen[key]; exists {
			return ErrSyntheticAdapter
		}
		seen[key] = struct{}{}
	}
	for _, conflict := range diff.Conflicts {
		if !validIdentifier(conflict.ResourceID) ||
			!conflict.Operation.valid() ||
			!conflict.Reason.Valid() {
			return ErrSyntheticAdapter
		}
	}
	return nil
}

func (ownership SyntheticOwnership) Valid() bool {
	return ownership == SyntheticOwned ||
		ownership == SyntheticForeign ||
		ownership == SyntheticAmbiguous
}

func (fault SyntheticFaultPoint) Valid() bool {
	return fault == SyntheticFaultObserve ||
		fault == SyntheticFaultCompare ||
		fault == SyntheticFaultApply ||
		fault == SyntheticFaultVerify ||
		fault == SyntheticFaultCompensate ||
		fault == SyntheticFaultCleanup
}
