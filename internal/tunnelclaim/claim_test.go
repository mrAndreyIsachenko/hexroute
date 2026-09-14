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
	opened = opened.WithProcessConfig(HexrouteContent)
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

// A claim placed now names the configuration its holder runs.
func TestAPlacedClaimNamesWhatItCovers(t *testing.T) {
	opened, _ := store(t)
	if err := opened.Place("handover-covers"); err != nil {
		t.Fatalf("Place: %v", err)
	}
	claim, held, err := opened.Held()
	if err != nil || !held {
		t.Fatalf("Held = %v, %v", held, err)
	}
	if claim.Schema != Schema || claim.ProcessConfig != HexrouteContent {
		t.Fatalf("claim = %+v", claim)
	}
}

// A claim that would not say what it covers is not placed.
func TestAClaimWithoutAConfigurationIsNotPlaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel-claim.json")
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Place("handover-anonymous"); !errors.Is(err, ErrInvalidClaim) {
		t.Fatalf("Place without a configuration = %v, want ErrInvalidClaim", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a claim was written that does not say what it covers")
	}
}

// The claim on disk since 2026-09-14 is a v1 claim, and it still holds.
//
// Reading it as absent would let the previous owner start a second tunnel
// beside this runtime's; reading it as unreadable would be safe but would
// leave the daemon unable to say which process is the tunnel. It covers the
// path that handover wrote.
func TestAVersionOneClaimStillHoldsAndCoversTheDefault(t *testing.T) {
	opened, path := store(t)
	v1 := `{"schema":"` + SchemaV1 + `","holder":"hexroute","transaction":"handover-1789371131","claimed_at":"2026-09-14T07:32:00Z"}`
	if err := os.WriteFile(path, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	claim, held, err := opened.Held()
	if err != nil || !held {
		t.Fatalf("a v1 claim read as held=%v err=%v", held, err)
	}
	if claim.ProcessConfig != HexrouteContent {
		t.Fatalf("a v1 claim covers %q, want %q", claim.ProcessConfig, HexrouteContent)
	}
}

// A v2 claim that does not name an absolute configuration is not a claim.
func TestAVersionTwoClaimMustNameAConfiguration(t *testing.T) {
	for _, item := range []struct{ name, config string }{
		{"absent", ""},
		{"relative", "tunnel-config.json"},
	} {
		t.Run(item.name, func(t *testing.T) {
			opened, path := store(t)
			body := `{"schema":"` + Schema + `","holder":"hexroute","transaction":"x","claimed_at":"now","process_config":"` + item.config + `"}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, held, err := opened.Held(); err == nil || held {
				t.Fatalf("a v2 claim with config %q read as held=%v err=%v", item.config, held, err)
			}
		})
	}
}

// The owner's configuration follows the claim.
func TestTheOwnersConfigurationFollowsTheClaim(t *testing.T) {
	opened, path := store(t)

	config, err := opened.OwnerConfig()
	if err != nil || config != PreviousOwnerConfig {
		t.Fatalf("no claim: OwnerConfig = %q, %v; want the previous owner's", config, err)
	}

	if err := opened.Place("handover-owner"); err != nil {
		t.Fatal(err)
	}
	config, err = opened.OwnerConfig()
	if err != nil || config != HexrouteContent {
		t.Fatalf("claimed: OwnerConfig = %q, %v; want this runtime's", config, err)
	}

	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if config, err := opened.OwnerConfig(); err == nil {
		t.Fatalf("an unreadable claim answered %q instead of refusing", config)
	}
}
