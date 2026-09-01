BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 225 / Epic 180. The write-side stores sanitized preview evidence and
-- approved intents. A connector worker may advance an intent only after a
-- capability-level credentialed qualification; these tables never store
-- credentials or raw marketplace responses.
CREATE TABLE marketplace_growth_rules (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  rule_id text NOT NULL,
  channel_id text NOT NULL,
  version bigint NOT NULL,
  rule_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,rule_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_growth_rule_ref_chk CHECK (
    rule_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    channel_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND version >= 1 AND
    jsonb_typeof(rule_document) = 'object' AND pg_column_size(rule_document) <= 1048576
  )
);
CREATE INDEX marketplace_growth_rules_channel_idx ON marketplace_growth_rules(organization_id,workspace_id,channel_id,version DESC,rule_id DESC);

CREATE TABLE marketplace_growth_previews (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  preview_id text NOT NULL,
  operation text NOT NULL,
  channel_id text NOT NULL,
  account_id text NOT NULL,
  target_id text NOT NULL,
  input_digest char(64) NOT NULL,
  rule_version bigint NOT NULL,
  state text NOT NULL,
  affected_count integer NOT NULL,
  eligible_count integer NOT NULL,
  blocked_count integer NOT NULL,
  preview_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,preview_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_growth_preview_ref_chk CHECK (
    preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    operation ~ '^[a-z][a-z0-9._-]{1,63}$' AND
    channel_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    input_digest ~ '^[0-9a-f]{64}$' AND rule_version >= 1 AND
    state IN ('ready','approval_required','blocked') AND
    affected_count BETWEEN 1 AND 1000 AND eligible_count BETWEEN 0 AND affected_count AND
    blocked_count = affected_count - eligible_count AND
    jsonb_typeof(preview_document) = 'object' AND pg_column_size(preview_document) <= 16777216
  )
);
CREATE INDEX marketplace_growth_previews_created_idx ON marketplace_growth_previews(organization_id,workspace_id,created_at DESC,preview_id DESC);

CREATE TABLE marketplace_growth_operations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_id text NOT NULL,
  preview_id text NOT NULL,
  idempotency_key text NOT NULL,
  approval_request_id text NOT NULL,
  operation text NOT NULL,
  capability text NOT NULL,
  channel_id text NOT NULL,
  account_id text NOT NULL,
  target_id text NOT NULL,
  input_digest char(64) NOT NULL,
  state text NOT NULL,
  remote_write_qualified boolean NOT NULL DEFAULT false,
  remote_operation_id text,
  error_code text,
  operation_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,operation_id),
  FOREIGN KEY (organization_id,workspace_id,preview_id) REFERENCES marketplace_growth_previews(organization_id,workspace_id,preview_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_growth_operation_ref_chk CHECK (
    operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    approval_request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    operation ~ '^[a-z][a-z0-9._-]{1,63}$' AND capability IN ('ads.manage','promotions.manage') AND
    channel_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    input_digest ~ '^[0-9a-f]{64}$' AND
    state IN ('accepted','applied','rejected','conflict','rate_limited','unknown','manual_attention','qualification_required') AND
    (remote_operation_id IS NULL OR length(remote_operation_id) <= 192) AND
    (error_code IS NULL OR length(error_code) <= 128) AND
    jsonb_typeof(operation_document) = 'object' AND pg_column_size(operation_document) <= 1048576 AND
    created_at <= updated_at
  ),
  CONSTRAINT marketplace_growth_operation_idempotency_uq UNIQUE (organization_id,workspace_id,idempotency_key)
);
CREATE INDEX marketplace_growth_operations_updated_idx ON marketplace_growth_operations(organization_id,workspace_id,updated_at DESC,operation_id DESC);

CREATE TABLE marketplace_growth_drifts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  drift_id text NOT NULL,
  operation_id text NOT NULL,
  kind text NOT NULL,
  expected_value text NOT NULL,
  actual_value text NOT NULL,
  severity text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,drift_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_growth_drift_ref_chk CHECK (
    drift_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$' AND
    kind ~ '^[a-z][a-z0-9._-]{1,63}$' AND length(expected_value) <= 192 AND length(actual_value) <= 192 AND
    severity IN ('low','medium','high','critical')
  )
);
CREATE INDEX marketplace_growth_drifts_observed_idx ON marketplace_growth_drifts(organization_id,workspace_id,observed_at DESC,drift_id DESC);

CREATE TABLE marketplace_growth_controls (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  kill_switch_enabled boolean NOT NULL DEFAULT false,
  reason text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_growth_control_reason_chk CHECK (length(reason) <= 500)
);

CREATE FUNCTION marketplace_growth_immutable() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace growth evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER marketplace_growth_rules_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_growth_rules FOR EACH STATEMENT EXECUTE FUNCTION marketplace_growth_immutable();
CREATE TRIGGER marketplace_growth_previews_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_growth_previews FOR EACH STATEMENT EXECUTE FUNCTION marketplace_growth_immutable();
CREATE TRIGGER marketplace_growth_drifts_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_growth_drifts FOR EACH STATEMENT EXECUTE FUNCTION marketplace_growth_immutable();
REVOKE UPDATE,DELETE,TRUNCATE ON marketplace_growth_rules,marketplace_growth_previews,marketplace_growth_drifts FROM PUBLIC;

ALTER TABLE marketplace_growth_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_rules FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_operations FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_drifts ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_drifts FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_controls ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_growth_controls FORCE ROW LEVEL SECURITY;
CREATE POLICY marketplace_growth_previews_tenant_all ON marketplace_growth_previews FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_growth_rules_tenant_all ON marketplace_growth_rules FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_growth_operations_tenant_all ON marketplace_growth_operations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_growth_drifts_tenant_all ON marketplace_growth_drifts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_growth_controls_tenant_all ON marketplace_growth_controls FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE marketplace_growth_previews IS 'Tenant-scoped immutable pricing, promotion and advertising qualification previews without raw remote payloads.';
COMMENT ON TABLE marketplace_growth_rules IS 'Tenant-scoped immutable promotion rules and versioned eligibility/calendar inputs.';
COMMENT ON TABLE marketplace_growth_operations IS 'Tenant-scoped approved growth intents; qualification_required is explicit until a connector writer is admitted.';
COMMENT ON TABLE marketplace_growth_drifts IS 'Append-only sanitized read-after-write and reconciliation findings for promotions and advertising.';
COMMENT ON TABLE marketplace_growth_controls IS 'Tenant-scoped kill switch for promotion and advertising write workers.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
