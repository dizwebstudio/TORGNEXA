BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Epic 171: provider-neutral traceability execution. Raw Data Matrix values
-- are intentionally absent from this schema. artifact_ref is an opaque,
-- expiring reference into the protected artifact/secret contour.
CREATE TABLE marking_code_batches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  product_group text NOT NULL,
  gtin text NOT NULL,
  sku text NOT NULL,
  requested_quantity bigint NOT NULL,
  received_quantity bigint NOT NULL DEFAULT 0,
  reserved_quantity bigint NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'requested',
  raw_artifact_ref text NOT NULL DEFAULT '',
  raw_artifact_expires_at timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,batch_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_batch_ref_chk CHECK(batch_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND product_group ~ '^[a-z][a-z0-9._-]{0,63}$' AND gtin ~ '^[0-9]{8,14}$' AND sku <> '' AND char_length(sku) <= 200 AND sku = btrim(sku)),
  CONSTRAINT marking_batch_quantity_chk CHECK(requested_quantity BETWEEN 1 AND 1000000000 AND received_quantity BETWEEN 0 AND requested_quantity AND reserved_quantity BETWEEN 0 AND received_quantity),
  CONSTRAINT marking_batch_state_chk CHECK(status IN ('requested','reserved','available','printed','applied','aggregated','introduced','in_circulation','sold','withdrawn','written_off','returned','rejected','unknown')),
  CONSTRAINT marking_batch_artifact_chk CHECK(raw_artifact_ref = '' OR raw_artifact_ref ~ '^(artifact|sec):[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT marking_batch_time_chk CHECK(updated_at >= created_at AND (raw_artifact_ref = '' OR raw_artifact_expires_at IS NOT NULL))
);
CREATE INDEX marking_batch_recent_idx ON marking_code_batches(organization_id,workspace_id,updated_at DESC,batch_id DESC);

