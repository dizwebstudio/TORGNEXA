BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Epic 176 operations center findings. Findings and operator decisions are
-- immutable projections; retry/reconcile workers consume action intents after
-- rechecking capability, approval and mapping at the protected boundary.
CREATE TABLE marketplace_operation_findings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  flow_id text NOT NULL,
  account_id text NOT NULL,
  stage text NOT NULL,
  kind text NOT NULL,
  entity_kind text NOT NULL,
  entity_id text NOT NULL,
  severity text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  reason_code text NOT NULL,
  expected_value text NOT NULL DEFAULT '',
  observed_value text NOT NULL DEFAULT '',
  evidence_digest text NOT NULL DEFAULT '',
  detected_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,finding_id),
  FOREIGN KEY (organization_id,workspace_id,flow_id) REFERENCES marketplace_operation_flows(organization_id,workspace_id,flow_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_operation_finding_ref_chk CHECK (
    finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    flow_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    entity_kind ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    entity_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    reason_code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    stage IN ('account','product','publication','pricing','inventory','order','reservation','pick_pack','shipment','return','settlement','profitability','reconciliation') AND
    kind IN ('unknown_remote_outcome','stale_data','missing_mapping','duplicate_order','price_stock_mismatch','marketplace_health','dead_letter','partial_response','status_drift') AND
    severity IN ('info','warn','block') AND status='open' AND
    char_length(expected_value) <= 2000 AND char_length(observed_value) <= 2000 AND
    evidence_digest ~ '^$|^[0-9a-f]{64}$'
  )
);
CREATE INDEX marketplace_operation_findings_time_idx ON marketplace_operation_findings(organization_id,workspace_id,detected_at DESC,finding_id DESC);
CREATE INDEX marketplace_operation_findings_flow_idx ON marketplace_operation_findings(organization_id,workspace_id,flow_id,detected_at DESC,finding_id DESC);
CREATE INDEX marketplace_operation_findings_open_idx ON marketplace_operation_findings(organization_id,workspace_id,status,detected_at DESC,finding_id DESC);

CREATE TABLE marketplace_operation_finding_actions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  action_id text NOT NULL,
  action text NOT NULL,
  idempotency_key text NOT NULL,
  actor_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,finding_id,action_id),
  UNIQUE (organization_id,workspace_id,finding_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,finding_id) REFERENCES marketplace_operation_findings(organization_id,workspace_id,finding_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_operation_finding_action_ref_chk CHECK (
    finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    action_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    action IN ('retry','reconcile','resolve')
  )
);
CREATE INDEX marketplace_operation_finding_actions_time_idx ON marketplace_operation_finding_actions(organization_id,workspace_id,finding_id,occurred_at DESC,action_id DESC);

CREATE FUNCTION marketplace_operation_finding_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace operation findings are append-only'';
  RETURN NULL;
END';
CREATE TRIGGER marketplace_operation_findings_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_operation_findings FOR EACH STATEMENT EXECUTE FUNCTION marketplace_operation_finding_no_mutation();
CREATE TRIGGER marketplace_operation_finding_actions_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_operation_finding_actions FOR EACH STATEMENT EXECUTE FUNCTION marketplace_operation_finding_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON marketplace_operation_findings,marketplace_operation_finding_actions FROM PUBLIC;

ALTER TABLE marketplace_operation_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_findings FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_finding_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_operation_finding_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY marketplace_operation_findings_tenant_all ON marketplace_operation_findings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_operation_finding_actions_tenant_all ON marketplace_operation_finding_actions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE marketplace_operation_findings IS 'Tenant-scoped immutable marketplace drift/findings; raw provider payloads and credentials are forbidden.';
COMMENT ON TABLE marketplace_operation_finding_actions IS 'Append-only operator retry/reconcile/resolve intents for marketplace findings.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
