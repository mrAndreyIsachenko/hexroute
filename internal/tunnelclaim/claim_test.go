package tunnelclaim

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func store(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tunnel-claim.json")
	opened, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return opened, path
}

// No claim means the previous owner still owns the tunnel.
//
// The absence is the default and has to be, because it is the state every
// machine is in until a handover is made — including a machine where this
// system was never installed.
func TestNoClaimMeansTheSupervisorOwnsIt(t *testing.T) {
	opened, _ := store(t)
	claim, held, err := opened.Held()
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if held {
		t.Fatalf("an empty directory reported a claim: %+v", claim)
	}
}

// A claim is placed, read back, and released.
func TestAClaimIsPlacedReadAndReleased(t *testing.T) {
	opened, path := store(t)
	if err := opened.Place("handover-1"); err != nil {
		t.Fatalf("Place: %v", err)
	}
	claim, held, err := opened.Held()
	if err != nil || !held {
		t.Fatalf("Held after Place = %+v, %v, %v", claim, held, err)
	}
	if claim.Holder != HolderHexroute || claim.Transaction != "handover-1" {
		t.Fatalf("the claim says %+v", claim)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the claim is readable by others: %v", info.Mode().Perm())
	}

	if err := opened.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, held, err := opened.Held(); err != nil || held {
		t.Fatalf("Held after Release = %v, %v", held, err)
	}
}

// Releasing what is not held is not an error.
//
// An abort runs when the state is uncertain, which means it must be able to run
// twice and to run against a machine that never got as far as claiming.
func TestReleasingNothingIsNotAnError(t *testing.T) {
	opened, _ := store(t)
	if err := opened.Release(); err != nil {
		t.Fatalf("releasing an unheld tunnel: %v", err)
	}
	if err := opened.Release(); err != nil {
		t.Fatalf("releasing twice: %v", err)
	}
}

// A second transaction does not take the tunnel from the first.
func TestAClaimIsNotOverwritten(t *testing.T) {
	opened, _ := store(t)
	if err := opened.Place("handover-1"); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := opened.Place("handover-2"); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second transaction placed a claim over the first: %v", err)
	}
	claim, _, err := opened.Held()
	if err != nil {
		t.Fatal(err)
	}
	if claim.Transaction != "handover-1" {
		t.Fatalf("the claim now belongs to %q", claim.Transaction)
	}
}

// A file that cannot be read as a claim is an error, never an absence.
//
// They mean opposite things: one is a runtime that may start the tunnel, the
// other is one that must not. Guessing between them is how two owners happen.
func TestAnUnreadableClaimIsNotAnAbsence(t *testing.T) {
	for _, item := range []struct {
		name    string
		content string
	}{
		{"not json", "{"},
		{"another schema", `{"schema":"something.else","holder":"hexroute","transaction":"x","claimed_at":"now"}`},
		{"another holder", `{"schema":"` + Schema + `","holder":"supervisor","transaction":"x","claimed_at":"now"}`},
		{"no transaction", `{"schema":"` + Schema + `","holder":"hexroute","claimed_at":"now"}`},
		{"empty", ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			opened, path := store(t)
			if err := os.WriteFile(path, []byte(item.content), 0o600); err != nil {
				t.Fatal(err)
			}
			claim, held, err := opened.Held()
			if err == nil {
				t.Fatalf("%s was read as a claim: %+v held=%v", item.name, claim, held)
			}
			if held {
				t.Fatalf("%s reported the tunnel held", item.name)
			}
		})
	}
}

// The claim is written whole or not at all.
//
// A reader arriving mid-write would see half a claim, and half a claim is
// unreadable — which stops the previous owner from starting the tunnel for no
// reason at all.
func TestAClaimIsNeverHalfWritten(t *testing.T) {
	opened, path := store(t)
	if err := opened.Place("handover-1"); err != nil {
		t.Fatalf("Place: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".staged" {
			t.Fatalf("a staged claim was left behind: %s", entry.Name())
		}
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var claim Claim
	if err := json.Unmarshal(encoded, &claim); err != nil {
		t.Fatalf("what was written is not a claim: %v", err)
	}
}

// The stamp is the clock's, so a reader can tell a claim made now from one made
// before the machine was last restarted.
func TestTheClaimCarriesWhenItWasMade(t *testing.T) {
	opened, _ := store(t)
	fixed := time.Date(2026, 9, 13, 18, 30, 0, 0, time.UTC)
	opened = opened.WithClock(func() time.Time { return fixed })
	if err := opened.Place("handover-1"); err != nil {
		t.Fatalf("Place: %v", err)
	}
	claim, _, err := opened.Held()
	if err != nil {
		t.Fatal(err)
	}
	if claim.ClaimedAt != "2026-09-13T18:30:00Z" {
		t.Fatalf("claimed_at = %q", claim.ClaimedAt)
	}
}
