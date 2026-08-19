BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE TABLE cloud_plan_versions (plan_id text NOT NULL, version bigint NOT NULL CHECK(version>0), name text NOT NULL, price_minor bigint NOT NULL, currency char(3) NOT NULL, entitlements jsonb NOT NULL, effective_at timestamptz NOT NULL, PRIMARY KEY(plan_id,version));
CREATE TABLE cloud_subscriptions (organization_id text NOT NULL, workspace_id text NOT NULL, subscription_id text NOT NULL, plan_id text NOT NULL, plan_version bigint NOT NULL, state text NOT NULL CHECK(state IN ('trial','active','past_due','grace','suspended','cancelled')), period_start timestamptz NOT NULL, period_end timestamptz NOT NULL, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,subscription_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_usage_records (organization_id text NOT NULL, workspace_id text NOT NULL, usage_id text NOT NULL, meter text NOT NULL, source_event_id text NOT NULL, quantity bigint NOT NULL CHECK(quantity>0), occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,usage_id), UNIQUE(organization_id,workspace_id,source_event_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_invoices (organization_id text NOT NULL, workspace_id text NOT NULL, invoice_id text NOT NULL, subscription_id text NOT NULL, amount_minor bigint NOT NULL, currency char(3) NOT NULL, state text NOT NULL, provider_payment_ref text NOT NULL DEFAULT '', version bigint NOT NULL CHECK(version>0), created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,invoice_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE cloud_invoice_adjustments (organization_id text NOT NULL, workspace_id text NOT NULL, adjustment_id text NOT NULL, invoice_id text NOT NULL, reason text NOT NULL, amount_minor bigint NOT NULL, currency char(3) NOT NULL, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,adjustment_id), FOREIGN KEY(organization_id,workspace_id,invoice_id) REFERENCES cloud_invoices(organization_id,workspace_id,invoice_id));
CREATE FUNCTION cloud_usage_records_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''cloud_usage_records is append-only''; END';
CREATE TRIGGER cloud_usage_records_append_only_guard BEFORE UPDATE OR DELETE ON cloud_usage_records FOR EACH ROW EXECUTE FUNCTION cloud_usage_records_append_only();
CREATE FUNCTION cloud_invoice_adjustments_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''cloud_invoice_adjustments is append-only''; END';
CREATE TRIGGER cloud_invoice_adjustments_append_only_guard BEFORE UPDATE OR DELETE ON cloud_invoice_adjustments FOR EACH ROW EXECUTE FUNCTION cloud_invoice_adjustments_append_only();
ALTER TABLE cloud_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_subscriptions_tenant_policy ON cloud_subscriptions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_usage_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_usage_records FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_usage_records_tenant_policy ON cloud_usage_records FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_invoices FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_invoices_tenant_policy ON cloud_invoices FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE cloud_invoice_adjustments ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_invoice_adjustments FORCE ROW LEVEL SECURITY;
CREATE POLICY cloud_invoice_adjustments_tenant_policy ON cloud_invoice_adjustments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
