CREATE TABLE cutover_write_control (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    cutover_id UUID,
    write_frozen BOOLEAN NOT NULL DEFAULT FALSE,
    frozen_at TIMESTAMPTZ,
    deadline_at TIMESTAMPTZ,
    CHECK (
        (
            NOT write_frozen
            AND cutover_id IS NULL
            AND frozen_at IS NULL
            AND deadline_at IS NULL
        ) OR (
            write_frozen
            AND cutover_id IS NOT NULL
            AND frozen_at IS NOT NULL
            AND deadline_at IS NOT NULL
            AND deadline_at > frozen_at
        )
    )
);

INSERT INTO cutover_write_control (singleton, write_frozen)
VALUES (TRUE, FALSE);

ALTER TABLE cutover_write_control OWNER TO hexroute_migrator;
REVOKE ALL ON cutover_write_control FROM PUBLIC;
GRANT SELECT ON cutover_write_control TO
    hexroute_ingest,
    hexroute_dashboard,
    hexroute_dashboard_auth,
    hexroute_maintenance;

CREATE FUNCTION hexroute_enforce_writable()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    frozen BOOLEAN;
BEGIN
    SELECT write_frozen
    INTO frozen
    FROM public.cutover_write_control
    WHERE singleton
    FOR SHARE;

    IF NOT FOUND THEN
        RAISE SQLSTATE '55000' USING MESSAGE = 'write_freeze_state_invalid';
    END IF;
    IF frozen THEN
        RAISE SQLSTATE '55000' USING MESSAGE = 'write_frozen';
    END IF;
    RETURN NULL;
END
$$;

ALTER FUNCTION hexroute_enforce_writable() OWNER TO hexroute_migrator;
REVOKE ALL ON FUNCTION hexroute_enforce_writable() FROM PUBLIC;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'nodes',
        'batches',
        'events',
        'node_sequence_cursors',
        'sequence_gaps',
        'security_audit_records',
        'latest_component_states',
        'sleep_intervals',
        'incidents',
        'incident_events',
        'incident_transitions',
        'incident_bundles',
        'worker_heartbeats',
        'dashboard_principals',
        'passkey_credentials',
        'alert_deliveries',
        'incident_alert_outbox',
        'slo_aggregates',
        'slo_incident_links'
    ]
    LOOP
        EXECUTE format(
            'CREATE TRIGGER hexroute_write_gate BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION hexroute_enforce_writable()',
            table_name
        );
    END LOOP;
END
$$;
