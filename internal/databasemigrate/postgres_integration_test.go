package databasemigrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRunnerAdoptsBaselineAndSeedsOnePrincipal(t *testing.T) {
	dsn := os.Getenv("HEXROUTE_TEST_POSTGRES_ADMIN_DSN")
	if dsn == "" {
		t.Skip("HEXROUTE_TEST_POSTGRES_ADMIN_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	runner, err := New(pool, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err = runner.Apply(ctx, "operator", "Operator"); err != nil {
			t.Fatalf("Apply(%d) error = %v", attempt, err)
		}
	}
	var migrationsCount, principalCount int
	if err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM hexroute_schema_migrations),
			(SELECT count(*) FROM dashboard_principals
			 WHERE username = 'operator' AND enabled)
	`).Scan(&migrationsCount, &principalCount); err != nil {
		t.Fatalf("verification error = %v", err)
	}
	if migrationsCount != int(ledgerVersion) || principalCount != 1 {
		t.Fatalf(
			"migrations=%d principals=%d",
			migrationsCount,
			principalCount,
		)
	}
}
