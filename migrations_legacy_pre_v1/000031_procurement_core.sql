BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE procurement_suppliers (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, legal_party_id text NOT NULL, name text NOT NULL, active boolean NOT NULL, version bigint NOT NULL CHECK(version>=1), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE supplier_offers (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, supplier_id text NOT NULL REFERENCES procurement_suppliers(id), sku text NOT NULL, unit_price_minor bigint NOT NULL CHECK(unit_price_minor>=0), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), min_quantity text NOT NULL, lead_time_days integer NOT NULL CHECK(lead_time_days BETWEEN 0 AND 3650), valid_until timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1));
CREATE TABLE purchase_orders (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, supplier_id text NOT NULL REFERENCES procurement_suppliers(id), status text NOT NULL CHECK(status IN ('draft','approved','sent','partially_received','received','cancelled')), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), version bigint NOT NULL CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL CHECK(updated_at>=created_at));
CREATE TABLE purchase_order_lines (organization_id text NOT NULL, workspace_id text NOT NULL, purchase_order_id text NOT NULL REFERENCES purchase_orders(id), line_id text NOT NULL, offer_id text NOT NULL, sku text NOT NULL, quantity text NOT NULL, unit_price_minor bigint NOT NULL CHECK(unit_price_minor>=0), PRIMARY KEY(organization_id,workspace_id,purchase_order_id,line_id));
ALTER TABLE procurement_suppliers ENABLE ROW LEVEL SECURITY;
ALTER TABLE procurement_suppliers FORCE ROW LEVEL SECURITY;
CREATE POLICY procurement_suppliers_tenant_policy ON procurement_suppliers FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE supplier_offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_offers FORCE ROW LEVEL SECURITY;
CREATE POLICY supplier_offers_tenant_policy ON supplier_offers FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_orders_tenant_policy ON purchase_orders FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE purchase_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY purchase_order_lines_tenant_policy ON purchase_order_lines FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
