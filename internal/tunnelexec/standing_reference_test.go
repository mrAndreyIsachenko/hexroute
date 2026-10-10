package tunnelexec

import (
	"strings"
	"testing"
)

// Every reason has a spelling an event identity can carry, and the list is
// what says "every". A fourth reason added without one is caught here rather
// than at the moment an alert has to name it.
func TestEveryHandbackReasonCanBeSpelledInAnIdentity(t *testing.T) {
	reasons := Handbacks()
	if len(reasons) != 3 {
		t.Fatalf("the vocabulary holds %d reasons; this test and the spelling "+
			"below were written for 3, so one of them is now wrong", len(reasons))
	}
	seen := map[string]Handback{}
	for _, reason := range reasons {
		reference, ok := reason.Reference()
		if !ok {
			t.Fatalf("%q has no spelling for an identity", reason)
		}
		if previous, taken := seen[reference]; taken {
			t.Fatalf("%q and %q both spell as %q", previous, reason, reference)
		}
		seen[reference] = reason
		// The character an event reference will not take is the one these
		// reasons are written with, which is the whole reason this mapping
		// exists.
		if strings.Contains(reference, "_") {
			t.Fatalf("%q spells as %q, which an event identity refuses",
				reason, reference)
		}
		if reference != strings.ToLower(reference) ||
			strings.HasPrefix(reference, "-") ||
			strings.HasSuffix(reference, "-") {
			t.Fatalf("%q spells as %q, which is not identity-shaped",
				reason, reference)
		}
	}
}

// Anything that is not one of them has no spelling. A replacement of
// underscores would have answered for every string it was handed, including a
// reason nobody had decided how to describe.
func TestAReasonOutsideTheVocabularyHasNoSpelling(t *testing.T) {
	for _, outside := range []Handback{"", "rate_bound ", "carrier_changed", "RATE_BOUND"} {
		if reference, ok := outside.Reference(); ok {
			t.Fatalf("%q was spelled %q and is not a reason", outside, reference)
		}
		if outside.Valid() {
			t.Fatalf("%q reported itself valid", outside)
		}
	}
}

// The notice reads the same list, so a reason the vocabulary admits is a reason
// the word on disk admits, without a second switch to keep in step.
func TestTheNoticeAcceptsExactlyTheVocabulary(t *testing.T) {
	for _, reason := range Handbacks() {
		if !Handback(reason).Valid() {
			t.Fatalf("%q is in the vocabulary and not valid", reason)
		}
	}
}
