BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE wms_tasks (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, task_type text NOT NULL CHECK(task_type IN ('receiving','put_away','pick','pack','cycle_count','transfer','return_receiving')), state text NOT NULL CHECK(state IN ('pending','in_progress','completed','cancelled','exception')), warehouse_id text NOT NULL, sku text NOT NULL, expected_quantity bigint NOT NULL CHECK(expected_quantity>0), processed_quantity bigint NOT NULL CHECK(processed_quantity BETWEEN 0 AND expected_quantity), version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_task_events (organization_id text NOT NULL, workspace_id text NOT NULL, event_id text NOT NULL, task_id text NOT NULL REFERENCES wms_tasks(id), idempotency_key text NOT NULL, kind text NOT NULL, quantity bigint NOT NULL CHECK(quantity>0), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,event_id), UNIQUE(organization_id,workspace_id,idempotency_key));
ALTER TABLE wms_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_tasks_tenant_policy ON wms_tasks FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_task_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_task_events FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_task_events_tenant_policy ON wms_task_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
