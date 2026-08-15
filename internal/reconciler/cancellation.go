package reconciler

import (
	"errors"
	"sort"
	"sync"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type AttemptStateJournal interface {
	Latest(metadata.UUID) (AttemptJournalEntry, bool, error)
	CompareAndSwap(AttemptBinding, AttemptState, AttemptState, Reason) (AttemptJournalEntry, error)
}

type CancellationIntentRecord struct {
	CancelID      metadata.UUID  `json:"cancel_id"`
	ActionID      metadata.UUID  `json:"action_id"`
	AttemptID     metadata.UUID  `json:"attempt_id"`
	Binding       AttemptBinding `json:"binding"`
	ObservedState AttemptState   `json:"observed_state"`
	Reason        Reason         `json:"reason"`
	IntentSHA256  string         `json:"intent_sha256"`
}

type cancellationIntentDigestInput struct {
	CancelID      metadata.UUID  `json:"cancel_id"`
	ActionID      metadata.UUID  `json:"action_id"`
	AttemptID     metadata.UUID  `json:"attempt_id"`
	Binding       AttemptBinding `json:"binding"`
	ObservedState AttemptState   `json:"observed_state"`
	Reason        Reason         `json:"reason"`
}

type MemoryCancellationIntentStore struct {
	mu      sync.Mutex
	intents map[metadata.UUID]CancellationIntentRecord
}

type MemorySyntheticResourceRegistry struct {
	mu        sync.Mutex
	resources map[string]ResourceRecord
}

type CleanupResult struct {
	Closed      []string        `json:"closed"`
	Failed      []string        `json:"failed"`
	Incident    *IncidentRecord `json:"incident,omitempty"`
	Uncertainty bool            `json:"uncertainty"`
}

type CompensationResult struct {
	Compensated []string        `json:"compensated"`
	Outcome     AttemptState    `json:"outcome"`
	Reason      Reason          `json:"reason"`
	Incident    *IncidentRecord `json:"incident,omitempty"`
}

type CancellationResolution struct {
	Intent       CancellationIntentRecord `json:"intent"`
	Attempt      AttemptJournalEntry      `json:"attempt"`
	Compensation CompensationResult       `json:"compensation"`
	Cleanup      CleanupResult            `json:"cleanup"`
	Outcome      OutcomeRecord            `json:"outcome"`
}

var (
	ErrCancellationIntent   = errors.New("invalid cancellation intent")
	ErrCancellationRejected = errors.New("cancellation rejected")
	ErrCancellationBlocked  = errors.New("cancellation blocks next step")
	ErrCleanupRegistry      = errors.New("invalid cleanup registry")
)

func NewMemoryCancellationIntentStore() *MemoryCancellationIntentStore {
	return &MemoryCancellationIntentStore{intents: make(map[metadata.UUID]CancellationIntentRecord)}
}

func NewMemorySyntheticResourceRegistry() *MemorySyntheticResourceRegistry {
	return &MemorySyntheticResourceRegistry{resources: make(map[string]ResourceRecord)}
}

func RequestCancellation(
	journal AttemptStateJournal,
	store *MemoryCancellationIntentStore,
	actionID metadata.UUID,
	cancelID metadata.UUID,
) (CancellationIntentRecord, error) {
	if journal == nil || store == nil || !validUUID(actionID) || !validUUID(cancelID) {
		return CancellationIntentRecord{}, ErrCancellationIntent
	}
	entry, exists, err := journal.Latest(actionID)
	if err != nil {
		return CancellationIntentRecord{}, err
	}
	if !exists || !cancellableAttemptState(entry.Attempt.State) {
		return CancellationIntentRecord{}, ErrCancellationRejected
	}
	intent, err := newCancellationIntent(entry, cancelID)
	if err != nil {
		return CancellationIntentRecord{}, err
	}
	if err := store.persist(intent); err != nil {
		return CancellationIntentRecord{}, err
	}
	return intent, nil
}

func CanStartNextSyntheticStep(store *MemoryCancellationIntentStore, binding AttemptBinding) error {
	if store == nil || binding.validate() != nil {
		return ErrCancellationIntent
	}
	if _, exists, err := store.Lookup(binding.ActionID); err != nil {
		return err
	} else if exists {
		return ErrCancellationBlocked
	}
	return nil
}

func ResolveCancellation(
	journal AttemptStateJournal,
	store *MemoryCancellationIntentStore,
	registry *MemorySyntheticResourceRegistry,
	adapter SyntheticAdapter,
	binding AttemptBinding,
	applied []SyntheticPlanStep,
) (CancellationResolution, error) {
	if journal == nil || store == nil || registry == nil || binding.validate() != nil ||
		len(applied) > MaxPlanSteps {
		return CancellationResolution{}, ErrCancellationIntent
	}
	intent, exists, err := store.Lookup(binding.ActionID)
	if err != nil {
		return CancellationResolution{}, err
	}
	if !exists {
		return CancellationResolution{}, ErrCancellationRejected
	}
	latest, exists, err := journal.Latest(binding.ActionID)
	if err != nil {
		return CancellationResolution{}, err
	}
	if !exists || latest.Binding != binding || !cancellableAttemptState(latest.Attempt.State) {
		return CancellationResolution{}, ErrCancellationRejected
	}

	compensation := CompensationResult{Outcome: AttemptCancelled, Reason: ReasonCancelled}
	target := AttemptCancelled
	if len(applied) > 0 {
		compensation, err = CompensateSyntheticAppliedPrefix(adapter, binding, applied)
		if err != nil {
			return CancellationResolution{}, err
		}
		target = compensation.Outcome
	}
	cleanup := registry.CloseAll(binding)
	if cleanup.Uncertainty && target != AttemptSafeMode {
		target = AttemptSafeMode
		compensation.Outcome = AttemptSafeMode
		compensation.Reason = ReasonCleanup
		if compensation.Incident == nil {
			compensation.Incident = cleanup.Incident
		}
	}
	next, err := journal.CompareAndSwap(binding, latest.Attempt.State, target, compensation.Reason)
	if err != nil {
		return CancellationResolution{}, err
	}
	outcome := BuildCancellationOutcome(binding, target, compensation.Reason)
	return CancellationResolution{
		Intent: intent, Attempt: next, Compensation: compensation,
		Cleanup: cleanup, Outcome: outcome,
	}, nil
}

func CompensateSyntheticAppliedPrefix(
	adapter SyntheticAdapter,
	binding AttemptBinding,
	applied []SyntheticPlanStep,
) (CompensationResult, error) {
	if adapter == nil || binding.validate() != nil || len(applied) == 0 || len(applied) > MaxPlanSteps {
		return CompensationResult{}, ErrSyntheticAdapter
	}
	result := CompensationResult{Outcome: AttemptRolledBack, Reason: ReasonCompensation}
	for index := len(applied) - 1; index >= 0; index-- {
		step := applied[index]
		if step.validate() != nil || step.Owner != binding {
			return safeModeCompensation(binding, ReasonOwnership), nil
		}
		state, err := adapter.Observe()
		if err != nil {
			return safeModeCompensation(binding, ReasonCompensation), nil
		}
		if !syntheticStepIsExactlyOwned(state, step) {
			return safeModeCompensation(binding, ReasonSafeMode), nil
		}
		if _, err := adapter.Compensate(step); err != nil {
			return safeModeCompensation(binding, ReasonCompensation), nil
		}
		result.Compensated = append(result.Compensated, step.ID)
	}
	return result, nil
}

func (registry *MemorySyntheticResourceRegistry) Register(
	binding AttemptBinding,
	kind ResourceKind,
	resourceID string,
) (ResourceRecord, error) {
	if registry == nil || binding.validate() != nil ||
		!kind.Valid() || !validIdentifier(resourceID) {
		return ResourceRecord{}, ErrCleanupRegistry
	}
	record := ResourceRecord{
		ResourceID:  resourceID,
		Kind:        kind,
		State:       ResourceRegistered,
		OwnerSHA256: AttemptBindingSHA256(binding),
	}
	if record.Validate() != nil {
		return ResourceRecord{}, ErrCleanupRegistry
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing, exists := registry.resources[resourceID]; exists && existing.State != ResourceClosed {
		return ResourceRecord{}, ErrCleanupRegistry
	}
	registry.resources[resourceID] = record
	return record, nil
}

func (registry *MemorySyntheticResourceRegistry) MarkFailed(resourceID string) error {
	if registry == nil || !validIdentifier(resourceID) {
		return ErrCleanupRegistry
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record, exists := registry.resources[resourceID]
	if !exists {
		return ErrCleanupRegistry
	}
	record.State = ResourceFailed
	registry.resources[resourceID] = record
	return nil
}

func (registry *MemorySyntheticResourceRegistry) CloseAll(binding AttemptBinding) CleanupResult {
	result := CleanupResult{}
	if registry == nil || binding.validate() != nil {
		return cleanupUncertain(binding, ReasonCleanup)
	}
	ownerSHA256 := AttemptBindingSHA256(binding)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	keys := make([]string, 0, len(registry.resources))
	for key := range registry.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		record := registry.resources[key]
		if record.OwnerSHA256 != ownerSHA256 || record.State == ResourceFailed {
			result.Failed = append(result.Failed, record.ResourceID)
			result.Uncertainty = true
			continue
		}
		record.State = ResourceClosed
		registry.resources[key] = record
		result.Closed = append(result.Closed, record.ResourceID)
	}
	if result.Uncertainty {
		incident := cleanupIncident(binding, ReasonCleanup)
		result.Incident = &incident
	}
	return result
}

func (registry *MemorySyntheticResourceRegistry) Snapshot() []ResourceRecord {
	if registry == nil {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	keys := make([]string, 0, len(registry.resources))
	for key := range registry.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ResourceRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, registry.resources[key])
	}
	return out
}

func BuildCancellationOutcome(binding AttemptBinding, state AttemptState, reason Reason) OutcomeRecord {
	outcome := OutcomeCancelled
	switch state {
	case AttemptCancelled:
		outcome = OutcomeCancelled
	case AttemptRolledBack:
		outcome = OutcomeRolledBack
	case AttemptSafeMode:
		outcome = OutcomeSafeMode
	case AttemptFailed:
		outcome = OutcomeFailed
	}
	return OutcomeRecord{
		ActionID:       binding.ActionID,
		AttemptID:      binding.AttemptID,
		Outcome:        outcome,
		Reason:         reason,
		ReportDelivery: ReportPending,
	}
}

func AttemptBindingSHA256(binding AttemptBinding) string {
	encoded, err := policy.MarshalCanonical(binding)
	if err != nil {
		return ""
	}
	return policy.SHA256Hex(encoded)
}

func (store *MemoryCancellationIntentStore) Lookup(actionID metadata.UUID) (CancellationIntentRecord, bool, error) {
	if store == nil || !validUUID(actionID) {
		return CancellationIntentRecord{}, false, ErrCancellationIntent
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	intent, exists := store.intents[actionID]
	if exists && intent.Validate() != nil {
		return CancellationIntentRecord{}, false, ErrCancellationIntent
	}
	return intent, exists, nil
}

func (store *MemoryCancellationIntentStore) persist(intent CancellationIntentRecord) error {
	if intent.Validate() != nil {
		return ErrCancellationIntent
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, exists := store.intents[intent.ActionID]; exists && existing != intent {
		return ErrCancellationRejected
	}
	store.intents[intent.ActionID] = intent
	return nil
}

func newCancellationIntent(entry AttemptJournalEntry, cancelID metadata.UUID) (CancellationIntentRecord, error) {
	intent := CancellationIntentRecord{
		CancelID:      cancelID,
		ActionID:      entry.Binding.ActionID,
		AttemptID:     entry.Binding.AttemptID,
		Binding:       entry.Binding,
		ObservedState: entry.Attempt.State,
		Reason:        ReasonCancelled,
	}
	digest, err := cancellationIntentDigest(intent)
	if err != nil {
		return CancellationIntentRecord{}, err
	}
	intent.IntentSHA256 = digest
	if intent.Validate() != nil {
		return CancellationIntentRecord{}, ErrCancellationIntent
	}
	return intent, nil
}

func cancellationIntentDigest(intent CancellationIntentRecord) (string, error) {
	if !validUUID(intent.CancelID) ||
		!validUUID(intent.ActionID) ||
		!validUUID(intent.AttemptID) ||
		intent.Binding.validate() != nil ||
		!intent.ObservedState.Valid() ||
		!intent.Reason.Valid() ||
		intent.ActionID != intent.Binding.ActionID ||
		intent.AttemptID != intent.Binding.AttemptID {
		return "", ErrCancellationIntent
	}
	encoded, err := policy.MarshalCanonical(cancellationIntentDigestInput{
		CancelID:      intent.CancelID,
		ActionID:      intent.ActionID,
		AttemptID:     intent.AttemptID,
		Binding:       intent.Binding,
		ObservedState: intent.ObservedState,
		Reason:        intent.Reason,
	})
	if err != nil {
		return "", ErrCancellationIntent
	}
	return policy.SHA256Hex(encoded), nil
}

func (intent CancellationIntentRecord) Validate() error {
	if !validDigest(intent.IntentSHA256) {
		return ErrCancellationIntent
	}
	digest, err := cancellationIntentDigest(intent)
	if err != nil || digest != intent.IntentSHA256 {
		return ErrCancellationIntent
	}
	return nil
}

func syntheticStepIsExactlyOwned(state SyntheticState, step SyntheticPlanStep) bool {
	for _, resource := range state.Resources {
		if resource.ID == step.ResourceID &&
			resource.Operation == step.Operation &&
			resource.StateSHA256 == step.AppliedSHA256 &&
			resource.Ownership == SyntheticOwned &&
			resource.OwnerActionID == step.Owner.ActionID &&
			resource.OwnerAttemptID == step.Owner.AttemptID &&
			!resource.Protected {
			return true
		}
	}
	return false
}

func cancellableAttemptState(state AttemptState) bool {
	return state == AttemptPending ||
		state == AttemptClaimed ||
		state == AttemptRunning ||
		state == AttemptVerifying
}

func safeModeCompensation(binding AttemptBinding, reason Reason) CompensationResult {
	incident := cleanupIncident(binding, reason)
	return CompensationResult{
		Outcome:  AttemptSafeMode,
		Reason:   reason,
		Incident: &incident,
	}
}

func cleanupUncertain(binding AttemptBinding, reason Reason) CleanupResult {
	incident := cleanupIncident(binding, reason)
	return CleanupResult{
		Incident:    &incident,
		Uncertainty: true,
	}
}

func cleanupIncident(binding AttemptBinding, reason Reason) IncidentRecord {
	return IncidentRecord{
		IncidentID: binding.ActionID,
		Severity:   SeverityCritical,
		Reason:     reason,
		Target:     binding.Target,
	}
}
