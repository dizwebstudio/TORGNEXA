BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 227 is an evidence and completeness layer. Existing orders, payments,
-- settlement entries, FX facts, inventory layers and advertising facts remain
-- authoritative; these tables never become a second money ledger.
CREATE TABLE financial_bank_accounts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  account_id text NOT NULL,
  provider text NOT NULL,
  masked_reference text NOT NULL,
  currency char(3) NOT NULL,
  status text NOT NULL DEFAULT 'active',
  secret_reference text NOT NULL,
  next_cursor text NOT NULL DEFAULT '',
  last_observed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,account_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT financial_bank_account_ref_chk CHECK (
    account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    provider ~ '^[a-z][a-z0-9._-]{0,63}$' AND
    masked_reference ~ '^[A-Za-z0-9][A-Za-z0-9* .:/_-]{0,127}$' AND
    masked_reference !~ '^[0-9]{12,}$' AND
    currency ~ '^[A-Z]{3}$' AND
    secret_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'
  ),
  CONSTRAINT financial_bank_account_state_chk CHECK (status IN ('active','disabled','reauthorization_required','degraded'))
);
CREATE UNIQUE INDEX financial_bank_accounts_masked_uq ON financial_bank_accounts(organization_id,workspace_id,provider,masked_reference);

CREATE TABLE financial_bank_statements (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  statement_id text NOT NULL,
  account_id text NOT NULL,
  period_from timestamptz NOT NULL,
  period_to timestamptz NOT NULL,
  source_reference text NOT NULL,
  source_digest char(64) NOT NULL,
  state text NOT NULL,
  transaction_count integer NOT NULL DEFAULT 0,
  imported_count integer NOT NULL DEFAULT 0,
  rejected_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,statement_id),
  UNIQUE (organization_id,workspace_id,account_id,source_reference),
  FOREIGN KEY (organization_id,workspace_id,account_id) REFERENCES financial_bank_accounts(organization_id,workspace_id,account_id) ON DELETE RESTRICT,
  CONSTRAINT financial_bank_statement_ref_chk CHECK (
    statement_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    source_reference ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    source_digest ~ '^[0-9a-f]{64}$' AND period_from < period_to AND
    state IN ('preview','posted','partial','rejected','unknown') AND
    transaction_count >= 0 AND imported_count >= 0 AND rejected_count >= 0 AND
    imported_count + rejected_count <= transaction_count
  )
);
CREATE INDEX financial_bank_statements_account_idx ON financial_bank_statements(organization_id,workspace_id,account_id,period_to DESC,statement_id DESC);

