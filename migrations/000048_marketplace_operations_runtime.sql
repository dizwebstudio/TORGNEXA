BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Epic 176 / marketplace operations runtime. This is an orchestration
-- projection and command journal. Canonical products, orders, inventory,
-- fulfillment, returns and finance remain owned by their bounded contexts.
CREATE TABLE marketplace_operation_flows (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  flow_id text NOT NULL,
  account_id text NOT NULL,
  stage text NOT NULL,
  state text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  last_operation_id text NOT NULL DEFAULT '',
  last_idempotency_key text NOT NULL DEFAULT '',
  last_reason_code text NOT NULL DEFAULT '',
  last_command_digest text NOT NULL DEFAULT '',
  references_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,flow_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_operation_flow_ref_chk CHECK (
    flow_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    last_operation_id ~ '^[A-Za-z0-9._:/-]{0,191}$' AND
    last_idempotency_key ~ '^[A-Za-z0-9._:/-]{0,191}$' AND
    last_reason_code ~ '^[A-Za-z0-9._:/-]{0,191}$' AND
    last_command_digest ~ '^$|^[0-9a-f]{64}$' AND
    version >= 1 AND jsonb_typeof(references_json)='array' AND pg_column_size(references_json)<=65536
  ),
  CONSTRAINT marketplace_operation_flow_state_chk CHECK (
    stage IN ('account','product','publication','pricing','inventory','order','reservation','pick_pack','shipment','return','settlement','profitability','reconciliation','complete') AND
    state IN ('pending','unknown','blocked','complete') AND
    ((stage='complete' AND state='complete') OR stage<>'complete') AND
    updated_at >= created_at
  )
);
CREATE INDEX marketplace_operation_flows_updated_idx ON marketplace_operation_flows(organization_id,workspace_id,updated_at DESC,flow_id DESC);

CREATE TABLE marketplace_operation_commands (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  flow_id text NOT NULL,
  operation_id text NOT NULL,
  idempotency_key text NOT NULL,
  stage text NOT NULL,
  outcome text NOT NULL,
  reason_code text NOT NULL DEFAULT '',
  references_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,flow_id,operation_id),
  UNIQUE (organization_id,workspace_id,flow_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,flow_id) REFERENCES marketplace_operation_flows(organization_id,workspace_id,flow_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_operation_command_ref_chk CHECK (
    operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    reason_code ~ '^[A-Za-z0-9._:/-]{0,191}$' AND
    jsonb_typeof(references_json)='array' AND pg_column_size(references_json)<=65536
  ),
  CONSTRAINT marketplace_operation_command_state_chk CHECK (
    stage IN ('account','product','publication','pricing','inventory','order','reservation','pick_pack','shipment','return','settlement','profitability','reconciliation','complete') AND
    outcome IN ('succeeded','rejected','unknown')
  )
);
CREATE INDEX marketplace_operation_commands_time_idx ON marketplace_operation_commands(organization_id,workspace_id,flow_id,occurred_at DESC,operation_id DESC);

CREATE FUNCTION marketplace_operation_flow_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.flow_id IS DISTINCT FROM OLD.flow_id OR NEW.account_id IS DISTINCT FROM OLD.account_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace operation flow identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace operation flow version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER marketplace_operation_flow_guard BEFORE UPDATE ON marketplace_operation_flows FOR EACH ROW EXECUTE FUNCTION marketplace_operation_flow_guard();
CREATE FUNCTION marketplace_operation_command_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace operation command journal is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER marketplace_operation_commands_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_operation_commands FOR EACH STATEMENT EXECUTE FUNCTION marketplace_operation_command_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON marketplace_operation_commands FROM PUBLIC;

ALTER TABLE marketplace_operation_flows ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_flows FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY marketplace_operation_flows_tenant_all ON marketplace_operation_flows FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_operation_commands_tenant_all ON marketplace_operation_commands FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE marketplace_operation_flows IS 'Tenant-scoped provider-neutral marketplace workflow projection; canonical domain aggregates remain authoritative.';
COMMENT ON TABLE marketplace_operation_commands IS 'Append-only idempotency journal for marketplace workflow commands; provider payloads and credentials are forbidden.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
