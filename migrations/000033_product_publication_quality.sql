BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 166: derived publication-quality evidence. Source-of-truth ownership
-- remains with catalog/PIM, price, inventory, media security and compliance.
CREATE TABLE publication_quality_profiles (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  profile_id text NOT NULL,
  profile_version bigint NOT NULL,
  connector_id text NOT NULL,
  channel_family text NOT NULL,
  locale text NOT NULL,
  jurisdiction char(2) NOT NULL,
  ruleset_digest char(64) NOT NULL,
  profile_document jsonb NOT NULL DEFAULT '{}'::jsonb,
  active boolean NOT NULL DEFAULT false,
  freshness_seconds bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,profile_id,profile_version),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT publication_quality_profiles_ref_chk CHECK (
    profile_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    channel_family ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    locale ~ '^[a-z]{2}(-[A-Z]{2})?$' AND jurisdiction ~ '^[A-Z]{2}$' AND
    ruleset_digest ~ '^[0-9a-f]{64}$' AND profile_version >= 1
  ),
  CONSTRAINT publication_quality_profiles_document_chk CHECK (
    jsonb_typeof(profile_document) = 'object' AND
    jsonb_array_length(COALESCE(profile_document -> 'rules','[]'::jsonb)) <= 512 AND
    pg_column_size(profile_document) <= 262144
  ),
  CONSTRAINT publication_quality_profiles_freshness_chk CHECK (freshness_seconds BETWEEN 60 AND 2592000 AND updated_at >= created_at)
);

CREATE UNIQUE INDEX publication_quality_profiles_active_uq
  ON publication_quality_profiles (organization_id,workspace_id,connector_id,channel_family,locale,jurisdiction)
  WHERE active;
CREATE INDEX publication_quality_profiles_lookup_idx
  ON publication_quality_profiles (organization_id,workspace_id,connector_id,channel_family,updated_at,profile_id,profile_version);

