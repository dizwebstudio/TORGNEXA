BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 167: tenant-scoped channel attribution and immutable unit-economics
-- evidence. Orders, payments and settlement entries remain authoritative.
ALTER TABLE settlement_entries DROP CONSTRAINT IF EXISTS settlement_entries_kind_check;
ALTER TABLE settlement_entries ADD CONSTRAINT settlement_entries_kind_check CHECK(kind IN ('sale','fee','refund','payout','adjustment','logistics','storage','advertising','penalty','compensation','withholding'));
CREATE TABLE unit_economics_channels (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  channel_ref text NOT NULL,
  display_name text NOT NULL,
  family text NOT NULL,
  connector_account_id text NOT NULL DEFAULT '',
  currency_policy text NOT NULL DEFAULT 'original',
  status text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,channel_ref),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT unit_economics_channels_ref_chk CHECK(channel_ref ~ '^[a-z][a-z0-9._:/-]{0,191}$' AND display_name <> '' AND char_length(display_name) <= 160 AND family ~ '^[a-z][a-z0-9._:/-]{0,63}$'),
  CONSTRAINT unit_economics_channels_policy_chk CHECK(currency_policy IN ('original','workspace_reporting_currency','compare_currency')),
  CONSTRAINT unit_economics_channels_status_chk CHECK(status IN ('active','retired') AND version >= 1 AND updated_at >= created_at)
);
CREATE INDEX unit_economics_channels_status_idx ON unit_economics_channels(organization_id,workspace_id,status,channel_ref);

