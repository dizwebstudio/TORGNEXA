BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 175 / marketplace advertising runtime. Only normalized facts and
-- bounded evidence are stored. Raw provider responses and credentials never
-- cross the connector boundary.
CREATE TABLE marketplace_advertising_campaigns (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  campaign_id text NOT NULL,
  account_id text NOT NULL,
  channel text NOT NULL,
  remote_id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL,
  currency char(3) NOT NULL,
  daily_budget_minor bigint NOT NULL DEFAULT 0,
  total_budget_minor bigint NOT NULL DEFAULT 0,
  observed_at timestamptz NOT NULL,
  effective_from timestamptz,
  effective_to timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,campaign_id),
  UNIQUE (organization_id,workspace_id,account_id,remote_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT advertising_campaign_ref_chk CHECK (campaign_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND channel ~ '^[a-z][a-z0-9._-]{0,63}$' AND remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT advertising_campaign_value_chk CHECK (char_length(name) BETWEEN 1 AND 300 AND status IN ('draft','active','paused','stopped','archived','unknown') AND currency ~ '^[A-Z]{3}$' AND daily_budget_minor >= 0 AND total_budget_minor >= daily_budget_minor AND version >= 1 AND (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from))
);
CREATE INDEX marketplace_advertising_campaigns_channel_idx ON marketplace_advertising_campaigns(organization_id,workspace_id,channel,status,updated_at DESC,campaign_id DESC);

CREATE TABLE advertising_ad_groups (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  ad_group_id text NOT NULL,
  campaign_id text NOT NULL,
  remote_id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,ad_group_id),
  FOREIGN KEY (organization_id,workspace_id,campaign_id) REFERENCES marketplace_advertising_campaigns(organization_id,workspace_id,campaign_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_ad_group_ref_chk CHECK (ad_group_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(name) BETWEEN 1 AND 300 AND status IN ('draft','active','paused','stopped','archived','unknown'))
);

CREATE TABLE advertising_ads (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  ad_id text NOT NULL,
  ad_group_id text NOT NULL,
  remote_id text NOT NULL,
  name text NOT NULL,
  status text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,ad_id),
  FOREIGN KEY (organization_id,workspace_id,ad_group_id) REFERENCES advertising_ad_groups(organization_id,workspace_id,ad_group_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_ad_ref_chk CHECK (ad_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(name) BETWEEN 1 AND 300 AND status IN ('draft','active','paused','stopped','archived','unknown'))
);

CREATE TABLE advertising_campaign_products (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  link_id text NOT NULL,
  campaign_id text NOT NULL,
  sku text NOT NULL,
  remote_product_id text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,link_id),
  UNIQUE (organization_id,workspace_id,campaign_id,sku,remote_product_id),
  FOREIGN KEY (organization_id,workspace_id,campaign_id) REFERENCES marketplace_advertising_campaigns(organization_id,workspace_id,campaign_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_campaign_product_chk CHECK (link_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(sku) BETWEEN 1 AND 200 AND remote_product_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
);

CREATE TABLE advertising_spend_facts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  fact_id text NOT NULL,
  account_id text NOT NULL,
  channel text NOT NULL,
  campaign_id text NOT NULL,
  ad_id text NOT NULL DEFAULT '',
  sku text NOT NULL DEFAULT '',
  remote_fact_id text NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  amount_minor bigint NOT NULL,
  currency char(3) NOT NULL,
  source text NOT NULL,
  observed_at timestamptz NOT NULL,
  effective_at timestamptz NOT NULL,
  quality text NOT NULL,
  fingerprint char(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,fact_id),
  UNIQUE (organization_id,workspace_id,account_id,remote_fact_id,period_start,period_end),
  FOREIGN KEY (organization_id,workspace_id,campaign_id) REFERENCES marketplace_advertising_campaigns(organization_id,workspace_id,campaign_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_spend_fact_chk CHECK (fact_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND channel ~ '^[a-z][a-z0-9._-]{0,63}$' AND remote_fact_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND period_end > period_start AND amount_minor >= 0 AND currency ~ '^[A-Z]{3}$' AND source ~ '^[a-z][a-z0-9._-]{0,63}$' AND quality IN ('observed','confirmed','estimated','partial','delayed','unknown','conflict') AND fingerprint ~ '^[0-9a-f]{64}$' AND char_length(ad_id) <= 192 AND char_length(sku) <= 200)
);
CREATE INDEX advertising_spend_facts_period_idx ON advertising_spend_facts(organization_id,workspace_id,period_start,period_end,channel,campaign_id);

CREATE TABLE advertising_performance_facts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  fact_id text NOT NULL,
  account_id text NOT NULL,
  channel text NOT NULL,
  campaign_id text NOT NULL,
  ad_id text NOT NULL DEFAULT '',
  sku text NOT NULL DEFAULT '',
  remote_fact_id text NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  impressions bigint NOT NULL DEFAULT 0,
  clicks bigint NOT NULL DEFAULT 0,
  orders bigint NOT NULL DEFAULT 0,
  revenue_minor bigint NOT NULL DEFAULT 0,
  currency char(3) NOT NULL,
  source text NOT NULL,
  observed_at timestamptz NOT NULL,
  effective_at timestamptz NOT NULL,
  quality text NOT NULL,
  fingerprint char(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,fact_id),
  UNIQUE (organization_id,workspace_id,account_id,remote_fact_id,period_start,period_end),
  FOREIGN KEY (organization_id,workspace_id,campaign_id) REFERENCES marketplace_advertising_campaigns(organization_id,workspace_id,campaign_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_performance_fact_chk CHECK (fact_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND channel ~ '^[a-z][a-z0-9._-]{0,63}$' AND remote_fact_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND period_end > period_start AND impressions >= 0 AND clicks >= 0 AND orders >= 0 AND revenue_minor >= 0 AND currency ~ '^[A-Z]{3}$' AND source ~ '^[a-z][a-z0-9._-]{0,63}$' AND quality IN ('observed','confirmed','estimated','partial','delayed','unknown','conflict') AND fingerprint ~ '^[0-9a-f]{64}$' AND char_length(ad_id) <= 192 AND char_length(sku) <= 200)
);
CREATE INDEX advertising_performance_facts_period_idx ON advertising_performance_facts(organization_id,workspace_id,period_start,period_end,channel,campaign_id);

CREATE TABLE advertising_attributions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  attribution_id text NOT NULL,
  campaign_id text NOT NULL,
  sku text NOT NULL DEFAULT '',
  order_id text NOT NULL DEFAULT '',
  orders bigint NOT NULL DEFAULT 0,
  revenue_minor bigint NOT NULL DEFAULT 0,
  currency char(3) NOT NULL,
  source text NOT NULL,
  confidence text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,attribution_id),
  FOREIGN KEY (organization_id,workspace_id,campaign_id) REFERENCES marketplace_advertising_campaigns(organization_id,workspace_id,campaign_id) ON DELETE RESTRICT,
  CONSTRAINT advertising_attribution_chk CHECK (attribution_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND orders >= 0 AND revenue_minor >= 0 AND currency ~ '^[A-Z]{3}$' AND source ~ '^[a-z][a-z0-9._-]{0,63}$' AND confidence IN ('observed','confirmed','estimated','partial','delayed','unknown','conflict'))
);

