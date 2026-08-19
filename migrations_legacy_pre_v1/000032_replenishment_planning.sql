BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE replenishment_snapshots (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, algorithm_version text NOT NULL, digest char(64) NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'), captured_at timestamptz NOT NULL, inputs jsonb NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE replenishment_recommendations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, snapshot_id text NOT NULL REFERENCES replenishment_snapshots(id), algorithm_version text NOT NULL, sku text NOT NULL, supplier_offer_id text NOT NULL, recommended_units bigint NOT NULL CHECK(recommended_units>=0), explanation text NOT NULL, auto_send_po boolean NOT NULL DEFAULT false CHECK(auto_send_po=false), created_at timestamptz NOT NULL);
ALTER TABLE replenishment_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_snapshots_tenant_policy ON replenishment_snapshots FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_recommendations FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_recommendations_tenant_policy ON replenishment_recommendations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
