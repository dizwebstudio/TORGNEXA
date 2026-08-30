BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 165: provider-neutral stock forecasting and replenishment planning.
-- These tables contain derived facts and operator decisions only. Inventory
-- ledger movements and purchase-order submission remain owned by their
-- respective bounded contexts.
CREATE TABLE replenishment_forecast_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  algorithm_version text NOT NULL,
  input_digest varchar(64) NOT NULL,
  horizon_days smallint NOT NULL,
  generated_at timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'running',
  quality_status text NOT NULL DEFAULT 'healthy',
  freshness_seconds bigint NOT NULL DEFAULT 0,
  coverage_bps integer NOT NULL DEFAULT 0,
  sample_count bigint NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT replenishment_forecast_runs_ref_chk CHECK (run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND algorithm_version ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND input_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT replenishment_forecast_runs_range_chk CHECK (horizon_days BETWEEN 1 AND 366 AND freshness_seconds >= 0 AND coverage_bps BETWEEN 0 AND 10000 AND sample_count >= 0 AND version >= 1),
  CONSTRAINT replenishment_forecast_runs_state_chk CHECK (status IN ('running','completed','failed') AND quality_status IN ('healthy','degraded','unavailable')),
  CONSTRAINT replenishment_forecast_runs_time_chk CHECK (valid_until >= generated_at AND updated_at >= created_at)
);