CREATE TABLE marking_codes (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  fingerprint char(64) NOT NULL,
  batch_id text NOT NULL,
  gtin text NOT NULL,
  sku text NOT NULL,
  status text NOT NULL DEFAULT 'requested',
  package_id text NOT NULL DEFAULT '',
  remote_status text NOT NULL DEFAULT '',
  last_observed_at timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,fingerprint),
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES marking_code_batches(organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  CONSTRAINT marking_code_fingerprint_chk CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT marking_code_ref_chk CHECK(gtin ~ '^[0-9]{8,14}$' AND sku <> '' AND char_length(sku) <= 200 AND (package_id = '' OR package_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (remote_status = '' OR remote_status ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')),
  CONSTRAINT marking_code_state_chk CHECK(status IN ('requested','reserved','available','printed','applied','aggregated','introduced','in_circulation','sold','withdrawn','written_off','returned','rejected','unknown')),
  CONSTRAINT marking_code_time_chk CHECK(updated_at >= created_at)
);
CREATE INDEX marking_codes_batch_idx ON marking_codes(organization_id,workspace_id,batch_id,status,fingerprint);

CREATE TABLE marking_operations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_id text NOT NULL,
  kind text NOT NULL,
  state text NOT NULL DEFAULT 'queued',
  idempotency_key text NOT NULL,
  dry_run boolean NOT NULL DEFAULT false,
  approval_ref text NOT NULL DEFAULT '',
  artifact_ref text NOT NULL DEFAULT '',
  remote_id text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  attempt integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,operation_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_operation_ref_chk CHECK(operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND kind ~ '^marking\.[a-z][a-z0-9._-]{1,95}$' AND (approval_ref = '' OR approval_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (artifact_ref = '' OR artifact_ref ~ '^(artifact|sec):[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (remote_id = '' OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (error_code = '' OR error_code ~ '^[a-z0-9._-]{1,95}$') AND attempt BETWEEN 1 AND 20),
  CONSTRAINT marking_operation_state_chk CHECK(state IN ('queued','running','succeeded','failed','unknown','cancelled') AND ((state IN ('queued','running') AND completed_at IS NULL) OR (state IN ('succeeded','failed','unknown','cancelled') AND completed_at IS NOT NULL))),
  CONSTRAINT marking_operation_time_chk CHECK(updated_at >= created_at AND (completed_at IS NULL OR completed_at >= created_at))
);
CREATE UNIQUE INDEX marking_operations_idempotency_idx ON marking_operations(organization_id,workspace_id,idempotency_key);

CREATE TABLE marking_packages (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  package_id text NOT NULL,
  kind text NOT NULL,
  code_fingerprint char(64) NOT NULL,
  parent_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open',
  shipment_ref text NOT NULL DEFAULT '',
  order_ref text NOT NULL DEFAULT '',
  upd_ref text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,package_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_package_ref_chk CHECK(package_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND code_fingerprint ~ '^[0-9a-f]{64}$' AND (parent_id = '' OR parent_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND status IN ('open','closed','dissolved','unknown') AND (shipment_ref = '' OR shipment_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (order_ref = '' OR order_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (upd_ref = '' OR upd_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')),
  CONSTRAINT marking_package_kind_chk CHECK(kind IN ('unit','kit','box','pallet')),
  CONSTRAINT marking_package_time_chk CHECK(updated_at >= created_at)
);
CREATE TABLE marking_package_links (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  parent_id text NOT NULL,
  child_id text NOT NULL,
  quantity bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,parent_id,child_id),
  FOREIGN KEY (organization_id,workspace_id,parent_id) REFERENCES marking_packages(organization_id,workspace_id,package_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,child_id) REFERENCES marking_packages(organization_id,workspace_id,package_id) ON DELETE RESTRICT,
  CONSTRAINT marking_package_link_ref_chk CHECK(parent_id <> child_id AND quantity BETWEEN 1 AND 1000000000)
);

CREATE TABLE marking_print_jobs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  print_job_id text NOT NULL,
  template_ref text NOT NULL,
  template_version bigint NOT NULL,
  printer_ref text NOT NULL,
  code_count bigint NOT NULL,
  state text NOT NULL DEFAULT 'queued',
  attempt integer NOT NULL DEFAULT 1,
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,print_job_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_print_job_ref_chk CHECK(print_job_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND template_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND printer_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND template_version >= 1 AND code_count BETWEEN 1 AND 1000000000 AND attempt BETWEEN 1 AND 20),
  CONSTRAINT marking_print_job_state_chk CHECK(state IN ('queued','running','completed','failed','unknown')),
  CONSTRAINT marking_print_job_time_chk CHECK(updated_at >= created_at)
);
CREATE UNIQUE INDEX marking_print_jobs_idempotency_idx ON marking_print_jobs(organization_id,workspace_id,idempotency_key);

CREATE TABLE marking_scans (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  scan_id text NOT NULL,
  fingerprint char(64) NOT NULL,
  gtin text NOT NULL,
  sku text NOT NULL,
  wms_action text NOT NULL,
  result text NOT NULL,
  reason_code text NOT NULL DEFAULT '',
  actor_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,scan_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_scan_ref_chk CHECK(scan_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND fingerprint ~ '^[0-9a-f]{64}$' AND gtin ~ '^[0-9]{8,14}$' AND sku <> '' AND wms_action ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (reason_code = '' OR reason_code ~ '^[a-z0-9._-]{1,95}$')),
  CONSTRAINT marking_scan_result_chk CHECK(result IN ('accepted','rejected','duplicate','overflow'))
);

CREATE TABLE marking_documents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  document_id text NOT NULL,
  format_version text NOT NULL DEFAULT '5.03',
  kind text NOT NULL,
  counterparty_ref text NOT NULL,
  state text NOT NULL DEFAULT 'draft',
  artifact_ref text NOT NULL,
  signature_ref text NOT NULL DEFAULT '',
  mchd_ref text NOT NULL DEFAULT '',
  remote_id text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,document_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_document_ref_chk CHECK(document_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND format_version = '5.03' AND kind ~ '^[a-z][a-z0-9._-]{0,63}$' AND counterparty_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND artifact_ref ~ '^(artifact|sec):[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (signature_ref = '' OR signature_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (mchd_ref = '' OR mchd_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (remote_id = '' OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')),
  CONSTRAINT marking_document_state_chk CHECK(state IN ('draft','ready','signing','sent','confirmed','rejected','correction_required','unknown')),
  CONSTRAINT marking_document_time_chk CHECK(updated_at >= created_at)
);
CREATE TABLE marking_document_lines (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  document_id text NOT NULL,
  line_id text NOT NULL,
  gtin text NOT NULL,
  sku text NOT NULL,
  fingerprint char(64) NOT NULL DEFAULT '',
  package_id text NOT NULL DEFAULT '',
  quantity bigint NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,document_id,line_id),
  FOREIGN KEY (organization_id,workspace_id,document_id) REFERENCES marking_documents(organization_id,workspace_id,document_id) ON DELETE RESTRICT,
  CONSTRAINT marking_document_line_ref_chk CHECK(line_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND gtin ~ '^[0-9]{8,14}$' AND sku <> '' AND quantity BETWEEN 1 AND 1000000000 AND (fingerprint = '' OR fingerprint ~ '^[0-9a-f]{64}$') AND (package_id = '' OR package_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'))
);

CREATE TABLE marking_remote_observations (
  evidence_id bigserial PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  observation_id text NOT NULL,
  entity_type text NOT NULL,
  entity_ref text NOT NULL,
  remote_status text NOT NULL,
  remote_request_id text NOT NULL DEFAULT '',
  observed_at timestamptz NOT NULL,
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_observation_ref_chk CHECK(observation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND entity_type ~ '^[a-z][a-z0-9._-]{0,63}$' AND entity_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND remote_status ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (remote_request_id = '' OR remote_request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'))
);
CREATE UNIQUE INDEX marking_observation_dedupe_idx ON marking_remote_observations(organization_id,workspace_id,observation_id,entity_ref,observed_at);

CREATE TABLE marking_drifts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  drift_id text NOT NULL,
  entity_type text NOT NULL,
  entity_ref text NOT NULL,
  drift_type text NOT NULL,
  expected_value text NOT NULL,
  observed_value text NOT NULL,
  resolved boolean NOT NULL DEFAULT false,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,drift_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marking_drift_ref_chk CHECK(drift_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND entity_type ~ '^[a-z][a-z0-9._-]{0,63}$' AND entity_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND drift_type IN ('status','quantity','package_composition','unknown_write_result','missing_remote_observation') AND expected_value <> '' AND observed_value <> '')
);

CREATE TABLE marking_process_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  batch_id text NOT NULL,
  stage text NOT NULL DEFAULT 'codes_request',
  state text NOT NULL DEFAULT 'queued',
  last_operation_id text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES marking_code_batches(organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  CONSTRAINT marking_process_ref_chk CHECK(run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (last_operation_id = '' OR last_operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND stage IN ('codes_request','reserve','print','scan','aggregate','upd','sign','edo','circulation','reconciliation')),
  CONSTRAINT marking_process_state_chk CHECK(state IN ('queued','running','succeeded','failed','unknown','manual_attention')),
  CONSTRAINT marking_process_time_chk CHECK(updated_at >= created_at)
);

CREATE FUNCTION marking_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''marking evidence is append-only''; END';
CREATE TRIGGER marking_remote_observations_append_only_guard BEFORE UPDATE OR DELETE ON marking_remote_observations FOR EACH ROW EXECUTE FUNCTION marking_append_only();
CREATE TRIGGER marking_drifts_append_only_guard BEFORE UPDATE OR DELETE ON marking_drifts FOR EACH ROW EXECUTE FUNCTION marking_append_only();
CREATE TRIGGER marking_scans_append_only_guard BEFORE UPDATE OR DELETE ON marking_scans FOR EACH ROW EXECUTE FUNCTION marking_append_only();

DO $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY['marking_code_batches','marking_codes','marking_operations','marking_packages','marking_package_links','marking_print_jobs','marking_scans','marking_documents','marking_document_lines','marking_remote_observations','marking_drifts','marking_process_runs'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
    EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (organization_id=current_setting(''app.organization_id'',true) AND workspace_id=current_setting(''app.workspace_id'',true)) WITH CHECK (organization_id=current_setting(''app.organization_id'',true) AND workspace_id=current_setting(''app.workspace_id'',true))', table_name || '_tenant_policy', table_name);
  END LOOP;
END $$;

CREATE INDEX marking_codes_observation_idx ON marking_codes(organization_id,workspace_id,last_observed_at DESC,fingerprint);
CREATE INDEX marking_packages_parent_idx ON marking_packages(organization_id,workspace_id,parent_id,package_id);
CREATE INDEX marking_drifts_open_idx ON marking_drifts(organization_id,workspace_id,resolved,observed_at DESC,drift_id);
CREATE INDEX marking_process_queue_idx ON marking_process_runs(organization_id,workspace_id,state,updated_at,run_id);

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