CREATE TABLE financial_source_records (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  record_id text NOT NULL,
  kind text NOT NULL,
  source_system text NOT NULL,
  account_ref text NOT NULL,
  source_ref text NOT NULL,
  statement_id text,
  order_id text NOT NULL DEFAULT '',
  payout_id text NOT NULL DEFAULT '',
  sku text NOT NULL DEFAULT '',
  campaign_id text NOT NULL DEFAULT '',
  attribution_status text NOT NULL DEFAULT '',
  amount_minor_units bigint NOT NULL,
  currency char(3) NOT NULL,
  state text NOT NULL,
  quality text NOT NULL,
  occurred_at timestamptz NOT NULL,
  posted_at timestamptz,
  source_digest char(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,record_id),
  UNIQUE (organization_id,workspace_id,kind,source_system,account_ref,source_ref),
  FOREIGN KEY (organization_id,workspace_id,statement_id) REFERENCES financial_bank_statements(organization_id,workspace_id,statement_id) ON DELETE RESTRICT,
  CONSTRAINT financial_source_record_ref_chk CHECK (
    record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    kind IN ('order','payment','refund','payout','bank_receipt','cogs','fx','advertising','promotion','settlement','other') AND
    source_system ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    account_ref ~ '^[A-Za-z0-9][A-Za-z0-9* .:/_-]{1,127}$' AND account_ref !~ '^[0-9]{12,}$' AND
    source_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (statement_id IS NULL OR statement_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    char_length(order_id) <= 192 AND char_length(payout_id) <= 192 AND char_length(sku) <= 192 AND char_length(campaign_id) <= 192 AND
    char_length(attribution_status) <= 64 AND currency ~ '^[A-Z]{3}$' AND
    state IN ('pending','posted','reversed','fee','transfer','unknown','matched','unmatched','disputed','observed') AND
    quality IN ('observed','confirmed','estimated','missing','unmatched','stale','disputed','conflict') AND
    source_digest ~ '^[0-9a-f]{64}$'
  )
);
CREATE INDEX financial_source_records_period_idx ON financial_source_records(organization_id,workspace_id,kind,occurred_at,record_id);
CREATE INDEX financial_source_records_order_idx ON financial_source_records(organization_id,workspace_id,order_id,record_id) WHERE order_id <> '';
CREATE INDEX financial_source_records_payout_idx ON financial_source_records(organization_id,workspace_id,payout_id,record_id) WHERE payout_id <> '';

CREATE TABLE financial_completeness_findings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  kind text NOT NULL,
  subject_ref text NOT NULL DEFAULT '',
  expected_minor_units bigint NOT NULL DEFAULT 0,
  observed_minor_units bigint NOT NULL DEFAULT 0,
  currency char(3) NOT NULL,
  severity text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  explanation text NOT NULL,
  owner_ref text NOT NULL DEFAULT '',
  detected_at timestamptz NOT NULL,
  resolved_at timestamptz,
  resolution_digest char(64),
  PRIMARY KEY (organization_id,workspace_id,finding_id),
  CONSTRAINT financial_completeness_finding_chk CHECK (
    finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    kind ~ '^[a-z][a-z0-9._-]{0,63}$' AND char_length(subject_ref) <= 192 AND
    currency ~ '^[A-Z]{3}$' AND severity IN ('info','warn','block') AND
    status IN ('open','acknowledged','resolved') AND char_length(explanation) BETWEEN 1 AND 500 AND
    char_length(owner_ref) <= 192 AND
    (resolution_digest IS NULL OR resolution_digest ~ '^[0-9a-f]{64}$') AND
    (resolved_at IS NULL OR resolved_at >= detected_at)
  )
);
CREATE INDEX financial_completeness_findings_queue_idx ON financial_completeness_findings(organization_id,workspace_id,status,severity,detected_at DESC,finding_id);

CREATE TABLE financial_cogs_backfill_jobs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  job_id text NOT NULL,
  from_at timestamptz NOT NULL,
  to_at timestamptz NOT NULL,
  sku text NOT NULL DEFAULT '',
  warehouse_id text NOT NULL DEFAULT '',
  preview_digest char(64) NOT NULL,
  status text NOT NULL DEFAULT 'preview',
  total_rows integer NOT NULL DEFAULT 0,
  valued_rows integer NOT NULL DEFAULT 0,
  missing_rows integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,job_id),
  CONSTRAINT financial_cogs_backfill_job_chk CHECK (
    job_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND from_at < to_at AND
    preview_digest ~ '^[0-9a-f]{64}$' AND status IN ('preview','queued','running','completed','partial','failed') AND
    total_rows >= 0 AND valued_rows >= 0 AND missing_rows >= 0 AND valued_rows + missing_rows <= total_rows AND
    (completed_at IS NULL OR completed_at >= created_at)
  )
);

CREATE FUNCTION financial_completeness_source_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''financial source evidence is append-only''; RETURN NULL; END';
CREATE TRIGGER financial_bank_statements_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON financial_bank_statements FOR EACH STATEMENT EXECUTE FUNCTION financial_completeness_source_no_mutation();
CREATE TRIGGER financial_source_records_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON financial_source_records FOR EACH STATEMENT EXECUTE FUNCTION financial_completeness_source_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON financial_bank_statements,financial_source_records FROM PUBLIC;

ALTER TABLE financial_bank_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_bank_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_bank_statements ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_bank_statements FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_source_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_source_records FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_completeness_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_completeness_findings FORCE ROW LEVEL SECURITY;
ALTER TABLE financial_cogs_backfill_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE financial_cogs_backfill_jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY financial_bank_accounts_tenant_all ON financial_bank_accounts FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_bank_statements_tenant_all ON financial_bank_statements FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_source_records_tenant_all ON financial_source_records FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_completeness_findings_tenant_all ON financial_completeness_findings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY financial_cogs_backfill_jobs_tenant_all ON financial_cogs_backfill_jobs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE financial_source_records IS 'Redacted append-only bank/payout/COGS/FX/advertising evidence; not a money ledger.';
COMMENT ON TABLE financial_completeness_findings IS 'Tenant-scoped explainable reconciliation exceptions; source facts remain immutable.';
COMMENT ON TABLE financial_cogs_backfill_jobs IS 'Bounded versioned COGS remediation jobs; report snapshots are never rewritten.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
