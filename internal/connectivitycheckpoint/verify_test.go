package connectivitycheckpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type verifyClock struct{ step int64 }

func (clock *verifyClock) WallNow() time.Time {
	clock.step++
	return time.Unix(1_800_000_000+clock.step, 0).UTC()
}

func (clock *verifyClock) MonotonicNow() time.Duration {
	clock.step++
	return time.Duration(clock.step) * time.Millisecond
}

// verifiable builds a store and the journal that fed it, so a verification has
// both halves of what a real host would have kept.
func verifiable(
	t *testing.T,
	links int,
) (*Store, *connectivityjournal.Journal, *connectivityjournal.Journal) {
	t.Helper()
	store, _ := openStore(t, Options{})
	base := t.TempDir()
	root := openJournal(t, filepath.Join(base, "root"), policy.DomainRoot,
		"33333333-3333-4333-8333-333333333333")
	user := openJournal(t, filepath.Join(base, "user"), policy.DomainUser,
		"44444444-4444-4444-8444-444444444444")
	chain := newLineage(t)
	for index := 0; index < links; index++ {
		before := chain.consumed
		checkpoint := chain.next()
		if err := store.Append(checkpoint); err != nil {
			t.Fatalf("append: %v", err)
		}
		for _, record := range journalled(t, chain, before+1) {
			// Each fact goes to the journal its own domain owns, exactly as
			// the runtime writes them.
			target := root
			if record.Fact.Domain == policy.DomainUser {
				target = user
			}
			if err := target.Append(
				record.Fact, record.HostSequence, record.Role); err != nil {
				t.Fatalf("journal append: %v", err)
			}
		}
	}
	return store, root, user
}

func openJournal(
	t *testing.T,
	path string,
	domain policy.Domain,
	node string,
) *connectivityjournal.Journal {
	t.Helper()
	journal, err := connectivityjournal.Open(path, domain,
		connectivityjournal.Options{
			NodeID: metadata.UUID(node), Clock: &verifyClock{},
		})
	if err != nil {
		t.Fatalf("journal %s: %v", domain, err)
	}
	return journal
}

// A lineage nobody re-derived is well-formed, not true. This asks the retained
// facts whether they still produce what was written.
func TestVerifyReproducesAnUntouchedLineage(t *testing.T) {
	store, root, user := verifiable(t, 3)
	result, err := Verify(store, root, user, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !result.Sound() {
		t.Fatalf("an untouched lineage diverged: %+v", result)
	}
	if result.Reproduced == 0 {
		t.Fatalf("nothing was reproduced: %+v", result)
	}
}

// The finding the gate exists to refuse: a published conclusion the evidence
// no longer supports. Without this the verification would confirm whatever was
// written.
func TestVerifyCatchesARewrittenOutputDigest(t *testing.T) {
	store, root, user := verifiable(t, 3)
	index, err := store.Index()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	// Rewrite the recorded snapshot digest of a link that has a parent.
	target := index[len(index)-1]
	path := filepath.Join(store.root, checkpointDir, target.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tampered := strings.Replace(string(raw),
		`"snapshot_digest":"`, `"snapshot_digest":"f`, 1)
	if tampered == string(raw) {
		t.Fatal("the fixture no longer contains a snapshot digest to rewrite")
	}
	if err := os.WriteFile(path, []byte(tampered[:len(tampered)-1]+"}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := Verify(store, root, user, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Sound() && result.Unreplayable == 0 {
		t.Fatal("a rewritten output digest was reported as sound")
	}
}

// A journal that no longer holds a link's range leaves it unverified. That is
// a different answer from a link that disagreed, and conflating them would
// turn retention into a divergence.
func TestVerifyReportsAnUnreplayableLinkSeparately(t *testing.T) {
	store, _, _ := verifiable(t, 2)
	base := t.TempDir()
	emptyRoot := openJournal(t, filepath.Join(base, "root"), policy.DomainRoot,
		"55555555-5555-4555-8555-555555555555")
	emptyUser := openJournal(t, filepath.Join(base, "user"), policy.DomainUser,
		"66666666-6666-4666-8666-666666666666")
	result, err := Verify(store, emptyRoot, emptyUser, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Diverged != 0 {
		t.Fatalf("a missing journal was counted as divergence: %+v", result)
	}
	if result.Unreplayable == 0 {
		t.Fatalf("a missing journal left nothing unverified: %+v", result)
	}
}

// Genesis has no parent to replay from, and saying so is not a failure.
func TestVerifyNamesGenesisRatherThanFailingIt(t *testing.T) {
	store, root, user := verifiable(t, 1)
	result, err := Verify(store, root, user, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(result.Links) != 1 || result.Links[0].Status != VerifyGenesis {
		t.Fatalf("links = %+v", result.Links)
	}
	if !result.Sound() {
		t.Fatal("a lineage of one link was reported unsound")
	}
	_ = connectivity.Components()
}
