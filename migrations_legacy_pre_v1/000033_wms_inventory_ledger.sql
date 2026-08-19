BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE wms_locations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, warehouse_id text NOT NULL, code text NOT NULL, kind text NOT NULL CHECK(kind IN ('storage','picking','quarantine','receiving')), active boolean NOT NULL, UNIQUE(organization_id,workspace_id,warehouse_id,code), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_lots (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, sku text NOT NULL, expires_at timestamptz, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE wms_stock_ledger (organization_id text NOT NULL, workspace_id text NOT NULL, entry_id text NOT NULL, idempotency_key text NOT NULL, sku text NOT NULL, location_id text NOT NULL REFERENCES wms_locations(id), lot_id text REFERENCES wms_lots(id), serial text NOT NULL DEFAULT '', kind text NOT NULL CHECK(kind IN ('receive','move_in','move_out','adjust','quarantine','release','reserve','unreserve','consume')), quantity bigint NOT NULL CHECK(quantity>0), reference text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,entry_id), UNIQUE(organization_id,workspace_id,idempotency_key));
CREATE FUNCTION wms_stock_ledger_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''wms stock ledger is append-only''; END';
CREATE TRIGGER wms_stock_ledger_append_only_guard BEFORE UPDATE OR DELETE ON wms_stock_ledger FOR EACH ROW EXECUTE FUNCTION wms_stock_ledger_append_only();
ALTER TABLE wms_locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_locations FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_locations_tenant_policy ON wms_locations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_lots ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_lots FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_lots_tenant_policy ON wms_lots FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_stock_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_stock_ledger FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_stock_ledger_tenant_policy ON wms_stock_ledger FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
