BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 137: verified webhook delivery resolves a payment by the provider's
-- own remote_id, not by our external_id. remote_id is unique per connector
-- account by construction (a gateway account cannot hand out the same
-- remote payment id twice), which this index both enforces and serves.
CREATE UNIQUE INDEX payments_remote_uq ON payments(organization_id,workspace_id,connector_account_id,remote_id) WHERE remote_id IS NOT NULL;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
