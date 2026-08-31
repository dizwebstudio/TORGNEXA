BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 174 / seller financial analytics. This is a report-evidence layer;
-- settlement, payment, order and inventory ledgers remain authoritative.
CREATE TABLE financial_calculation_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  idempotency_key text NOT NULL,
  from_at timestamptz NOT NULL,
  to_at timestamptz NOT NULL,
  basis text NOT NULL,
  reporting_currency char(3),
  algorithm_version text NOT NULL,
  metric_definition_version text NOT NULL,
  allocation_policy_version text NOT NULL,
  valuation_policy_version text NOT NULL,
  attribution_policy_version text NOT NULL,
  input_digest char(64) NOT NULL,
  status text NOT NULL,
  quality_status text NOT NULL,
  coverage_percent integer NOT NULL DEFAULT 0,
  source_watermarks jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,run_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT financial_runs_ref_chk CHECK (
    run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    input_digest ~ '^[0-9a-f]{64}$' AND from_at < to_at
  ),
  CONSTRAINT financial_runs_basis_chk CHECK (basis IN ('order_accrual','settlement','cash')),
  CONSTRAINT financial_runs_currency_chk CHECK (reporting_currency IS NULL OR reporting_currency ~ '^[A-Z]{3}$'),
  CONSTRAINT financial_runs_version_chk CHECK (
    algorithm_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$' AND metric_definition_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$' AND
    allocation_policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$' AND valuation_policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$' AND
    attribution_policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
  ),
  CONSTRAINT financial_runs_state_chk CHECK (status IN ('queued','running','completed','partial','stale','failed') AND quality_status IN ('complete','partial','stale','unmatched','conflict','mixed_currency','unsupported','missing_cogs','missing_fx','unmatched_settlement','unattributed_advertising','disputed') AND coverage_percent BETWEEN 0 AND 100 AND jsonb_typeof(source_watermarks)='object' AND pg_column_size(source_watermarks)<=65536),
  CONSTRAINT financial_runs_time_chk CHECK (completed_at IS NULL OR completed_at >= created_at)
);
CREATE INDEX financial_runs_latest_idx ON financial_calculation_runs(organization_id,workspace_id,basis,from_at,to_at,completed_at DESC,run_id DESC);

CREATE TABLE financial_calculation_snapshots (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  snapshot_id text NOT NULL,
  snapshot_document jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,snapshot_id),
  UNIQUE (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES financial_calculation_runs(organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT financial_snapshots_ref_chk CHECK (
    snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND jsonb_typeof(snapshot_document)='object' AND pg_column_size(snapshot_document)<=8388608 AND
    snapshot_document::text !~* '(access_token|authorization|private_key|client_secret)'
  )
);
CREATE INDEX financial_snapshots_run_idx ON financial_calculation_snapshots(organization_id,workspace_id,run_id,created_at DESC);

CREATE TABLE financial_calculation_quality_issues (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  issue_id text NOT NULL,
  code text NOT NULL,
  subject_ref text NOT NULL DEFAULT '',
  severity text NOT NULL DEFAULT 'warn',
  explanation text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id,issue_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES financial_calculation_runs(organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT financial_quality_issue_chk CHECK (issue_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND code ~ '^[a-z][a-z0-9._-]{0,63}$' AND (subject_ref='' OR subject_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND severity IN ('info','warn','block') AND char_length(explanation) BETWEEN 1 AND 500)
);

CREATE TABLE financial_calculation_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  event_id text NOT NULL,
  from_status text NOT NULL,
  to_status text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id,event_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES financial_calculation_runs(organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT financial_events_state_chk CHECK (from_status IN ('queued','running','completed','partial','stale','failed') AND to_status IN ('queued','running','completed','partial','stale','failed'))
);

CREATE FUNCTION financial_evidence_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''financial calculation evidence is append-only''; RETURN NULL; END';
CREATE TRIGGER financial_snapshots_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON financial_calculation_snapshots FOR EACH STATEMENT EXECUTE FUNCTION financial_evidence_no_mutation();
CREATE TRIGGER financial_quality_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON financial_calculation_quality_issues FOR EACH STATEMENT EXECUTE FUNCTION financial_evidence_no_mutation();
CREATE TRIGGER financial_events_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON financial_calculation_events FOR EACH STATEMENT EXECUTE FUNCTION financial_evidence_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON financial_calculation_snapshots,financial_calculation_quality_issues,financial_calculation_events FROM PUBLIC;

ALTER TABLE financial_calculation_runs ENABLE ROW LEVEL SECURITY; ALTER TABLE financial_calculation_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_calculation_snapshots ENABLE ROW LEVEL SECURITY; ALTER TABLE financial_calculation_snapshots FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_calculation_quality_issues ENABLE ROW LEVEL SECURITY; ALTER TABLE financial_calculation_quality_issues FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_calculation_events ENABLE ROW LEVEL SECURITY; ALTER TABLE financial_calculation_events FORCE ROW LEVEL SECURITY;
CREATE POLICY financial_runs_tenant_all ON financial_calculation_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_snapshots_tenant_all ON financial_calculation_snapshots FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_quality_tenant_all ON financial_calculation_quality_issues FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_events_tenant_all ON financial_calculation_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE financial_calculation_runs IS 'Immutable seller-finance calculation metadata; reports are snapshots, not live recomputations.';
COMMENT ON TABLE financial_calculation_snapshots IS 'Redacted P&L, cash-flow and detail evidence; raw provider payloads and secrets are forbidden.';
COMMENT ON TABLE financial_calculation_quality_issues IS 'Explainable data-quality evidence for incomplete seller reports.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
