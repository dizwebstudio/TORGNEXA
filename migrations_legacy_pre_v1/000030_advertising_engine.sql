BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE advertising_campaigns (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, name text NOT NULL, status text NOT NULL CHECK(status IN ('draft','active','paused','ended')), daily_budget_minor bigint NOT NULL CHECK(daily_budget_minor>=0), total_budget_minor bigint NOT NULL CHECK(total_budget_minor>=daily_budget_minor), currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), attribution_source text NOT NULL, attribution_medium text NOT NULL, attribution_campaign text NOT NULL, version bigint NOT NULL CHECK(version>=1), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE advertising_actions (organization_id text NOT NULL, workspace_id text NOT NULL, action_id text NOT NULL, campaign_id text NOT NULL, requested_spend_minor bigint NOT NULL CHECK(requested_spend_minor>=0), currency char(3) NOT NULL, dry_run boolean NOT NULL, approval_ref text, executed_at timestamptz, PRIMARY KEY(organization_id,workspace_id,action_id), FOREIGN KEY(campaign_id) REFERENCES advertising_campaigns(id));
ALTER TABLE advertising_campaigns ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_campaigns FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_campaigns_tenant_policy ON advertising_campaigns FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_actions_tenant_policy ON advertising_actions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