CREATE TABLE advertising_sync_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  account_id text NOT NULL,
  channel text NOT NULL,
  from_at timestamptz NOT NULL,
  to_at timestamptz NOT NULL,
  mode text NOT NULL,
  status text NOT NULL,
  next_cursor text NOT NULL DEFAULT '',
  watermark_at timestamptz,
  fetched_count integer NOT NULL DEFAULT 0,
  accepted_count integer NOT NULL DEFAULT 0,
  rejected_count integer NOT NULL DEFAULT 0,
  error_code text NOT NULL DEFAULT '',
  evidence_digest char(64),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,run_id),
  UNIQUE (organization_id,workspace_id,account_id,channel,from_at,to_at,mode),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT advertising_sync_run_chk CHECK (run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND channel ~ '^[a-z][a-z0-9._-]{0,63}$' AND from_at < to_at AND mode IN ('daily','incremental','backfill') AND status IN ('queued','running','completed','partial','failed','dead_letter') AND fetched_count >= 0 AND accepted_count >= 0 AND rejected_count >= 0 AND (evidence_digest IS NULL OR evidence_digest ~ '^[0-9a-f]{64}$') AND (completed_at IS NULL OR completed_at >= created_at))
);

CREATE TABLE advertising_reconciliation_findings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  kind text NOT NULL,
  campaign_id text NOT NULL DEFAULT '',
  remote_reference text NOT NULL DEFAULT '',
  expected_minor bigint NOT NULL DEFAULT 0,
  actual_minor bigint NOT NULL DEFAULT 0,
  severity text NOT NULL,
  explanation text NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,finding_id),
  CONSTRAINT advertising_finding_chk CHECK (finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND kind ~ '^[a-z][a-z0-9._-]{0,63}$' AND severity IN ('info','warn','block') AND char_length(explanation) BETWEEN 1 AND 500)
);

CREATE FUNCTION advertising_evidence_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''advertising evidence is append-only''; RETURN NULL; END';
CREATE TRIGGER advertising_spend_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON advertising_spend_facts FOR EACH STATEMENT EXECUTE FUNCTION advertising_evidence_no_mutation();
CREATE TRIGGER advertising_performance_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON advertising_performance_facts FOR EACH STATEMENT EXECUTE FUNCTION advertising_evidence_no_mutation();
CREATE TRIGGER advertising_attribution_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON advertising_attributions FOR EACH STATEMENT EXECUTE FUNCTION advertising_evidence_no_mutation();
CREATE TRIGGER advertising_findings_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON advertising_reconciliation_findings FOR EACH STATEMENT EXECUTE FUNCTION advertising_evidence_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON advertising_spend_facts,advertising_performance_facts,advertising_attributions,advertising_reconciliation_findings FROM PUBLIC;

ALTER TABLE marketplace_advertising_campaigns ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_advertising_campaigns FORCE ROW LEVEL SECURITY;
CREATE POLICY marketplace_advertising_campaigns_tenant_all ON marketplace_advertising_campaigns FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_ad_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_ad_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_ad_groups_tenant_all ON advertising_ad_groups FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_ads ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_ads FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_ads_tenant_all ON advertising_ads FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_campaign_products ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_campaign_products FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_campaign_products_tenant_all ON advertising_campaign_products FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_spend_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_spend_facts FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_spend_facts_tenant_all ON advertising_spend_facts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_performance_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_performance_facts FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_performance_facts_tenant_all ON advertising_performance_facts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_attributions ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_attributions FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_attributions_tenant_all ON advertising_attributions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_sync_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_sync_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_sync_runs_tenant_all ON advertising_sync_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE advertising_reconciliation_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE advertising_reconciliation_findings FORCE ROW LEVEL SECURITY;
CREATE POLICY advertising_reconciliation_findings_tenant_all ON advertising_reconciliation_findings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE advertising_spend_facts IS 'Normalized immutable advertising spend facts; raw provider payloads and secrets are forbidden.';
COMMENT ON TABLE advertising_performance_facts IS 'Normalized immutable advertising delivery/conversion facts.';
COMMENT ON TABLE advertising_sync_runs IS 'Tenant-scoped watermarks and bounded sync evidence for marketplace advertising.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
