BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE logistics_shipments (organization_id text NOT NULL, workspace_id text NOT NULL, shipment_id text NOT NULL, provider_account_id text NOT NULL, external_id text NOT NULL, remote_id text NOT NULL DEFAULT '', service_code text NOT NULL, status text NOT NULL, tracking_number text NOT NULL DEFAULT '', cost_minor_units bigint NOT NULL DEFAULT 0 CHECK(cost_minor_units>=0), currency char(3) NOT NULL, min_delivery_at timestamptz, max_delivery_at timestamptz, version bigint NOT NULL CHECK(version>0), updated_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,shipment_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE logistics_tracking_evidence (evidence_id bigserial PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, shipment_id text NOT NULL, remote_status text NOT NULL, observed_at timestamptz NOT NULL, FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION logistics_tracking_evidence_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''logistics tracking evidence is append-only''; END';
CREATE TRIGGER logistics_tracking_evidence_append_only_guard BEFORE UPDATE OR DELETE ON logistics_tracking_evidence FOR EACH ROW EXECUTE FUNCTION logistics_tracking_evidence_append_only();
ALTER TABLE logistics_shipments ENABLE ROW LEVEL SECURITY;
ALTER TABLE logistics_shipments FORCE ROW LEVEL SECURITY;
CREATE POLICY logistics_shipments_tenant_policy ON logistics_shipments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE logistics_tracking_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE logistics_tracking_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY logistics_tracking_evidence_tenant_policy ON logistics_tracking_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
