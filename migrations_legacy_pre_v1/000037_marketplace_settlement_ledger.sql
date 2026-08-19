BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE settlement_entries (organization_id text NOT NULL, workspace_id text NOT NULL, entry_id text NOT NULL, provider text NOT NULL, provider_account_id text NOT NULL, provider_entry_ref text NOT NULL, order_id text NOT NULL DEFAULT '', adjusts_entry_id text NOT NULL DEFAULT '', fee_code text NOT NULL DEFAULT '', fx_rate_ref text NOT NULL DEFAULT '', kind text NOT NULL CHECK(kind IN ('sale','fee','refund','payout','adjustment')), amount_minor bigint NOT NULL, currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), occurred_at timestamptz NOT NULL, imported_at timestamptz NOT NULL, disputed boolean NOT NULL DEFAULT false, PRIMARY KEY(organization_id,workspace_id,entry_id), UNIQUE(organization_id,workspace_id,provider,provider_account_id,provider_entry_ref), CHECK((kind='adjustment' AND adjusts_entry_id<>'') OR (kind<>'adjustment' AND adjusts_entry_id='')), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE FUNCTION settlement_entries_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''settlement entries are append-only; use adjustment entries''; END';
CREATE TRIGGER settlement_entries_append_only_guard BEFORE UPDATE OR DELETE ON settlement_entries FOR EACH ROW EXECUTE FUNCTION settlement_entries_append_only();
ALTER TABLE settlement_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY settlement_entries_tenant_policy ON settlement_entries FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
