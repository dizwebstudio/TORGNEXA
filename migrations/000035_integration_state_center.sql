BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 168: rebuildable, tenant-scoped integration-state metadata. Source
-- account, health, capability, sync and reconciliation tables remain the
-- authority; these tables only retain bounded evidence and work receipts.
CREATE TABLE integration_center_snapshots (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  snapshot_id text NOT NULL,
  snapshot_version integer NOT NULL DEFAULT 1,
  snapshot_digest char(64) NOT NULL,
  generated_at timestamptz NOT NULL,
  consistency text NOT NULL DEFAULT 'best_effort',
  partial boolean NOT NULL DEFAULT false,
  source_watermarks jsonb NOT NULL DEFAULT '{}'::jsonb,
  account_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, snapshot_id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT integration_center_snapshot_ref_chk CHECK(snapshot_id ~ '^ic:[0-9a-f]{26}$' AND snapshot_version >= 1 AND snapshot_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT integration_center_snapshot_state_chk CHECK(consistency IN ('atomic','best_effort') AND account_count BETWEEN 0 AND 100000 AND jsonb_typeof(source_watermarks)='object' AND pg_column_size(source_watermarks) <= 65536),
  CONSTRAINT integration_center_snapshot_time_chk CHECK(generated_at <= created_at)
);
CREATE UNIQUE INDEX integration_center_snapshot_digest_uq ON integration_center_snapshots(organization_id,workspace_id,snapshot_digest);
CREATE INDEX integration_center_snapshot_recent_idx ON integration_center_snapshots(organization_id,workspace_id,generated_at DESC,snapshot_id DESC);

CREATE TABLE integration_center_snapshot_accounts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  snapshot_id text NOT NULL,
  account_id text NOT NULL,
  connector_id text NOT NULL,
  family text NOT NULL,
  surface text NOT NULL,
  display_name text NOT NULL DEFAULT '',
  overall text NOT NULL,
  account_version bigint NOT NULL,
  dimensions jsonb NOT NULL,
  capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
  issues jsonb NOT NULL DEFAULT '[]'::jsonb,
  actions jsonb NOT NULL DEFAULT '[]'::jsonb,
  row_digest char(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,snapshot_id,account_id),
  FOREIGN KEY (organization_id,workspace_id,snapshot_id) REFERENCES integration_center_snapshots(organization_id,workspace_id,snapshot_id) ON DELETE RESTRICT,
  CONSTRAINT integration_center_snapshot_account_ref_chk CHECK(account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND connector_id ~ '^[a-z0-9][a-z0-9-]{0,95}$' AND family ~ '^[a-z][a-z0-9._:/-]{0,63}$' AND surface ~ '^[a-z][a-z0-9._:/-]{0,63}$' AND char_length(display_name) <= 160 AND display_name = btrim(display_name) AND account_version >= 1 AND overall IN ('healthy','attention','degraded','syncing','blocked','setup_required','reauthorization_required','stale','disabled','unsupported','unknown') AND row_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT integration_center_snapshot_account_json_chk CHECK(jsonb_typeof(dimensions)='object' AND jsonb_typeof(capabilities)='array' AND jsonb_typeof(issues)='array' AND jsonb_typeof(actions)='array' AND pg_column_size(dimensions) <= 65536 AND pg_column_size(capabilities) <= 32768 AND pg_column_size(issues) <= 32768 AND pg_column_size(actions) <= 16384)
);
CREATE INDEX integration_center_snapshot_account_status_idx ON integration_center_snapshot_accounts(organization_id,workspace_id,overall,account_id);

CREATE TABLE integration_center_status_transitions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  transition_id text NOT NULL,
  snapshot_id text NOT NULL,
  account_id text NOT NULL,
  dimension text NOT NULL,
  from_status text NOT NULL,
  to_status text NOT NULL,
  overall text NOT NULL,
  reason_code text NOT NULL,
  evidence_digest char(64) NOT NULL,
  observed_at timestamptz NOT NULL,
  correlation_id text NOT NULL DEFAULT '',
  causation_id text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,transition_id),
  FOREIGN KEY (organization_id,workspace_id,snapshot_id) REFERENCES integration_center_snapshots(organization_id,workspace_id,snapshot_id) ON DELETE RESTRICT,
  CONSTRAINT integration_center_transition_ref_chk CHECK(transition_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND dimension ~ '^[a-z][a-z0-9_]{0,63}$' AND reason_code ~ '^[a-z][a-z0-9._-]{0,95}$' AND evidence_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT integration_center_transition_status_chk CHECK(overall IN ('healthy','attention','degraded','syncing','blocked','setup_required','reauthorization_required','stale','disabled','unsupported','unknown') AND from_status <> to_status)
);
CREATE UNIQUE INDEX integration_center_transition_dedupe_idx ON integration_center_status_transitions(organization_id,workspace_id,account_id,dimension,observed_at,evidence_digest);
CREATE INDEX integration_center_transition_account_idx ON integration_center_status_transitions(organization_id,workspace_id,account_id,created_at DESC);

