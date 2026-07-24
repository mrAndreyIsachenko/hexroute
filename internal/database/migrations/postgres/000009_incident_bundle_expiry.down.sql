DROP INDEX incident_bundles_delete_due_idx;
DROP INDEX incident_bundles_incident_content_uidx;

ALTER TABLE incident_bundles
    DROP CONSTRAINT incident_bundles_delete_claim_pair_check,
    DROP COLUMN last_delete_result_code,
    DROP COLUMN next_delete_attempt_at,
    DROP COLUMN delete_attempt_count,
    DROP COLUMN delete_claim_until,
    DROP COLUMN delete_claim_owner;
