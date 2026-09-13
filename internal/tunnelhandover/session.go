// Package tunnelhandover holds the transaction that moves the tunnel from one
// runtime to the other.
//
// It is held by an operator's command in the foreground rather than by a
// daemon. Giving a daemon the authority to start another runtime's launchd job
// would outlive the forty seconds it is needed for, and a written procedure is
// not a transaction at all.
//
// What a terminal cannot do is survive being closed, so the phase reached is
// recorded as it advances. A later invocation reads what it finds and can
// finish or abandon it; a machine whose owner nobody can determine is the state
// this record exists to prevent.
package tunnelhandover

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	Schema = "hexroute.tunnel-handover.v1"

	// DefaultPath is beside the runtime's other durable state.
	DefaultPath = "/Library/Application Support/Hexroute/observe-root/state/handover.json"
)

// Phase is how far a transaction reached.
//
// The order matters: an abort undoes everything up to the phase it finds, and
// nothing beyond it.
type Phase string

const (
	// PhasePrepared is written before anything is touched, so that a
	// transaction interrupted at its very first step is still discoverable.
	PhasePrepared Phase = "prepared"
	// PhaseClaimed means the claim is on disk: the supervisor has stopped
	// starting the tunnel and this runtime has not started it yet. It is the
	// one phase in which nobody owns a running tunnel, which is why the abort
	// path is written before it is entered rather than after.
	PhaseClaimed Phase = "claimed"
	// PhaseStarted means this runtime started the tunnel and is proving it.
	PhaseStarted Phase = "started"
	PhaseProven  Phase = "proven"
	PhaseAborted Phase = "aborted"
)

var (
	ErrInvalidSession = errors.New("invalid handover session")
	ErrInFlight       = errors.New("a handover is already in flight")
)

// Session is the durable record of one transaction.
type Session struct {
	Schema      string `json:"schema"`
	Transaction string `json:"transaction"`
	Phase       Phase  `json:"phase"`
	StartedAt   string `json:"started_at"`
	// Rehearsal says this transaction performs every phase except claiming the
	// tunnel and starting the process. A reader must be able to tell one from a
	// real handover without inferring it from what is absent.
	Rehearsal bool `json:"rehearsal"`
	// StartedPID is what this runtime started, so an abort can stop exactly
	// that and not whatever holds the name now.
	StartedPID int `json:"started_pid,omitempty"`
}

type Store struct {
	path string
	now  func() time.Time
}

func OpenStore(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: path must be absolute", ErrInvalidSession)
	}
	return &Store{path: path, now: time.Now}, nil
}

func (store *Store) WithClock(now func() time.Time) *Store {
	if now != nil {
		store.now = now
	}
	return store
}

// Read returns the session in flight, if there is one.
//
// A file that cannot be read as a session is an error rather than an absence,
// for the reason the claim keeps: they mean opposite things, and starting a
// second transaction over an unreadable first is how two of them overlap.
func (store *Store) Read() (Session, bool, error) {
	encoded, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	var session Session
	if err := json.Unmarshal(encoded, &session); err != nil {
		return Session{}, false, fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if session.Schema != Schema || session.Transaction == "" ||
		!validPhase(session.Phase) || session.StartedAt == "" {
		return Session{}, false, ErrInvalidSession
	}
	return session, true, nil
}

// Begin records a new transaction, and refuses beside one in flight.
func (store *Store) Begin(transaction string, rehearsal bool) (Session, error) {
	if transaction == "" {
		return Session{}, fmt.Errorf("%w: a session without a transaction", ErrInvalidSession)
	}
	existing, inFlight, err := store.Read()
	if err != nil {
		return Session{}, err
	}
	if inFlight && existing.Phase != PhaseProven && existing.Phase != PhaseAborted {
		return Session{}, ErrInFlight
	}
	session := Session{
		Schema: Schema, Transaction: transaction, Phase: PhasePrepared,
		StartedAt: store.now().UTC().Format(time.RFC3339), Rehearsal: rehearsal,
	}
	return session, store.write(session)
}

// Advance records a phase reached. It is called before the act it names, not
// after: a transaction that fell over between acting and recording would leave
// a machine changed by a phase no record mentions.
func (store *Store) Advance(session Session, phase Phase) (Session, error) {
	if !validPhase(phase) {
		return session, fmt.Errorf("%w: unknown phase %q", ErrInvalidSession, phase)
	}
	session.Phase = phase
	return session, store.write(session)
}

// Clear removes the record. A transaction that finished leaves nothing to find.
func (store *Store) Clear() error {
	if err := os.Remove(store.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	return nil
}

func (store *Store) write(session Session) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	staged := store.path + ".staged"
	if err := os.WriteFile(staged, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if err := os.Rename(staged, store.path); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	return nil
}

func validPhase(phase Phase) bool {
	switch phase {
	case PhasePrepared, PhaseClaimed, PhaseStarted, PhaseProven, PhaseAborted:
		return true
	default:
		return false
	}
}
