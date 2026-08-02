package policy

import (
	"errors"
	"testing"
)

func TestSemanticNoOpIgnoresGenerationsAndIdentifiers(t *testing.T) {
	current := composedTestSnapshot(t)
	candidate := composedTestSnapshot(t)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.Root.BundleGeneration = 3
	candidate.User.BundleGeneration = 3
	candidate.Root.PolicyGeneration++
	candidate.User.PolicyGeneration++
	candidate.Root.Rules[0].ID = "root.renamed-rule"
	candidate.Root.Rules[0].Selector.ID = "root.renamed-selector"
	candidate.User.Rules[0].ID = "user.renamed-rule"
	oldSelectorID := candidate.User.Rules[0].Selector.ID
	candidate.User.Rules[0].Selector.ID = "user.renamed-selector"
	candidate.User.Leases[0].ID = "user.renamed-lease"
	for index, selectorID := range candidate.User.Leases[0].SelectorIDs {
		if selectorID == oldSelectorID {
			candidate.User.Leases[0].SelectorIDs[index] = candidate.User.Rules[0].Selector.ID
		}
	}

	noOp, err := IsSemanticNoOp(current, candidate)
	if err != nil || !noOp {
		t.Fatalf("equivalent effective policy should be a no-op: no_op=%v err=%v", noOp, err)
	}
	if !errors.Is(ValidateSemanticAdvance(current, candidate), ErrSemanticNoOp) {
		t.Fatal("semantic no-op must not advance generations")
	}
}

func TestSemanticMetadataAndDomainGenerationAdvance(t *testing.T) {
	current := composedTestSnapshot(t)
	candidate := composedTestSnapshot(t)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.Root.BundleGeneration = 3
	candidate.User.BundleGeneration = 3
	candidate.ExpiresAt = "2026-08-02T11:00:00Z"

	noOp, err := IsSemanticNoOp(current, candidate)
	if err != nil || noOp {
		t.Fatalf("expiry is semantic authorization metadata: no_op=%v err=%v", noOp, err)
	}
	if err := ValidateSemanticAdvance(current, candidate); err != nil {
		t.Fatalf("bundle-only metadata change should preserve domain generations: %v", err)
	}

	candidate.Root.PolicyGeneration++
	if !errors.Is(ValidateSemanticAdvance(current, candidate), ErrInvalidGenerationSemantic) {
		t.Fatal("unchanged domain content must not advance its generation")
	}
}

func TestChangedDomainRequiresGenerationAdvance(t *testing.T) {
	current := composedTestSnapshot(t)
	candidate := composedTestSnapshot(t)
	candidate.BundleGeneration = 3
	candidate.ParentBundleGeneration = 2
	candidate.Root.BundleGeneration = 3
	candidate.User.BundleGeneration = 3
	candidate.User.Rules[0].Effect = EffectDeny

	if !errors.Is(ValidateSemanticAdvance(current, candidate), ErrInvalidGenerationSemantic) {
		t.Fatal("changed domain content must advance its domain generation")
	}
	candidate.User.PolicyGeneration++
	if err := ValidateSemanticAdvance(current, candidate); err != nil {
		t.Fatalf("valid independently advancing user generation: %v", err)
	}
}

func composedTestSnapshot(t *testing.T) EffectiveSnapshot {
	t.Helper()
	envelope := DefaultSafetyEnvelope()
	snapshot, err := ComposeEffectiveSnapshot(validEnvelopeSource(t, envelope), envelope)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
