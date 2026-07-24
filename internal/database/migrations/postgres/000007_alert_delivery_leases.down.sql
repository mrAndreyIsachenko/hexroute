DROP INDEX alert_deliveries_claimable_idx;

ALTER TABLE alert_deliveries
    DROP CONSTRAINT alert_deliveries_snapshot_component_check,
    DROP CONSTRAINT alert_deliveries_snapshot_category_check,
    DROP CONSTRAINT alert_deliveries_snapshot_severity_check,
    DROP CONSTRAINT alert_deliveries_snapshot_status_check,
    DROP CONSTRAINT alert_deliveries_claim_pair_check,
    DROP COLUMN snapshot_transitioned_at,
    DROP COLUMN snapshot_component,
    DROP COLUMN snapshot_category,
    DROP COLUMN snapshot_severity,
    DROP COLUMN snapshot_status,
    DROP COLUMN claim_until,
    DROP COLUMN claim_owner;
