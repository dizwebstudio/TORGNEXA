BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Realtime only needs the newest opaque audit ID for one tenant. The existing
-- resource index cannot satisfy that order efficiently, so keep the hot
-- lookup tenant-local and index-only as the audit table grows.
CREATE INDEX audit_records_tenant_id_idx
  ON audit_records (organization_id, workspace_id, id DESC);

INSERT INTO migration_history (
  version,
  name,
  file_name,
  phase,
  risk,
  checksum_sha256,
  application_version,
  execution_id,
  duration_ms
) VALUES (
  current_setting('torgnexa.migration_version')::integer,
  current_setting('torgnexa.migration_name'),
  current_setting('torgnexa.migration_file'),
  current_setting('torgnexa.migration_phase'),
  current_setting('torgnexa.migration_risk'),
  current_setting('torgnexa.migration_checksum'),
  current_setting('torgnexa.application_version'),
  current_setting('torgnexa.migration_execution_id'),
  current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
