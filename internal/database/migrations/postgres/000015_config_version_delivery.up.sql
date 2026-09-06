-- config_versions and deployments have existed since 000002 with no writer.
-- Signed configuration delivery is their producer, and it needs two different
-- writers: the operator publishes a version, and the worker records what
-- became of it.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'hexroute_publisher'
    ) THEN
        CREATE ROLE hexroute_publisher
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_roles
        WHERE rolname = 'hexroute_publisher'
          AND (
              rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR
              rolreplication OR rolbypassrls
          )
    ) THEN
        RAISE EXCEPTION 'application role hexroute_publisher has elevated attributes';
    END IF;
END
$$;

DO $$
BEGIN
    EXECUTE format(
        'GRANT CONNECT ON DATABASE %I TO hexroute_publisher',
        CURRENT_DATABASE()
    );
END
$$;

GRANT USAGE ON SCHEMA public TO hexroute_publisher;
GRANT SELECT ON cutover_write_control TO hexroute_publisher;

-- The publisher adds a row and reads back the one it may have added before.
-- It cannot change a version's lifecycle, because whether a version is proven
-- is not the publisher's to assert.
GRANT SELECT, INSERT ON config_versions TO hexroute_publisher;

-- The worker moves a version through its lifecycle and records deployments.
-- It cannot insert a version: nothing that runs unattended may bring one into
-- existence.
GRANT UPDATE ON config_versions TO hexroute_maintenance;
GRANT INSERT, UPDATE ON deployments TO hexroute_maintenance;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'config_versions',
        'deployments'
    ]
    LOOP
        EXECUTE format(
            'CREATE TRIGGER hexroute_write_gate BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION hexroute_enforce_writable()',
            table_name
        );
    END LOOP;
END
$$;
