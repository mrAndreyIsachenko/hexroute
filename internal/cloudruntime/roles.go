package cloudruntime

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type databaseRole string

const (
	roleMigrator      databaseRole = "hexroute_migrator"
	roleIngest        databaseRole = "hexroute_ingest"
	roleDashboard     databaseRole = "hexroute_dashboard"
	roleDashboardAuth databaseRole = "hexroute_dashboard_auth"
	roleMaintenance   databaseRole = "hexroute_maintenance"
)

var ErrDatabaseRoleMismatch = errors.New("cloud database role mismatch")

func requireExclusiveRole(
	ctx context.Context,
	database queryRower,
	expected databaseRole,
) error {
	if ctx == nil || database == nil || !validDatabaseRole(expected) {
		return ErrDatabaseRoleMismatch
	}
	var (
		migrator      bool
		ingest        bool
		dashboard     bool
		dashboardAuth bool
		maintenance   bool
	)
	err := database.QueryRow(ctx, `
		SELECT
			pg_has_role(current_user, 'hexroute_migrator', 'member'),
			pg_has_role(current_user, 'hexroute_ingest', 'member'),
			pg_has_role(current_user, 'hexroute_dashboard', 'member'),
			pg_has_role(current_user, 'hexroute_dashboard_auth', 'member'),
			pg_has_role(current_user, 'hexroute_maintenance', 'member')
	`).Scan(
		&migrator,
		&ingest,
		&dashboard,
		&dashboardAuth,
		&maintenance,
	)
	if err != nil {
		return ErrDatabaseRoleMismatch
	}
	memberships := map[databaseRole]bool{
		roleMigrator:      migrator,
		roleIngest:        ingest,
		roleDashboard:     dashboard,
		roleDashboardAuth: dashboardAuth,
		roleMaintenance:   maintenance,
	}
	for role, member := range memberships {
		if member != (role == expected) {
			return ErrDatabaseRoleMismatch
		}
	}
	return nil
}

func validDatabaseRole(role databaseRole) bool {
	switch role {
	case roleMigrator, roleIngest, roleDashboard, roleDashboardAuth, roleMaintenance:
		return true
	default:
		return false
	}
}
