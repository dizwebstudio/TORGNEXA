BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE demo_dataset_tombstones (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  deleted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id, id) ON DELETE RESTRICT
);
ALTER TABLE demo_dataset_tombstones ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_dataset_tombstones FORCE ROW LEVEL SECURITY;
CREATE POLICY demo_dataset_tombstones_tenant_select ON demo_dataset_tombstones FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_insert ON demo_dataset_tombstones FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_update ON demo_dataset_tombstones FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_delete ON demo_dataset_tombstones FOR DELETE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
COMMENT ON TABLE demo_dataset_tombstones IS 'Tenant-scoped logical removal of synthetic demo data; immutable order history is retained.';
INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
