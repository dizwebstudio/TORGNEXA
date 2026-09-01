-- Outbound version fence for the commerce connector route.  It is a small
-- mutable control table, separate from the append-only receipt/state history:
-- a newer local event wins before another worker is allowed to perform remote
-- IO, while a retry of the same event remains idempotent.
BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE sync_outbound_version_fences (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  local_entity_id text NOT NULL,
  latest_local_version bigint NOT NULL,
  latest_event_id text NOT NULL,
  claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,policy_id,local_entity_id),
  FOREIGN KEY (organization_id,workspace_id,policy_id)
    REFERENCES sync_policies(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT sync_outbound_version_fence_ref_chk CHECK (
    policy_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    local_entity_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    latest_event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    latest_local_version >= 1
  )
);

CREATE INDEX sync_outbound_version_fences_claimed_idx
  ON sync_outbound_version_fences(organization_id,workspace_id,claimed_at DESC);

ALTER TABLE sync_outbound_version_fences ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_outbound_version_fences FORCE ROW LEVEL SECURITY;
CREATE POLICY sync_outbound_version_fences_tenant_all
  ON sync_outbound_version_fences FOR ALL
  USING (organization_id=current_setting('app.organization_id',true)
    AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true)
    AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON sync_outbound_version_fences FROM PUBLIC;

COMMENT ON TABLE sync_outbound_version_fences IS
  'Tenant-scoped outbound version fence. Newer local versions suppress stale remote workers; same-event retries remain idempotent.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
