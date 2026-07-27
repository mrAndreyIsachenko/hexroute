package cutoverfreeze

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const RetryAfterSeconds = 60

type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Reader interface {
	Read(context.Context) (State, error)
}

type Store struct {
	database Queryer
}

type State struct {
	Frozen     bool
	FrozenAt   time.Time
	DeadlineAt time.Time
}

var (
	ErrInvalidConfig = errors.New("invalid cutover freeze configuration")
	ErrInvalidState  = errors.New("invalid cutover freeze state")
	ErrUnavailable   = errors.New("cutover freeze state unavailable")
)

func NewStore(database Queryer) (*Store, error) {
	if database == nil {
		return nil, ErrInvalidConfig
	}
	return &Store{database: database}, nil
}

func (store *Store) Read(ctx context.Context) (State, error) {
	if store == nil || store.database == nil || ctx == nil {
		return State{}, ErrInvalidConfig
	}
	var (
		state      State
		frozenAt   *time.Time
		deadlineAt *time.Time
	)
	err := store.database.QueryRow(ctx, `
		SELECT write_frozen, frozen_at, deadline_at
		FROM cutover_write_control
		WHERE singleton
	`).Scan(&state.Frozen, &frozenAt, &deadlineAt)
	if err != nil {
		return State{}, errors.Join(ErrUnavailable, err)
	}
	if !state.Frozen {
		if frozenAt != nil || deadlineAt != nil {
			return State{}, ErrInvalidState
		}
		return state, nil
	}
	if frozenAt == nil || deadlineAt == nil {
		return State{}, ErrInvalidState
	}
	state.FrozenAt = frozenAt.UTC()
	state.DeadlineAt = deadlineAt.UTC()
	if !state.DeadlineAt.After(state.FrozenAt) {
		return State{}, ErrInvalidState
	}
	return state, nil
}

func (state State) ReadyAt(now time.Time) bool {
	if !state.Frozen {
		return state.FrozenAt.IsZero() && state.DeadlineAt.IsZero()
	}
	return !state.FrozenAt.IsZero() &&
		state.DeadlineAt.After(state.FrozenAt) &&
		now.UTC().Before(state.DeadlineAt)
}

func IsWriteFrozen(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == "55000" &&
		postgresError.Message == "write_frozen"
}
