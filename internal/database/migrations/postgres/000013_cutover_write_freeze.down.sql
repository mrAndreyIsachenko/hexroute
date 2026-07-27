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
            'DROP TRIGGER IF EXISTS hexroute_write_gate ON %I',
            table_name
        );
    END LOOP;
END
$$;

DROP FUNCTION hexroute_enforce_writable();
DROP TABLE cutover_write_control;
