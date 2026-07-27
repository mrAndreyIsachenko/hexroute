package cutoverfreeze

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestStateReadyAtIsBoundedAndFailsClosed(t *testing.T) {
	frozenAt := time.Date(2026, time.July, 27, 1, 0, 0, 0, time.UTC)
	state := State{
		Frozen:     true,
		FrozenAt:   frozenAt,
		DeadlineAt: frozenAt.Add(15 * time.Minute),
	}
	if !state.ReadyAt(frozenAt.Add(time.Minute)) {
		t.Fatal("valid frozen state is not ready")
	}
	if state.ReadyAt(state.DeadlineAt) || state.ReadyAt(state.DeadlineAt.Add(time.Second)) {
		t.Fatal("expired frozen state is ready")
	}
	if !(State{}).ReadyAt(frozenAt) {
		t.Fatal("normal state is not ready")
	}
}

func TestIsWriteFrozenRequiresStablePostgresCondition(t *testing.T) {
	err := errors.Join(errors.New("wrapped"), &pgconn.PgError{
		Code:    "55000",
		Message: "write_frozen",
	})
	if !IsWriteFrozen(err) {
		t.Fatal("write_frozen condition was not recognized")
	}
	if IsWriteFrozen(&pgconn.PgError{Code: "55000", Message: "other"}) ||
		IsWriteFrozen(errors.New("write_frozen")) {
		t.Fatal("unrelated error was recognized as write freeze")
	}
}