CREATE TABLE replenishment_forecast_points (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  point_id text NOT NULL,
  run_id text NOT NULL,
  offer_id text NOT NULL,
  sku text NOT NULL,
  warehouse_id text NOT NULL,
  sales_channel text NOT NULL DEFAULT '',
  period_start timestamptz NOT NULL,
  period_days smallint NOT NULL,
  unit text NOT NULL,
  demand_p50_coefficient bigint NOT NULL,
  demand_p50_scale smallint NOT NULL,
  demand_p90_coefficient bigint NOT NULL,
  demand_p90_scale smallint NOT NULL,
  confidence_bps integer NOT NULL,
  sample_count bigint NOT NULL,
  explanation text NOT NULL,
  generated_at timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,point_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES replenishment_forecast_runs (organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT replenishment_forecast_points_ref_chk CHECK (point_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (sales_channel = '' OR sales_channel ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT replenishment_forecast_points_range_chk CHECK (period_days BETWEEN 1 AND 366 AND confidence_bps BETWEEN 0 AND 10000 AND sample_count >= 0 AND demand_p50_scale BETWEEN 0 AND 9 AND demand_p90_scale BETWEEN 0 AND 9 AND (demand_p50_coefficient >= 0) AND (demand_p90_coefficient >= 0) AND (demand_p90_coefficient::numeric * power(10::numeric,demand_p50_scale) >= demand_p50_coefficient::numeric * power(10::numeric,demand_p90_scale))),
  CONSTRAINT replenishment_forecast_points_text_chk CHECK (char_length(explanation) BETWEEN 1 AND 192 AND explanation = btrim(explanation)),
  CONSTRAINT replenishment_forecast_points_time_chk CHECK (valid_until >= generated_at)
);

CREATE UNIQUE INDEX replenishment_forecast_points_grain_uq
  ON replenishment_forecast_points (organization_id,workspace_id,run_id,offer_id,sku,warehouse_id,sales_channel,period_start);
CREATE INDEX replenishment_forecast_runs_queue_idx
  ON replenishment_forecast_runs (organization_id,workspace_id,status,generated_at,run_id);

CREATE TABLE replenishment_stock_projections (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  projection_id text NOT NULL,
  run_id text NOT NULL,
  offer_id text NOT NULL,
  sku text NOT NULL,
  warehouse_id text NOT NULL,
  sales_channel text NOT NULL DEFAULT '',
  period_start timestamptz NOT NULL,
  unit text NOT NULL,
  opening_coefficient bigint NOT NULL,
  opening_scale smallint NOT NULL,
  inbound_coefficient bigint NOT NULL,
  inbound_scale smallint NOT NULL,
  demand_coefficient bigint NOT NULL,
  demand_scale smallint NOT NULL,
  projected_coefficient bigint NOT NULL,
  projected_scale smallint NOT NULL,
  shortfall_coefficient bigint NOT NULL,
  shortfall_scale smallint NOT NULL,
  days_of_supply_coefficient bigint NOT NULL DEFAULT 0,
  days_of_supply_scale smallint NOT NULL DEFAULT 0,
  stockout_risk boolean NOT NULL DEFAULT false,
  overstock_risk boolean NOT NULL DEFAULT false,
  explanation text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,projection_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES replenishment_forecast_runs (organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT replenishment_stock_projections_ref_chk CHECK (projection_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (sales_channel = '' OR sales_channel ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT replenishment_stock_projections_range_chk CHECK (opening_scale BETWEEN 0 AND 9 AND inbound_scale BETWEEN 0 AND 9 AND demand_scale BETWEEN 0 AND 9 AND projected_scale BETWEEN 0 AND 9 AND shortfall_scale BETWEEN 0 AND 9 AND days_of_supply_scale BETWEEN 0 AND 9 AND opening_coefficient >= 0 AND inbound_coefficient >= 0 AND demand_coefficient >= 0 AND projected_coefficient >= 0 AND shortfall_coefficient >= 0 AND days_of_supply_coefficient >= 0),
  CONSTRAINT replenishment_stock_projections_text_chk CHECK (char_length(explanation) BETWEEN 1 AND 192 AND explanation = btrim(explanation))
);

CREATE UNIQUE INDEX replenishment_stock_projections_grain_uq
  ON replenishment_stock_projections (organization_id,workspace_id,run_id,offer_id,sku,warehouse_id,sales_channel,period_start);
CREATE INDEX replenishment_stock_projections_risk_idx
  ON replenishment_stock_projections (organization_id,workspace_id,stockout_risk,period_start,projection_id);

CREATE TABLE replenishment_policies (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  offer_id text NOT NULL,
  sku text NOT NULL,
  warehouse_id text NOT NULL,
  sales_channel text NOT NULL DEFAULT '',
  supplier_offer_id text NOT NULL,
  mode text NOT NULL DEFAULT 'recommendation_only',
  target_days smallint NOT NULL,
  review_days smallint NOT NULL,
  unit text NOT NULL,
  safety_stock_coefficient bigint NOT NULL,
  safety_stock_scale smallint NOT NULL,
  moq_coefficient bigint NOT NULL,
  moq_scale smallint NOT NULL,
  case_pack_coefficient bigint NOT NULL,
  case_pack_scale smallint NOT NULL,
  max_order_coefficient bigint NOT NULL,
  max_order_scale smallint NOT NULL,
  budget_minor_units bigint NOT NULL,
  budget_currency char(3) NOT NULL,
  service_level_bps integer NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,policy_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT replenishment_policies_ref_chk CHECK (policy_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (sales_channel = '' OR sales_channel ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND supplier_offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$' AND budget_currency ~ '^[A-Z]{3}$'),
  CONSTRAINT replenishment_policies_mode_chk CHECK (mode IN ('recommendation_only','draft_po','auto_submit')),
  CONSTRAINT replenishment_policies_range_chk CHECK (target_days BETWEEN 1 AND 366 AND review_days BETWEEN 1 AND 90 AND safety_stock_scale BETWEEN 0 AND 9 AND moq_scale BETWEEN 0 AND 9 AND case_pack_scale BETWEEN 0 AND 9 AND max_order_scale BETWEEN 0 AND 9 AND safety_stock_coefficient >= 0 AND moq_coefficient >= 0 AND case_pack_coefficient > 0 AND max_order_coefficient >= moq_coefficient AND service_level_bps BETWEEN 0 AND 10000 AND version >= 1)
);

CREATE UNIQUE INDEX replenishment_policies_grain_uq
  ON replenishment_policies (organization_id,workspace_id,offer_id,sku,warehouse_id,sales_channel);
CREATE INDEX replenishment_policies_enabled_idx
  ON replenishment_policies (organization_id,workspace_id,enabled,updated_at,policy_id);

CREATE TABLE replenishment_recommendation_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  recommendation_id text NOT NULL,
  run_id text NOT NULL,
  input_digest varchar(64) NOT NULL,
  status text NOT NULL,
  reason_code text NOT NULL,
  version bigint NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,event_id),
  CONSTRAINT replenishment_recommendation_history_ref_chk CHECK (event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND recommendation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND input_digest ~ '^[0-9a-f]{64}$' AND reason_code ~ '^[a-z][a-z0-9._-]{0,63}$' AND version >= 1),
  CONSTRAINT replenishment_recommendation_history_status_chk CHECK (status IN ('proposed','accepted','rejected','deferred','on_hold'))
);

CREATE INDEX replenishment_recommendation_history_lookup_idx
  ON replenishment_recommendation_history (organization_id,workspace_id,recommendation_id,occurred_at,event_id);

CREATE TABLE replenishment_purchase_plans (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  plan_id text NOT NULL,
  recommendation_id text NOT NULL,
  supplier_offer_id text NOT NULL,
  mode text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  unit text NOT NULL,
  estimated_cost_minor_units bigint NOT NULL,
  estimated_cost_currency char(3) NOT NULL,
  idempotency_key text NOT NULL,
  approval_required boolean NOT NULL DEFAULT true,
  kill_switch_active boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,plan_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  CONSTRAINT replenishment_purchase_plans_ref_chk CHECK (plan_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND recommendation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND supplier_offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$' AND estimated_cost_currency ~ '^[A-Z]{3}$'),
  CONSTRAINT replenishment_purchase_plans_mode_chk CHECK (mode IN ('recommendation_only','draft_po','auto_submit') AND status IN ('draft','approved','submitted','unknown')),
  CONSTRAINT replenishment_purchase_plans_range_chk CHECK (quantity_coefficient >= 0 AND quantity_scale BETWEEN 0 AND 9 AND estimated_cost_minor_units >= 0 AND version >= 1 AND (mode <> 'auto_submit' OR kill_switch_active = false)),
  CONSTRAINT replenishment_purchase_plans_time_chk CHECK (updated_at >= created_at)
);

CREATE INDEX replenishment_purchase_plans_queue_idx
  ON replenishment_purchase_plans (organization_id,workspace_id,status,updated_at,plan_id);

-- Recommendation history is append-only at the application boundary.
CREATE FUNCTION replenishment_recommendation_history_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''replenishment recommendation history is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER replenishment_recommendation_history_no_update_delete
  BEFORE UPDATE OR DELETE OR TRUNCATE ON replenishment_recommendation_history
  FOR EACH STATEMENT EXECUTE FUNCTION replenishment_recommendation_history_no_mutation();
REVOKE DELETE,TRUNCATE ON replenishment_recommendation_history FROM PUBLIC;

ALTER TABLE replenishment_forecast_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_forecast_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_forecast_runs_tenant_all ON replenishment_forecast_runs FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_forecast_points ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_forecast_points FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_forecast_points_tenant_all ON replenishment_forecast_points FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_stock_projections ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_stock_projections FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_stock_projections_tenant_all ON replenishment_stock_projections FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_policies_tenant_all ON replenishment_policies FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_recommendation_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_recommendation_history FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_recommendation_history_tenant_all ON replenishment_recommendation_history FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE replenishment_purchase_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_purchase_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_purchase_plans_tenant_all ON replenishment_purchase_plans FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE replenishment_forecast_runs IS 'Tenant-scoped immutable metadata for deterministic inventory forecast runs.';
COMMENT ON TABLE replenishment_forecast_points IS 'Tenant-scoped forecast points with exact fixed-point demand quantities.';
COMMENT ON TABLE replenishment_stock_projections IS 'Tenant-scoped stock projections; shortfall is retained as an explicit derived fact.';
COMMENT ON TABLE replenishment_policies IS 'Tenant-scoped reorder policy; auto-submit requires separate approved runtime capability.';
COMMENT ON TABLE replenishment_recommendation_history IS 'Append-only tenant-scoped recommendation status history.';
COMMENT ON TABLE replenishment_purchase_plans IS 'Tenant-scoped draft purchase plans; this table never authorizes remote submission.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
