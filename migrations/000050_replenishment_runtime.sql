BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 165 runtime projection. Forecast runs/points remain derived facts;
-- recommendations are immutable decision candidates and never submit a PO.
CREATE TABLE replenishment_recommendations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recommendation_id text NOT NULL,
  run_id text NOT NULL,
  input_digest varchar(64) NOT NULL,
  offer_id text NOT NULL,
  sku text NOT NULL,
  warehouse_id text NOT NULL,
  sales_channel text NOT NULL DEFAULT '',
  supplier_offer_id text NOT NULL,
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  unit text NOT NULL,
  expected_receipt_days smallint NOT NULL,
  risk_reduction_bps integer NOT NULL,
  reason_codes jsonb NOT NULL,
  eligible_mode text NOT NULL,
  status text NOT NULL DEFAULT 'proposed',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, recommendation_id),
  FOREIGN KEY (organization_id, workspace_id, run_id) REFERENCES replenishment_forecast_runs (organization_id, workspace_id, run_id) ON DELETE RESTRICT,
  CONSTRAINT replenishment_recommendations_ref_chk CHECK (
    recommendation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    input_digest ~ '^[0-9a-f]{64}$' AND
    offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (sales_channel = '' OR sales_channel ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    supplier_offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'
  ),
  CONSTRAINT replenishment_recommendations_range_chk CHECK (
    quantity_coefficient >= 0 AND quantity_scale BETWEEN 0 AND 9 AND
    expected_receipt_days BETWEEN 1 AND 366 AND risk_reduction_bps BETWEEN 0 AND 10000 AND version >= 1
  ),
  CONSTRAINT replenishment_recommendations_state_chk CHECK (eligible_mode IN ('recommendation_only','draft_po','auto_submit') AND status IN ('proposed','accepted','rejected','deferred','on_hold'))
);
CREATE INDEX replenishment_recommendations_lookup_idx
  ON replenishment_recommendations (organization_id, workspace_id, status, created_at DESC, recommendation_id DESC);
CREATE INDEX replenishment_recommendations_grain_idx
  ON replenishment_recommendations (organization_id, workspace_id, warehouse_id, sku, created_at DESC, recommendation_id DESC);

ALTER TABLE replenishment_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenishment_recommendations FORCE ROW LEVEL SECURITY;
CREATE POLICY replenishment_recommendations_tenant_all ON replenishment_recommendations FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE, DELETE, TRUNCATE ON replenishment_recommendations FROM PUBLIC;
COMMENT ON TABLE replenishment_recommendations IS 'Tenant-scoped immutable derived replenishment decisions; does not submit purchase orders.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
