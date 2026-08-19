BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE promotions (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, name text NOT NULL, kind text NOT NULL CHECK(kind IN ('discount','coupon')), starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1), CHECK(ends_at>starts_at), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE promotion_participation (organization_id text NOT NULL, workspace_id text NOT NULL, promotion_id text NOT NULL, sku text NOT NULL, proposed_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), floor_minor bigint NOT NULL, minimum_margin_bps integer NOT NULL CHECK(minimum_margin_bps BETWEEN 0 AND 10000), approval_ref text, created_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,promotion_id,sku), FOREIGN KEY(promotion_id) REFERENCES promotions(id));
ALTER TABLE promotions ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotions FORCE ROW LEVEL SECURITY;
CREATE POLICY promotions_tenant_policy ON promotions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE promotion_participation ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_participation FORCE ROW LEVEL SECURITY;
CREATE POLICY promotion_participation_tenant_policy ON promotion_participation FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
