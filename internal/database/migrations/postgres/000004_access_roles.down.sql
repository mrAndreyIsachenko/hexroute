REASSIGN OWNED BY hexroute_migrator TO CURRENT_USER;

DROP OWNED BY hexroute_ingest;
DROP OWNED BY hexroute_dashboard;
DROP OWNED BY hexroute_maintenance;
DROP OWNED BY hexroute_migrator;

DO $$
BEGIN
    EXECUTE format(
        'GRANT CONNECT, TEMPORARY ON DATABASE %I TO PUBLIC',
        CURRENT_DATABASE()
    );
END
$$;

GRANT USAGE, CREATE ON SCHEMA public TO PUBLIC;

DROP ROLE hexroute_ingest;
DROP ROLE hexroute_dashboard;
DROP ROLE hexroute_maintenance;
DROP ROLE hexroute_migrator;