CREATE TABLE unit_economics_order_attributions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  order_id text NOT NULL,
  channel_ref text NOT NULL DEFAULT 'unattributed',
  assignment_state text NOT NULL DEFAULT 'unattributed',
  reason_code text NOT NULL,
  source_ref text NOT NULL DEFAULT '',
  effective_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,order_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT unit_economics_attributions_ref_chk CHECK(order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND channel_ref ~ '^[a-z][a-z0-9._:/-]{0,191}$' AND reason_code ~ '^[a-z][a-z0-9_]{0,63}$' AND (source_ref='' OR source_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')),
  CONSTRAINT unit_economics_attributions_state_chk CHECK(assignment_state IN ('resolved','unattributed','ambiguous','retired') AND version >= 1)
);
CREATE INDEX unit_economics_attributions_channel_idx ON unit_economics_order_attributions(organization_id,workspace_id,channel_ref,assignment_state,order_id);

CREATE TABLE unit_economics_cost_snapshots (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  snapshot_id text NOT NULL,
  order_id text NOT NULL,
  order_item_id text NOT NULL DEFAULT '',
  sku text NOT NULL DEFAULT '',
  cogs_minor_units bigint NOT NULL,
  currency char(3) NOT NULL,
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  cost_as_of timestamptz NOT NULL,
  valuation_policy_version text NOT NULL,
  source_ref text NOT NULL,
  input_digest char(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,snapshot_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT unit_economics_cost_ref_chk CHECK(snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (order_item_id='' OR order_item_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (sku='' OR sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$') AND source_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT unit_economics_cost_amount_chk CHECK(cogs_minor_units >= 0 AND currency ~ '^[A-Z]{3}$' AND quantity_coefficient > 0 AND quantity_scale BETWEEN 0 AND 9),
  CONSTRAINT unit_economics_cost_digest_chk CHECK(input_digest ~ '^[0-9a-f]{64}$' AND valuation_policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$')
);
CREATE INDEX unit_economics_cost_order_idx ON unit_economics_cost_snapshots(organization_id,workspace_id,order_id,order_item_id,cost_as_of);

CREATE TABLE unit_economics_calculation_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  run_key text NOT NULL,
  basis text NOT NULL,
  from_at timestamptz NOT NULL,
  to_at timestamptz NOT NULL,
  reporting_currency char(3),
  algorithm_version text NOT NULL,
  metric_definition_version text NOT NULL,
  allocation_policy_version text NOT NULL,
  valuation_policy_version text NOT NULL,
  attribution_policy_version text NOT NULL,
  input_digest char(64) NOT NULL,
  status text NOT NULL DEFAULT 'preview',
  quality_status text NOT NULL DEFAULT 'partial',
  row_count integer NOT NULL DEFAULT 0,
  source_watermarks jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT unit_economics_runs_ref_chk CHECK(run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND run_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND input_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT unit_economics_runs_basis_chk CHECK(basis IN ('order_accrual','settlement','cash') AND to_at > from_at),
  CONSTRAINT unit_economics_runs_currency_chk CHECK(reporting_currency IS NULL OR reporting_currency ~ '^[A-Z]{3}$'),
  CONSTRAINT unit_economics_runs_state_chk CHECK(status IN ('preview','rebuild','published','superseded','failed') AND quality_status IN ('complete','partial','stale','unmatched','conflict','mixed_currency','unsupported') AND row_count BETWEEN 0 AND 100000 AND jsonb_typeof(source_watermarks)='object' AND pg_column_size(source_watermarks) <= 65536),
  CONSTRAINT unit_economics_runs_time_chk CHECK(published_at IS NULL OR published_at >= created_at)
);
CREATE UNIQUE INDEX unit_economics_runs_key_idx ON unit_economics_calculation_runs(organization_id,workspace_id,run_key);
CREATE INDEX unit_economics_runs_period_idx ON unit_economics_calculation_runs(organization_id,workspace_id,basis,from_at,to_at,status,run_id);

CREATE TABLE unit_economics_quality_issues (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  issue_id text NOT NULL,
  code text NOT NULL,
  subject_ref text NOT NULL DEFAULT '',
  severity text NOT NULL DEFAULT 'warn',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(organization_id,workspace_id,run_id,issue_id),
  FOREIGN KEY(organization_id,workspace_id,run_id) REFERENCES unit_economics_calculation_runs(organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT unit_economics_quality_issue_chk CHECK(issue_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND code ~ '^[a-z][a-z0-9_]{0,63}$' AND (subject_ref='' OR subject_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND severity IN ('warn','block','info'))
);

CREATE FUNCTION unit_economics_immutable() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''unit economics evidence is immutable; create a new version''; END';
CREATE TRIGGER unit_economics_cost_immutable BEFORE UPDATE OR DELETE ON unit_economics_cost_snapshots FOR EACH ROW EXECUTE FUNCTION unit_economics_immutable();
CREATE TRIGGER unit_economics_runs_immutable BEFORE UPDATE OR DELETE ON unit_economics_calculation_runs FOR EACH ROW EXECUTE FUNCTION unit_economics_immutable();
CREATE TRIGGER unit_economics_quality_immutable BEFORE UPDATE OR DELETE ON unit_economics_quality_issues FOR EACH ROW EXECUTE FUNCTION unit_economics_immutable();

ALTER TABLE unit_economics_channels ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_economics_channels FORCE ROW LEVEL SECURITY;
ALTER TABLE unit_economics_order_attributions ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_economics_order_attributions FORCE ROW LEVEL SECURITY;
ALTER TABLE unit_economics_cost_snapshots ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_economics_cost_snapshots FORCE ROW LEVEL SECURITY;
ALTER TABLE unit_economics_calculation_runs ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_economics_calculation_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE unit_economics_quality_issues ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_economics_quality_issues FORCE ROW LEVEL SECURITY;
CREATE POLICY unit_economics_channels_tenant_all ON unit_economics_channels FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY unit_economics_attributions_tenant_all ON unit_economics_order_attributions FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY unit_economics_cost_tenant_all ON unit_economics_cost_snapshots FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY unit_economics_runs_tenant_all ON unit_economics_calculation_runs FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY unit_economics_quality_tenant_all ON unit_economics_quality_issues FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE,DELETE,TRUNCATE ON unit_economics_cost_snapshots,unit_economics_calculation_runs,unit_economics_quality_issues FROM PUBLIC;

COMMENT ON TABLE unit_economics_channels IS 'Provider-neutral tenant-scoped channel dimension; unattributed is explicit, never silently allocated.';
COMMENT ON TABLE unit_economics_order_attributions IS 'Tenant-scoped deterministic order-to-channel resolution evidence.';
COMMENT ON TABLE unit_economics_cost_snapshots IS 'Immutable historical COGS evidence; current catalog cost is never used as an as-of fallback.';
COMMENT ON TABLE unit_economics_calculation_runs IS 'Immutable versioned unit-economics calculation metadata and input digest.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
