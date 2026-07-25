DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'hexroute_dashboard_auth'
    ) THEN
        CREATE ROLE hexroute_dashboard_auth
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$$;

ALTER ROLE hexroute_dashboard_auth
    NOLOGIN NOCREATEDB NOCREATEROLE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_roles
        WHERE rolname = 'hexroute_dashboard_auth'
          AND (
              rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR
              rolreplication OR rolbypassrls
          )
    ) THEN
        RAISE EXCEPTION
            'application role hexroute_dashboard_auth has elevated attributes';
    END IF;
END
$$;

DO $$
BEGIN
    EXECUTE format(
        'GRANT hexroute_dashboard_auth TO %I',
        CURRENT_USER
    );
    EXECUTE format(
        'GRANT CONNECT ON DATABASE %I TO hexroute_dashboard_auth',
        CURRENT_DATABASE()
    );
END
$$;

GRANT USAGE ON SCHEMA public TO hexroute_dashboard_auth;
REVOKE SELECT ON dashboard_principals, passkey_credentials
    FROM hexroute_dashboard;

ALTER TABLE passkey_credentials
    ADD COLUMN user_present BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN user_verified BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN backup_state BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN clone_warning BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN authenticator_attachment TEXT NOT NULL DEFAULT ''
        CHECK (
            authenticator_attachment IN ('', 'platform', 'cross-platform')
        );

GRANT SELECT ON dashboard_principals, passkey_credentials
    TO hexroute_dashboard_auth;
GRANT INSERT ON passkey_credentials TO hexroute_dashboard_auth;
GRANT UPDATE (last_authenticated_at) ON dashboard_principals
    TO hexroute_dashboard_auth;
GRANT UPDATE (
    sign_count,
    user_present,
    user_verified,
    backup_state,
    clone_warning,
    last_used_at
) ON passkey_credentials TO hexroute_dashboard_auth;
