DROP TRIGGER hexroute_write_gate ON deployments;
DROP TRIGGER hexroute_write_gate ON config_versions;

REVOKE INSERT, UPDATE ON deployments FROM hexroute_maintenance;
REVOKE UPDATE ON config_versions FROM hexroute_maintenance;
REVOKE SELECT, INSERT ON config_versions FROM hexroute_publisher;
REVOKE SELECT ON cutover_write_control FROM hexroute_publisher;
REVOKE USAGE ON SCHEMA public FROM hexroute_publisher;

DO $$
BEGIN
    EXECUTE format(
        'REVOKE CONNECT ON DATABASE %I FROM hexroute_publisher',
        CURRENT_DATABASE()
    );
END
$$;

DROP ROLE IF EXISTS hexroute_publisher;
