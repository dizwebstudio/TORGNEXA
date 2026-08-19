BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE connector_health_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  sequence_id bigint GENERATED ALWAYS AS IDENTITY,
  status text NOT NULL CHECK (status IN ('healthy','degraded','unavailable')),
  category text NOT NULL CHECK (category IN ('healthy','configuration_error','authentication_error','rate_limited','remote_unavailable','degraded')),
  reason_code text CHECK (reason_code IS NULL OR (reason_code = btrim(reason_code) AND reason_code ~ '^[a-z][a-z0-9_]{0,63}$')),
  rate_limit_remaining bigint CHECK (rate_limit_remaining IS NULL OR rate_limit_remaining >= 0),
  rate_limit_reset_at timestamptz,
  checked_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, workspace_id, connector_account_id, sequence_id),
  FOREIGN KEY (connector_account_id, organization_id, workspace_id)
    REFERENCES connector_accounts (id, organization_id, workspace_id)
);
CREATE INDEX connector_health_history_recent_idx ON connector_health_history
  (organization_id,workspace_id,connector_account_id,checked_at DESC,sequence_id DESC);

ALTER TABLE connector_health_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_health_history FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_health_history_tenant_all ON connector_health_history
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE, DELETE, TRUNCATE ON connector_health_history FROM PUBLIC;

COMMENT ON TABLE connector_health_history IS 'Bounded operational connector-health evidence. Stores normalized codes only; raw provider responses, credentials and PII are forbidden.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
