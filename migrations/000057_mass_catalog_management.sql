BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 230: immutable multi-channel bulk catalog previews and apply evidence.
-- PIM/Product/Offer remain canonical; these tables contain bounded snapshots,
-- digests and normalized outcomes only. Connector credentials and raw remote
-- payloads stay behind the connector/SecretProvider boundary.
CREATE TABLE catalog_bulk_previews (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  preview_id text NOT NULL,
  input_digest char(64) NOT NULL,
  selection_digest char(64) NOT NULL,
  state text NOT NULL,
  affected_sku_count integer NOT NULL,
  affected_row_count integer NOT NULL,
  eligible_row_count integer NOT NULL,
  blocked_row_count integer NOT NULL,
  preview_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,preview_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT catalog_bulk_preview_ref_chk CHECK (
    preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    input_digest ~ '^[0-9a-f]{64}$' AND selection_digest ~ '^[0-9a-f]{64}$' AND
    state = 'previewed' AND affected_sku_count BETWEEN 1 AND 1000 AND
    affected_row_count BETWEEN 1 AND 8000 AND eligible_row_count >= 0 AND
    blocked_row_count >= 0 AND eligible_row_count + blocked_row_count = affected_row_count AND
    jsonb_typeof(preview_document) = 'object' AND pg_column_size(preview_document) <= 67108864 AND
    preview_document::text !~* 'authorization|access_token|client_secret|private_key|raw_provider_payload|<script'
  )
);
CREATE INDEX catalog_bulk_previews_created_idx ON catalog_bulk_previews(organization_id,workspace_id,created_at DESC,preview_id DESC);

CREATE TABLE catalog_bulk_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  preview_id text NOT NULL,
  actor_ref text NOT NULL,
  idempotency_key text NOT NULL,
  approval_request_id text NOT NULL,
  state text NOT NULL,
  input_digest char(64) NOT NULL,
  partition_count integer NOT NULL,
  result_count integer NOT NULL,
  run_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,run_id),
  CONSTRAINT catalog_bulk_run_ref_chk CHECK (
    run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    actor_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{7,127}$' AND
    approval_request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    state IN ('queued','running','partial','completed','failed','cancelled','unknown') AND
    input_digest ~ '^[0-9a-f]{64}$' AND partition_count BETWEEN 1 AND 8000 AND
    result_count BETWEEN 1 AND 8000 AND jsonb_typeof(run_document) = 'object' AND
    pg_column_size(run_document) <= 67108864 AND created_at <= updated_at AND
    run_document::text !~* 'authorization|access_token|client_secret|private_key|raw_provider_payload|<script'
  ),
  CONSTRAINT catalog_bulk_run_preview_fk FOREIGN KEY (organization_id,workspace_id,preview_id) REFERENCES catalog_bulk_previews(organization_id,workspace_id,preview_id) ON DELETE RESTRICT,
  CONSTRAINT catalog_bulk_run_idempotency_uq UNIQUE (organization_id,workspace_id,idempotency_key)
);
CREATE INDEX catalog_bulk_runs_updated_idx ON catalog_bulk_runs(organization_id,workspace_id,updated_at DESC,run_id DESC);

CREATE FUNCTION catalog_bulk_workspace_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''catalog bulk evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER catalog_bulk_previews_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON catalog_bulk_previews FOR EACH STATEMENT EXECUTE FUNCTION catalog_bulk_workspace_no_mutation();
CREATE TRIGGER catalog_bulk_runs_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON catalog_bulk_runs FOR EACH STATEMENT EXECUTE FUNCTION catalog_bulk_workspace_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON catalog_bulk_previews,catalog_bulk_runs FROM PUBLIC;

ALTER TABLE catalog_bulk_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_bulk_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE catalog_bulk_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_bulk_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_bulk_previews_tenant_all ON catalog_bulk_previews FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY catalog_bulk_runs_tenant_all ON catalog_bulk_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE catalog_bulk_previews IS 'Tenant-scoped immutable multi-channel catalog before/after and quality evidence for Task 230.';
COMMENT ON TABLE catalog_bulk_runs IS 'Tenant-scoped immutable approval-bound catalog bulk apply intent and per-row outcomes for Task 230.';

CREATE TABLE catalog_bulk_kill_switches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL,
  enabled boolean NOT NULL,
  reason text NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,version),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT catalog_bulk_kill_switch_ref_chk CHECK (version >= 1 AND length(reason) <= 500 AND updated_at IS NOT NULL)
);
CREATE INDEX catalog_bulk_kill_switch_latest_idx ON catalog_bulk_kill_switches(organization_id,workspace_id,version DESC);
CREATE TRIGGER catalog_bulk_kill_switches_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON catalog_bulk_kill_switches FOR EACH STATEMENT EXECUTE FUNCTION catalog_bulk_workspace_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON catalog_bulk_kill_switches FROM PUBLIC;
ALTER TABLE catalog_bulk_kill_switches ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_bulk_kill_switches FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_bulk_kill_switches_tenant_all ON catalog_bulk_kill_switches FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
COMMENT ON TABLE catalog_bulk_kill_switches IS 'Tenant-scoped append-only emergency stop state for mass catalog remote writes.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
