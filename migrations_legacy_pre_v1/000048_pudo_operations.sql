BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pickup_points (organization_id text NOT NULL, workspace_id text NOT NULL, point_id text NOT NULL, external_ref text NOT NULL DEFAULT '', name text NOT NULL, kind text NOT NULL CHECK(kind IN ('own','external')), capacity bigint NOT NULL CHECK(capacity>0), active boolean NOT NULL, updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,point_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE pickup_orders (organization_id text NOT NULL, workspace_id text NOT NULL, order_id text NOT NULL, point_id text NOT NULL, external_order_ref text NOT NULL, state text NOT NULL CHECK(state IN ('created','arrived','ready','issued','expired','return_pending','returned')), payment_ref text NOT NULL DEFAULT '', fiscal_ref text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,order_id), FOREIGN KEY(organization_id,workspace_id,point_id) REFERENCES pickup_points(organization_id,workspace_id,point_id));
CREATE TABLE pickup_order_events (event_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, order_id text NOT NULL, state text NOT NULL, occurred_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION pickup_order_events_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''pickup order events are append-only''; END';
CREATE TRIGGER pickup_order_events_append_only_guard BEFORE UPDATE OR DELETE ON pickup_order_events FOR EACH ROW EXECUTE FUNCTION pickup_order_events_append_only();
ALTER TABLE pickup_points ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_points FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_points_tenant_policy ON pickup_points FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE pickup_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_orders_tenant_policy ON pickup_orders FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE pickup_order_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE pickup_order_events FORCE ROW LEVEL SECURITY;
CREATE POLICY pickup_order_events_tenant_policy ON pickup_order_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
