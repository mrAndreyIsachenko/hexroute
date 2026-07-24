DO $$
DECLARE
    role_name TEXT;
BEGIN
    FOREACH role_name IN ARRAY ARRAY[
        'hexroute_migrator',
        'hexroute_ingest',
        'hexroute_dashboard',
        'hexroute_maintenance'
    ]
    LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN
            EXECUTE format(
                'CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS',
                role_name
            );
        END IF;
    END LOOP;
END
$$;

ALTER ROLE hexroute_migrator
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE hexroute_ingest
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE hexroute_dashboard
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE hexroute_maintenance
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;

DO $$
BEGIN
    EXECUTE format(
        'GRANT hexroute_migrator TO %I',
        CURRENT_USER
    );
    EXECUTE format(
        'REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC',
        CURRENT_DATABASE()
    );
    EXECUTE format(
        'GRANT CONNECT ON DATABASE %I TO hexroute_migrator, hexroute_ingest, hexroute_dashboard, hexroute_maintenance',
        CURRENT_DATABASE()
    );
END
$$;

REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC;

GRANT USAGE, CREATE ON SCHEMA public TO hexroute_migrator;
GRANT USAGE ON SCHEMA public
    TO hexroute_ingest, hexroute_dashboard, hexroute_maintenance;

ALTER TABLE nodes OWNER TO hexroute_migrator;
ALTER TABLE node_public_keys OWNER TO hexroute_migrator;
ALTER TABLE batches OWNER TO hexroute_migrator;
ALTER TABLE events OWNER TO hexroute_migrator;
ALTER TABLE node_sequence_cursors OWNER TO hexroute_migrator;
ALTER TABLE sequence_gaps OWNER TO hexroute_migrator;
ALTER TABLE security_audit_records OWNER TO hexroute_migrator;
ALTER TABLE latest_component_states OWNER TO hexroute_migrator;
ALTER TABLE sleep_intervals OWNER TO hexroute_migrator;
ALTER TABLE incidents OWNER TO hexroute_migrator;
ALTER TABLE incident_events OWNER TO hexroute_migrator;
ALTER TABLE incident_transitions OWNER TO hexroute_migrator;
ALTER TABLE incident_bundles OWNER TO hexroute_migrator;
ALTER TABLE config_versions OWNER TO hexroute_migrator;
ALTER TABLE deployments OWNER TO hexroute_migrator;
ALTER TABLE worker_heartbeats OWNER TO hexroute_migrator;
ALTER TABLE dashboard_principals OWNER TO hexroute_migrator;
ALTER TABLE passkey_credentials OWNER TO hexroute_migrator;
ALTER TABLE alert_deliveries OWNER TO hexroute_migrator;
ALTER TABLE slo_aggregates OWNER TO hexroute_migrator;
ALTER TABLE slo_incident_links OWNER TO hexroute_migrator;

GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO hexroute_migrator;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO hexroute_migrator;

GRANT SELECT ON nodes, node_public_keys TO hexroute_ingest;
GRANT UPDATE (first_seen_at, last_seen_at, updated_at) ON nodes TO hexroute_ingest;
GRANT SELECT, INSERT ON batches, events TO hexroute_ingest;
GRANT SELECT, INSERT, UPDATE ON
    node_sequence_cursors,
    sequence_gaps,
    latest_component_states
TO hexroute_ingest;
GRANT INSERT ON security_audit_records TO hexroute_ingest;

GRANT SELECT ON
    nodes,
    latest_component_states,
    sleep_intervals,
    incidents,
    incident_events,
    incident_transitions,
    incident_bundles,
    config_versions,
    deployments,
    worker_heartbeats,
    dashboard_principals,
    passkey_credentials,
    alert_deliveries,
    slo_aggregates,
    slo_incident_links
TO hexroute_dashboard;

GRANT SELECT ON
    nodes,
    batches,
    events,
    node_sequence_cursors,
    sequence_gaps,
    security_audit_records,
    latest_component_states,
    sleep_intervals,
    incidents,
    incident_events,
    incident_transitions,
    incident_bundles,
    config_versions,
    deployments,
    worker_heartbeats,
    alert_deliveries,
    slo_aggregates,
    slo_incident_links
TO hexroute_maintenance;
GRANT INSERT, UPDATE, DELETE ON
    latest_component_states,
    sleep_intervals,
    incidents,
    incident_events,
    incident_transitions,
    incident_bundles,
    worker_heartbeats,
    alert_deliveries,
    slo_aggregates,
    slo_incident_links
TO hexroute_maintenance;
GRANT UPDATE, DELETE ON sequence_gaps TO hexroute_maintenance;
GRANT DELETE ON batches, events, security_audit_records TO hexroute_maintenance;

ALTER DEFAULT PRIVILEGES FOR ROLE hexroute_migrator IN SCHEMA public
    REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE hexroute_migrator IN SCHEMA public
    REVOKE ALL ON SEQUENCES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE hexroute_migrator IN SCHEMA public
    REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
