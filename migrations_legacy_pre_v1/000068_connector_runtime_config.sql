BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Host-owned, non-secret provider configuration used by the production
-- connector runtime. Credentials remain exclusively in SecretProvider.
CREATE TABLE connector_runtime_configs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  config jsonb NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT connector_runtime_configs_account_fk FOREIGN KEY (organization_id, workspace_id, connector_account_id)
    REFERENCES connector_accounts (organization_id, workspace_id, id),
  CONSTRAINT connector_runtime_configs_version_chk CHECK (version >= 1),
  CONSTRAINT connector_runtime_configs_size_chk CHECK (octet_length(config::text) BETWEEN 2 AND 32768),
  -- Defense in depth: secrets/API keys must never be persisted in this table.
  CONSTRAINT connector_runtime_configs_nonsecret_chk CHECK (
    config::text !~* '"(password|secret|token|api[_-]?key|access[_-]?key|consumer[_-]?(key|secret)|private[_-]?key|authorization)"[[:space:]]*:'
  ),
  CONSTRAINT connector_runtime_configs_timestamps_chk CHECK (updated_at >= created_at),
  PRIMARY KEY (organization_id, workspace_id, connector_account_id)
);

ALTER TABLE connector_runtime_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_runtime_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_runtime_configs_tenant_select ON connector_runtime_configs FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_runtime_configs_tenant_insert ON connector_runtime_configs FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_runtime_configs_tenant_update ON connector_runtime_configs FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
REVOKE DELETE, TRUNCATE ON connector_runtime_configs FROM PUBLIC;

CREATE FUNCTION connector_runtime_configs_guard_delete() RETURNS trigger
LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector runtime configs are retained; disable the account instead''; RETURN NULL; END';
CREATE TRIGGER connector_runtime_configs_no_delete BEFORE DELETE ON connector_runtime_configs
FOR EACH ROW EXECUTE FUNCTION connector_runtime_configs_guard_delete();

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
