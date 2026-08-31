BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 164.3/164.6: durable carrier-side operation for one authorized return.
-- The return aggregate remains the source of the customer lifecycle; this
-- table records only the separate remote shipment attempt and its normalized
-- outcome.
CREATE TABLE return_logistics_operations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  return_id text NOT NULL,
  connector_account_id text NOT NULL,
  original_remote_id text NOT NULL,
  external_id text NOT NULL,
  mail_type text NOT NULL,
  status text NOT NULL DEFAULT 'requested',
  remote_id text,
  tracking_number text,
  failure_code text,
  idempotency_key text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT return_logistics_operations_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT return_logistics_operations_idempotency_key UNIQUE(organization_id,workspace_id,idempotency_key),
  CONSTRAINT return_logistics_operations_return_key UNIQUE(organization_id,workspace_id,return_id),
  CONSTRAINT return_logistics_operations_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_logistics_operations_return_fk FOREIGN KEY(organization_id,workspace_id,return_id) REFERENCES commerce_returns(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_logistics_operations_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_logistics_operations_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT return_logistics_operations_ref_chk CHECK(return_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND connector_account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND original_remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND external_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND mail_type ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT return_logistics_operations_status_chk CHECK(status IN ('requested','executing','succeeded','failed','unknown')),
  CONSTRAINT return_logistics_operations_remote_chk CHECK((status = 'succeeded' AND remote_id IS NOT NULL) OR (status <> 'succeeded' AND remote_id IS NULL)),
  CONSTRAINT return_logistics_operations_failure_chk CHECK(failure_code IS NULL OR failure_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT return_logistics_operations_version_chk CHECK(version >= 1),
  CONSTRAINT return_logistics_operations_time_chk CHECK(updated_at >= created_at)
);
CREATE INDEX return_logistics_operations_due_idx ON return_logistics_operations(organization_id,workspace_id,status,updated_at,id);
ALTER TABLE return_logistics_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE return_logistics_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY return_logistics_operations_tenant_all ON return_logistics_operations FOR ALL
  USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
COMMENT ON TABLE return_logistics_operations IS 'Tenant-scoped carrier operation for an authorized return; provider payloads and credentials are not stored.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
