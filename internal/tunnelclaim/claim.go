// Package tunnelclaim says which runtime owns the tunnel process, as a fact on
// disk rather than an assumption in either.
//
// Two runtimes can start sing-box. Exactly one may, and which one cannot live
// in either of them: a runtime that decided for itself would decide while the
// other still held the process. So it lives in a file both read and neither
// writes.
//
// The supervisor is kept alive by launchd and comes back within ten seconds of
// exiting. A claim it learned only at startup would be forgotten exactly when
// it matters, so the claim is read on every tick and before every start rather
// than once.
package tunnelclaim

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// Schema is stamped into every claim. A file that does not carry it is not
	// a claim, however much it looks like one.
	Schema = "hexroute.tunnel-claim.v2"

	// SchemaV1 is a claim written before a claim said which process it covers.
	// The one placed by the handover of 2026-09-14 is one, and it must still
	// read as held: an unreadable claim stops the previous owner, but a claim
	// read as absent would let it start a second tunnel.
	SchemaV1 = "hexroute.tunnel-claim.v1"

	// HexrouteContent is where this runtime writes the configuration its tunnel
	// runs. A v1 claim covers this, because it is the path that handover used.
	HexrouteContent = "/Library/Application Support/Hexroute/observe-root/state/tunnel-config.json"

	// PreviousOwnerConfig is the configuration the previous owner's tunnel runs.
	// No claim means the previous owner holds the tunnel, and this is how its
	// process is told from anything else running sing-box.
	PreviousOwnerConfig = "/Library/Application Support/twilight/supervisor/client/twilight-sing-box-tun.json"

	// DefaultPath is where both runtimes look.
	//
	// Durable rather than under /var/run, deliberately. A claim that vanished
	// on reboot would hand the tunnel back to the previous owner every time the
	// machine restarted, silently undoing a handover that had been made and
	// proved. A handover that should be undone is undone by releasing it.
	DefaultPath = "/Library/Application Support/Hexroute/tunnel-claim.json"
)

// Holder is a runtime that may own the tunnel.
type Holder string

const (
	// HolderHexroute is this system. It is the only holder that may be claimed:
	// the absence of a claim already means the supervisor owns the tunnel, and
	// a claim naming the supervisor would be a second way to say the same thing
	// that could disagree with the first.
	HolderHexroute Holder = "hexroute"
)

var (
	ErrInvalidClaim = errors.New("invalid tunnel claim")
	ErrHeld         = errors.New("the tunnel is already claimed")
)

// Claim is what one runtime holding the tunnel looks like on disk.
type Claim struct {
	Schema string `json:"schema"`
	Holder Holder `json:"holder"`
	// Transaction is the handover that placed it, so a claim can be traced to
	// the session that made it and a stale one can be told from a current one.
	Transaction string `json:"transaction"`
	ClaimedAt   string `json:"claimed_at"`
	// ProcessConfig is the configuration the holder's tunnel runs. A claim that
	// did not say which process it covers would leave every reader to guess,
	// and a guess is how a probe gets stopped instead of a tunnel.
	ProcessConfig string `json:"process_config,omitempty"`
}

// Store reads and writes the claim at one path.
type Store struct {
	path          string
	now           func() time.Time
	processConfig string
}

func Open(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrInvalidClaim)
	}
	return &Store{path: path, now: time.Now}, nil
}

// WithClock replaces the clock, for a test that needs a fixed stamp.
func (store *Store) WithClock(now func() time.Time) *Store {
	if now != nil {
		store.now = now
	}
	return store
}

// WithProcessConfig names the configuration a claim placed through this store
// covers. Placing without one is refused.
func (store *Store) WithProcessConfig(path string) *Store {
	store.processConfig = path
	return store
}

// OwnerConfig answers which configuration the tunnel's owner runs: the one the
// claim names, or the previous owner's when there is no claim.
//
// An unreadable claim is an error, for the reason Held gives: guessing between
// the two owners is how a probe or the wrong tunnel gets taken for the right one.
func (store *Store) OwnerConfig() (string, error) {
	claim, held, err := store.Held()
	if err != nil {
		return "", err
	}
	if !held {
		return PreviousOwnerConfig, nil
	}
	return claim.ProcessConfig, nil
}

// Held answers whether the tunnel is claimed, and by whom.
//
// A file that cannot be read as a claim is an error rather than an absence. An
// unreadable claim and no claim mean opposite things — one is a runtime that
// may start the tunnel, the other is one that must not — and guessing between
// them is how two owners happen.
func (store *Store) Held() (Claim, bool, error) {
	encoded, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return Claim{}, false, nil
	}
	if err != nil {
		return Claim{}, false, fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	var claim Claim
	if err := json.Unmarshal(encoded, &claim); err != nil {
		return Claim{}, false, fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	if claim.Holder != HolderHexroute || claim.Transaction == "" || claim.ClaimedAt == "" {
		return Claim{}, false, ErrInvalidClaim
	}
	switch claim.Schema {
	case SchemaV1:
		if claim.ProcessConfig != "" {
			return Claim{}, false, ErrInvalidClaim
		}
		claim.ProcessConfig = HexrouteContent
	case Schema:
		if !filepath.IsAbs(claim.ProcessConfig) {
			return Claim{}, false, ErrInvalidClaim
		}
	default:
		return Claim{}, false, ErrInvalidClaim
	}
	return claim, true, nil
}

// Place records that the tunnel is held, and refuses to overwrite a claim.
//
// Refusing rather than replacing, because two transactions overlapping is the
// one thing a claim exists to make impossible. The second learns that the first
// is in flight rather than taking the tunnel from under it.
func (store *Store) Place(transaction string) error {
	if transaction == "" {
		return fmt.Errorf("%w: a claim without a transaction", ErrInvalidClaim)
	}
	if !filepath.IsAbs(store.processConfig) {
		return fmt.Errorf("%w: a claim that does not name the configuration it covers", ErrInvalidClaim)
	}
	if _, held, err := store.Held(); err != nil {
		return err
	} else if held {
		return ErrHeld
	}
	encoded, err := json.Marshal(Claim{
		Schema: Schema, Holder: HolderHexroute,
		Transaction:   transaction,
		ClaimedAt:     store.now().UTC().Format(time.RFC3339),
		ProcessConfig: store.processConfig,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	// Written whole and renamed into place: a reader that arrived mid-write
	// would see a half claim, and a half claim is unreadable, which stops the
	// previous owner from starting the tunnel for no reason.
	staged := store.path + ".staged"
	if err := os.WriteFile(staged, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	if err := os.Rename(staged, store.path); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	return nil
}

// Release gives the tunnel back. Releasing what is not held is not an error:
// an abort runs when the state is uncertain, and it must be able to run twice.
func (store *Store) Release() error {
	if err := os.Remove(store.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	return nil
}
