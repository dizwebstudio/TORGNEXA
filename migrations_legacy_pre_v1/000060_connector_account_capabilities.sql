BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 107 stores immutable, complete capability snapshots. No row for an
-- account means default deny; a later revision never rewrites prior evidence.
CREATE TABLE connector_account_capability_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL,
  capability text NOT NULL,
  direction text NOT NULL,
  risk_class text NOT NULL,
  approval_required boolean NOT NULL,
  enabled boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT connector_account_capability_history_pkey PRIMARY KEY (
    organization_id, workspace_id, connector_account_id, account_version, capability
  ),
  CONSTRAINT connector_account_capability_history_account_fk FOREIGN KEY (
    connector_account_id, organization_id, workspace_id
  ) REFERENCES connector_accounts (id, organization_id, workspace_id),
  CONSTRAINT connector_account_capability_history_version_chk CHECK (account_version >= 2),
  CONSTRAINT connector_account_capability_history_name_chk CHECK (
    capability ~ '^[a-z][a-z0-9._-]{0,127}$'
  ),
  CONSTRAINT connector_account_capability_history_policy_chk CHECK (
    (direction = 'read' AND risk_class = 'read' AND approval_required = false)
    OR
    (direction = 'write' AND risk_class = 'write_sensitive' AND approval_required = true)
  )
);

CREATE INDEX connector_account_capability_history_current_idx
  ON connector_account_capability_history (
    organization_id, workspace_id, connector_account_id, account_version DESC, capability
  );

ALTER TABLE connector_account_capability_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_account_capability_history FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_account_capability_history_tenant_isolation
  ON connector_account_capability_history
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

REVOKE UPDATE, DELETE, TRUNCATE ON connector_account_capability_history FROM PUBLIC;

CREATE FUNCTION connector_account_capabilities_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account capability history is append-only'';
  RETURN NULL;
END';

CREATE TRIGGER connector_account_capability_history_no_update
  BEFORE UPDATE ON connector_account_capability_history
  FOR EACH ROW EXECUTE FUNCTION connector_account_capabilities_reject_mutation();
CREATE TRIGGER connector_account_capability_history_no_delete
  BEFORE DELETE ON connector_account_capability_history
  FOR EACH ROW EXECUTE FUNCTION connector_account_capabilities_reject_mutation();
CREATE TRIGGER connector_account_capability_history_no_clear
  BEFORE TRUNCATE ON connector_account_capability_history
  FOR EACH STATEMENT EXECUTE FUNCTION connector_account_capabilities_reject_mutation();

COMMENT ON TABLE connector_account_capability_history IS
  'Append-only tenant-scoped account capability snapshots; absence means default deny.';
COMMENT ON COLUMN connector_account_capability_history.approval_required IS
  'Host policy classification. Every remote write requires approval before execution.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