CREATE TABLE publication_quality_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  product_id text NOT NULL,
  offer_id text NOT NULL DEFAULT '',
  connector_account_id text NOT NULL,
  connector_id text NOT NULL,
  channel_family text NOT NULL,
  locale text NOT NULL,
  jurisdiction char(2) NOT NULL,
  product_version bigint NOT NULL,
  offer_version bigint NOT NULL DEFAULT 0,
  price_version bigint NOT NULL DEFAULT 0,
  inventory_version bigint NOT NULL DEFAULT 0,
  media_version bigint NOT NULL DEFAULT 0,
  mapping_version bigint NOT NULL DEFAULT 0,
  capability_version bigint NOT NULL DEFAULT 0,
  snapshot_digest char(64) NOT NULL,
  profile_digest char(64) NOT NULL,
  compliance_fingerprint char(64) NOT NULL DEFAULT repeat('0',64),
  status text NOT NULL DEFAULT 'queued',
  decision text NOT NULL DEFAULT 'unknown',
  score_bps integer NOT NULL DEFAULT 0,
  category_scores jsonb NOT NULL DEFAULT '{}'::jsonb,
  evaluated_at timestamptz,
  valid_until timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT publication_quality_runs_ref_chk CHECK (
    run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    product_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (offer_id = '' OR offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    connector_account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    channel_family ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    locale ~ '^[a-z]{2}(-[A-Z]{2})?$' AND jurisdiction ~ '^[A-Z]{2}$'
  ),
  CONSTRAINT publication_quality_runs_digest_chk CHECK (
    snapshot_digest ~ '^[0-9a-f]{64}$' AND profile_digest ~ '^[0-9a-f]{64}$' AND compliance_fingerprint ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT publication_quality_runs_version_chk CHECK (
    product_version >= 1 AND offer_version >= 0 AND price_version >= 0 AND inventory_version >= 0 AND
    media_version >= 0 AND mapping_version >= 0 AND capability_version >= 0 AND score_bps BETWEEN 0 AND 10000 AND
    version >= 1 AND pg_column_size(category_scores) <= 16384
  ),
  CONSTRAINT publication_quality_runs_state_chk CHECK (status IN ('queued','running','completed','failed','cancelled') AND decision IN ('ready','ready_with_warnings','blocked','approval_required','stale','unsupported','not_configured','unknown')),
  CONSTRAINT publication_quality_runs_time_chk CHECK (valid_until IS NULL OR evaluated_at IS NULL OR valid_until >= evaluated_at)
);

CREATE INDEX publication_quality_runs_target_idx
  ON publication_quality_runs (organization_id,workspace_id,product_id,connector_account_id,updated_at DESC,run_id DESC);
CREATE INDEX publication_quality_runs_decision_idx
  ON publication_quality_runs (organization_id,workspace_id,decision,updated_at DESC,run_id DESC);

CREATE TABLE publication_quality_issues (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  issue_id text NOT NULL,
  code text NOT NULL,
  category text NOT NULL,
  severity text NOT NULL,
  field_path text NOT NULL DEFAULT '',
  message text NOT NULL,
  expected text NOT NULL DEFAULT '',
  observed text NOT NULL DEFAULT '',
  remediation text NOT NULL,
  source_ref text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id,issue_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES publication_quality_runs (organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT publication_quality_issues_ref_chk CHECK (
    issue_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND code ~ '^[a-z][a-z0-9._-]{0,63}$' AND
    category ~ '^[a-z][a-z0-9._-]{0,63}$' AND severity IN ('block','approval_required','warn','info') AND
    char_length(field_path) <= 256 AND char_length(message) BETWEEN 1 AND 240 AND
    char_length(expected) <= 256 AND char_length(observed) <= 256 AND char_length(remediation) BETWEEN 1 AND 240 AND
    (source_ref = '' OR source_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
  )
);

CREATE INDEX publication_quality_issues_lookup_idx
  ON publication_quality_issues (organization_id,workspace_id,run_id,severity,category,issue_id);

CREATE TABLE publication_quality_gate_receipts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  receipt_id text NOT NULL,
  run_id text NOT NULL,
  product_id text NOT NULL,
  offer_id text NOT NULL DEFAULT '',
  connector_account_id text NOT NULL,
  connector_id text NOT NULL,
  channel_family text NOT NULL,
  locale text NOT NULL,
  jurisdiction char(2) NOT NULL,
  product_version bigint NOT NULL,
  offer_version bigint NOT NULL DEFAULT 0,
  price_version bigint NOT NULL DEFAULT 0,
  inventory_version bigint NOT NULL DEFAULT 0,
  media_version bigint NOT NULL DEFAULT 0,
  mapping_version bigint NOT NULL DEFAULT 0,
  capability_version bigint NOT NULL DEFAULT 0,
  snapshot_digest char(64) NOT NULL,
  profile_digest char(64) NOT NULL,
  compliance_fingerprint char(64) NOT NULL,
  decision text NOT NULL,
  issued_at timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (organization_id,workspace_id,receipt_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES publication_quality_runs (organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT publication_quality_receipts_ref_chk CHECK (
    receipt_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    product_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (offer_id = '' OR offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    connector_account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    channel_family ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND locale ~ '^[a-z]{2}(-[A-Z]{2})?$' AND jurisdiction ~ '^[A-Z]{2}$'
  ),
  CONSTRAINT publication_quality_receipts_digest_chk CHECK (
    snapshot_digest ~ '^[0-9a-f]{64}$' AND profile_digest ~ '^[0-9a-f]{64}$' AND compliance_fingerprint ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT publication_quality_receipts_version_chk CHECK (
    product_version >= 1 AND offer_version >= 0 AND price_version >= 0 AND inventory_version >= 0 AND media_version >= 0 AND
    mapping_version >= 0 AND capability_version >= 0 AND decision IN ('ready','ready_with_warnings') AND version >= 1 AND valid_until >= issued_at
  )
);

CREATE UNIQUE INDEX publication_quality_receipts_exact_uq
  ON publication_quality_gate_receipts (organization_id,workspace_id,product_id,offer_id,connector_account_id,product_version,offer_version,price_version,inventory_version,media_version,mapping_version,capability_version,snapshot_digest,profile_digest,compliance_fingerprint);
CREATE INDEX publication_quality_receipts_active_idx
  ON publication_quality_gate_receipts (organization_id,workspace_id,connector_account_id,product_id,valid_until DESC,receipt_id DESC);

-- Remediation intent is an auditable proposal. It is not a write to the
-- canonical catalog and stores only a bounded digest of any proposed diff.
CREATE TABLE publication_quality_remediations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  remediation_id text NOT NULL,
  run_id text NOT NULL,
  issue_id text NOT NULL,
  action_code text NOT NULL,
  status text NOT NULL DEFAULT 'proposed',
  expected_snapshot_digest char(64) NOT NULL,
  proposed_diff_digest char(64) NOT NULL,
  approval_id text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,remediation_id),
  FOREIGN KEY (organization_id,workspace_id,run_id) REFERENCES publication_quality_runs (organization_id,workspace_id,run_id) ON DELETE RESTRICT,
  CONSTRAINT publication_quality_remediations_ref_chk CHECK (
    remediation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND issue_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    action_code ~ '^[a-z][a-z0-9._-]{0,63}$' AND status IN ('proposed','approved','applied','rejected','expired') AND
    expected_snapshot_digest ~ '^[0-9a-f]{64}$' AND proposed_diff_digest ~ '^[0-9a-f]{64}$' AND
    (approval_id = '' OR approval_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
  )
);

CREATE FUNCTION publication_quality_evidence_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''publication quality evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER publication_quality_issues_no_update_delete
  BEFORE UPDATE OR DELETE OR TRUNCATE ON publication_quality_issues
  FOR EACH STATEMENT EXECUTE FUNCTION publication_quality_evidence_no_mutation();
CREATE TRIGGER publication_quality_receipts_no_update_delete
  BEFORE UPDATE OR DELETE OR TRUNCATE ON publication_quality_gate_receipts
  FOR EACH STATEMENT EXECUTE FUNCTION publication_quality_evidence_no_mutation();
CREATE TRIGGER publication_quality_remediations_no_update_delete
  BEFORE UPDATE OR DELETE OR TRUNCATE ON publication_quality_remediations
  FOR EACH STATEMENT EXECUTE FUNCTION publication_quality_evidence_no_mutation();
REVOKE DELETE,TRUNCATE ON publication_quality_issues,publication_quality_gate_receipts,publication_quality_remediations FROM PUBLIC;

ALTER TABLE publication_quality_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_quality_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY publication_quality_profiles_tenant_all ON publication_quality_profiles FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE publication_quality_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_quality_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY publication_quality_runs_tenant_all ON publication_quality_runs FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE publication_quality_issues ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_quality_issues FORCE ROW LEVEL SECURITY;
CREATE POLICY publication_quality_issues_tenant_all ON publication_quality_issues FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE publication_quality_gate_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_quality_gate_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY publication_quality_receipts_tenant_all ON publication_quality_gate_receipts FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE publication_quality_remediations ENABLE ROW LEVEL SECURITY;
ALTER TABLE publication_quality_remediations FORCE ROW LEVEL SECURITY;
CREATE POLICY publication_quality_remediations_tenant_all ON publication_quality_remediations FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE publication_quality_profiles IS 'Versioned tenant-scoped declarative publication profiles; never executable code.';
COMMENT ON TABLE publication_quality_runs IS 'Tenant-scoped derived publication-quality evaluations and target-specific decisions.';
COMMENT ON TABLE publication_quality_issues IS 'Append-only bounded issues explaining a quality run without raw provider payloads.';
COMMENT ON TABLE publication_quality_gate_receipts IS 'Append-only exact-match receipts checked immediately before product publication egress.';
COMMENT ON TABLE publication_quality_remediations IS 'Append-only remediation proposals; canonical product writes remain policy-gated elsewhere.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
