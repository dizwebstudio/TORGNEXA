BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE settlement_reconciliation_runs (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, generated_at timestamptz NOT NULL, timing_window_seconds bigint NOT NULL CHECK(timing_window_seconds>=0), status text NOT NULL CHECK(status IN ('running','completed','failed')), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE settlement_reconciliation_differences (organization_id text NOT NULL, workspace_id text NOT NULL, run_id text NOT NULL REFERENCES settlement_reconciliation_runs(id), difference_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('timing','known_fee','unmatched','duplicate','disputed')), reference text NOT NULL, order_id text NOT NULL DEFAULT '', detail text NOT NULL, PRIMARY KEY(organization_id,workspace_id,run_id,difference_id));
ALTER TABLE settlement_reconciliation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_reconciliation_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_reconciliation_runs_tenant_policy ON settlement_reconciliation_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settlement_reconciliation_differences ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_reconciliation_differences FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_reconciliation_differences_tenant_policy ON settlement_reconciliation_differences FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