CREATE TABLE integration_center_action_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  receipt_id text NOT NULL,
  action_id text NOT NULL,
  account_id text NOT NULL,
  snapshot_id text NOT NULL,
  idempotency_key text NOT NULL,
  action_kind text NOT NULL,
  result text NOT NULL DEFAULT 'pending',
  expected_version bigint NOT NULL,
  error_code text NOT NULL DEFAULT '',
  correlation_id text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,receipt_id),
  FOREIGN KEY (organization_id,workspace_id,snapshot_id) REFERENCES integration_center_snapshots(organization_id,workspace_id,snapshot_id) ON DELETE RESTRICT,
  CONSTRAINT integration_center_action_ref_chk CHECK(action_id ~ '^[a-z][a-z0-9._-]{0,95}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND action_kind ~ '^[a-z][a-z0-9._-]{0,63}$' AND expected_version >= 1 AND error_code ~ '^[a-z0-9._-]{0,95}$'),
  CONSTRAINT integration_center_action_result_chk CHECK(result IN ('pending','succeeded','failed','conflict','unknown') AND ((result='pending' AND completed_at IS NULL) OR (result<>'pending' AND completed_at IS NOT NULL))),
  CONSTRAINT integration_center_action_time_chk CHECK(completed_at IS NULL OR completed_at >= created_at)
);
CREATE UNIQUE INDEX integration_center_action_idempotency_idx ON integration_center_action_receipts(organization_id,workspace_id,account_id,idempotency_key);

CREATE TABLE integration_center_recompute_queue (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  account_id text NOT NULL,
  reason_code text NOT NULL,
  event_id text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  attempts integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  lease_token text,
  lease_expires_at timestamptz,
  last_error_code text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,account_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT integration_center_queue_ref_chk CHECK(account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND reason_code ~ '^[a-z][a-z0-9._-]{0,95}$' AND event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND status IN ('pending','leased','completed','dead_letter') AND attempts BETWEEN 0 AND 20 AND last_error_code ~ '^[a-z0-9._-]{0,95}$'),
  CONSTRAINT integration_center_queue_lease_chk CHECK((status='leased') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL) AND updated_at >= created_at)
);
CREATE INDEX integration_center_queue_claim_idx ON integration_center_recompute_queue(organization_id,workspace_id,status,available_at,account_id);

CREATE FUNCTION integration_center_immutable() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''integration center evidence is immutable; create a new snapshot''; END';
CREATE TRIGGER integration_center_snapshot_immutable BEFORE UPDATE OR DELETE ON integration_center_snapshots FOR EACH ROW EXECUTE FUNCTION integration_center_immutable();
CREATE TRIGGER integration_center_snapshot_account_immutable BEFORE UPDATE OR DELETE ON integration_center_snapshot_accounts FOR EACH ROW EXECUTE FUNCTION integration_center_immutable();
CREATE TRIGGER integration_center_transition_immutable BEFORE UPDATE OR DELETE ON integration_center_status_transitions FOR EACH ROW EXECUTE FUNCTION integration_center_immutable();
CREATE TRIGGER integration_center_action_immutable BEFORE UPDATE OR DELETE ON integration_center_action_receipts FOR EACH ROW EXECUTE FUNCTION integration_center_immutable();

ALTER TABLE integration_center_snapshots ENABLE ROW LEVEL SECURITY; ALTER TABLE integration_center_snapshots FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_center_snapshot_accounts ENABLE ROW LEVEL SECURITY; ALTER TABLE integration_center_snapshot_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_center_status_transitions ENABLE ROW LEVEL SECURITY; ALTER TABLE integration_center_status_transitions FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_center_action_receipts ENABLE ROW LEVEL SECURITY; ALTER TABLE integration_center_action_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_center_recompute_queue ENABLE ROW LEVEL SECURITY; ALTER TABLE integration_center_recompute_queue FORCE ROW LEVEL SECURITY;
CREATE POLICY integration_center_snapshots_tenant_all ON integration_center_snapshots FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY integration_center_snapshot_accounts_tenant_all ON integration_center_snapshot_accounts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY integration_center_transitions_tenant_all ON integration_center_status_transitions FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY integration_center_actions_tenant_all ON integration_center_action_receipts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY integration_center_queue_tenant_all ON integration_center_recompute_queue FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE, DELETE, TRUNCATE ON integration_center_snapshots, integration_center_snapshot_accounts, integration_center_status_transitions, integration_center_action_receipts FROM PUBLIC;

COMMENT ON TABLE integration_center_snapshots IS 'Task 168 rebuildable tenant-scoped snapshot metadata; source domain tables remain authoritative.';
COMMENT ON TABLE integration_center_action_receipts IS 'Task 168 immutable idempotent operator action receipts; no secret or provider payload.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
