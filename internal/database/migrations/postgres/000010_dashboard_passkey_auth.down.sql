DROP OWNED BY hexroute_dashboard_auth;
DROP ROLE hexroute_dashboard_auth;

GRANT SELECT ON dashboard_principals, passkey_credentials
    TO hexroute_dashboard;

ALTER TABLE passkey_credentials
    DROP COLUMN authenticator_attachment,
    DROP COLUMN clone_warning,
    DROP COLUMN backup_state,
    DROP COLUMN backup_eligible,
    DROP COLUMN user_verified,
    DROP COLUMN user_present;
