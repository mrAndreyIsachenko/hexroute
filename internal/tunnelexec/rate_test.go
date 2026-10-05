package tunnelexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rateFor(t *testing.T, at *time.Time) Rate {
	t.Helper()
	return Rate{
		Path: filepath.Join(t.TempDir(), "rebuilds.json"),
		Now:  func() time.Time { return *at },
	}
}

func allows(t *testing.T, rate Rate) bool {
	t.Helper()
	allowed, err := rate.Allows()
	if err != nil {
		t.Fatalf("Allows: %v", err)
	}
	return allowed
}

// Each bound holds on its own, and the shortest one is reached first.
func TestEachBoundStopsTheRebuilds(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rate := rateFor(t, &at)
	if !allows(t, rate) {
		t.Fatal("a store with nothing in it refused the first rebuild")
	}
	for index := 0; index < 2; index++ {
		if err := rate.Record(at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Minute)
	}
	if allows(t, rate) {
		t.Fatal("two rebuilds in five minutes did not reach the bound")
	}
	// Past the five minutes, the hour still allows more.
	at = at.Add(6 * time.Minute)
	if !allows(t, rate) {
		t.Fatal("the five-minute bound outlived its window")
	}
	// Six in the hour reaches the hour's bound.
	for index := 0; index < 4; index++ {
		if err := rate.Record(at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(6 * time.Minute)
	}
	if allows(t, rate) {
		t.Fatal("six rebuilds in an hour did not reach the bound")
	}
	// Past the hour, the day still counts them: nine more reach fifteen.
	at = at.Add(time.Hour)
	for index := 0; index < 9; index++ {
		if !allows(t, rate) {
			t.Fatalf("the day's bound was reached after %d of 15", index+6)
		}
		if err := rate.Record(at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(11 * time.Minute)
	}
	if allows(t, rate) {
		t.Fatal("fifteen rebuilds in a day did not reach the bound")
	}
	// A day later, everything is forgotten.
	at = at.Add(25 * time.Hour)
	if !allows(t, rate) {
		t.Fatal("a day of quiet did not clear the day's bound")
	}
}

// The count outlives the process. A runtime that rebuilds, dies and comes back
// is the case the bound exists for.
func TestTheCountSurvivesARestart(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "rebuilds.json")
	first := Rate{Path: path, Now: func() time.Time { return at }}
	for index := 0; index < 2; index++ {
		if err := first.Record(at); err != nil {
			t.Fatal(err)
		}
	}
	second := Rate{Path: path, Now: func() time.Time { return at }}
	if allows(t, second) {
		t.Fatal("a restarted runtime forgot what it had already rebuilt")
	}
	// Only the operator clears it.
	if err := second.Clear(); err != nil {
		t.Fatal(err)
	}
	if !allows(t, second) {
		t.Fatal("a cleared store still refused")
	}
}

// A store that cannot be read is a bound reached, not permission. A rate that
// answered "go ahead" to an unreadable file would be no bound at all.
func TestAnUnreadableStoreRefuses(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "rebuilds.json")
	if err := os.WriteFile(path, []byte("{not a list}"), 0o600); err != nil {
		t.Fatal(err)
	}
	rate := Rate{Path: path, Now: func() time.Time { return at }}
	allowed, err := rate.Allows()
	if err == nil {
		t.Fatal("an unreadable store was read without complaint")
	}
	if allowed {
		t.Fatal("an unreadable store allowed a rebuild")
	}
	// So is a rate with nowhere to keep the count.
	if allowed, err := (Rate{Now: func() time.Time { return at }}).Allows(); allowed || err == nil {
		t.Fatalf("a rate with no path gave %v, %v", allowed, err)
	}
}

// What no bound can still see is forgotten, so the store does not grow for the
// life of the machine.
func TestWhatNoBoundCanSeeIsForgotten(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rate := rateFor(t, &at)
	if err := rate.Record(at.Add(-48 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := rate.Record(at); err != nil {
		t.Fatal(err)
	}
	performed, err := rate.Performed()
	if err != nil {
		t.Fatal(err)
	}
	if len(performed) != 1 || !performed[0].Equal(at) {
		t.Fatalf("performed = %v, want only the recent rebuild", performed)
	}
}
