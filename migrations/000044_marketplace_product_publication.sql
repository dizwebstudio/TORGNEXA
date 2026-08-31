BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Epic 172 / repository task 217. The snapshot is the durable contract sent
-- to connectors. It contains no provider payloads, URLs, tokens or storage
-- keys; the host validates released media before assembling it.
CREATE TABLE marketplace_publication_snapshots (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  snapshot_id text NOT NULL,
  product_id text NOT NULL,
  offer_id text NOT NULL DEFAULT '',
  connector_account_id text NOT NULL,
  connector_id text NOT NULL,
  snapshot_version bigint NOT NULL,
  snapshot_digest char(64) NOT NULL,
  snapshot_document jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,snapshot_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_publication_snapshots_ref_chk CHECK (
    snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    product_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (offer_id = '' OR offer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    connector_account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND snapshot_version >= 1 AND
    snapshot_digest ~ '^[0-9a-f]{64}$' AND jsonb_typeof(snapshot_document) = 'object' AND
    pg_column_size(snapshot_document) <= 1048576 AND snapshot_document::text !~* 'https?://'
  )
);
CREATE INDEX marketplace_publication_snapshots_target_idx ON marketplace_publication_snapshots (organization_id,workspace_id,connector_account_id,product_id,created_at DESC);

CREATE TABLE marketplace_publication_operations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_id text NOT NULL,
  snapshot_id text NOT NULL,
  connector_account_id text NOT NULL,
  connector_id text NOT NULL,
  operation_kind text NOT NULL,
  state text NOT NULL DEFAULT 'draft',
  idempotency_key text NOT NULL,
  remote_id text NOT NULL DEFAULT '',
  remote_operation_id text NOT NULL DEFAULT '',
  attempt integer NOT NULL DEFAULT 0,
  dry_run boolean NOT NULL DEFAULT false,
  approval_request_id text NOT NULL DEFAULT '',
  quality_receipt_id text NOT NULL,
  error_code text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,operation_id),
  FOREIGN KEY (organization_id,workspace_id,snapshot_id) REFERENCES marketplace_publication_snapshots (organization_id,workspace_id,snapshot_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_publication_operations_ref_chk CHECK (
    operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    operation_kind IN ('create_product','update_product','update_variant','update_attributes','update_media','archive','unarchive','publish','unpublish','status_read') AND
    state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled') AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    (remote_id = '' OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    (remote_operation_id = '' OR remote_operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    attempt BETWEEN 0 AND 100 AND (approval_request_id = '' OR approval_request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    quality_receipt_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (error_code = '' OR error_code ~ '^[a-z][a-z0-9._-]{0,63}$') AND version >= 1 AND updated_at >= created_at
  ),
  CONSTRAINT marketplace_publication_operations_idempotency_uq UNIQUE (organization_id,workspace_id,connector_account_id,idempotency_key)
);
CREATE INDEX marketplace_publication_operations_queue_idx ON marketplace_publication_operations (organization_id,workspace_id,state,updated_at,operation_id);

CREATE TABLE marketplace_publication_operation_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_id text NOT NULL,
  event_id text NOT NULL,
  from_state text NOT NULL,
  to_state text NOT NULL,
  error_code text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,operation_id,event_id),
  FOREIGN KEY (organization_id,workspace_id,operation_id) REFERENCES marketplace_publication_operations (organization_id,workspace_id,operation_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_publication_operation_events_ref_chk CHECK (
    operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    from_state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled') AND
    to_state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled') AND
    (error_code = '' OR error_code ~ '^[a-z][a-z0-9._-]{0,63}$')
  )
);

CREATE TABLE marketplace_publication_observations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  observation_id text NOT NULL,
  operation_id text NOT NULL,
  remote_id text NOT NULL,
  remote_operation_id text NOT NULL DEFAULT '',
  state text NOT NULL,
  moderation text NOT NULL DEFAULT 'unknown',
  snapshot_digest char(64) NOT NULL DEFAULT '',
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,observation_id),
  FOREIGN KEY (organization_id,workspace_id,operation_id) REFERENCES marketplace_publication_operations (organization_id,workspace_id,operation_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_publication_observations_ref_chk CHECK (
    observation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (remote_operation_id = '' OR remote_operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled') AND
    moderation IN ('unknown','pending','approved','rejected') AND (snapshot_digest = '' OR snapshot_digest ~ '^[0-9a-f]{64}$')
  )
);

CREATE TABLE marketplace_publication_drifts (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  drift_id text NOT NULL,
  operation_id text NOT NULL,
  snapshot_id text NOT NULL,
  drift_type text NOT NULL,
  remote_id text NOT NULL DEFAULT '',
  expected_digest char(64) NOT NULL DEFAULT '',
  observed_digest char(64) NOT NULL DEFAULT '',
  observed_state text NOT NULL DEFAULT '',
  detected_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,drift_id),
  FOREIGN KEY (organization_id,workspace_id,operation_id) REFERENCES marketplace_publication_operations (organization_id,workspace_id,operation_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,snapshot_id) REFERENCES marketplace_publication_snapshots (organization_id,workspace_id,snapshot_id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_publication_drifts_ref_chk CHECK (
    drift_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND snapshot_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    drift_type IN ('missing_remote_product','duplicate_remote_product','content_mismatch','attribute_mismatch','media_mismatch','mapping_conflict','moderation_rejected','publication_status_mismatch','unknown_write_outcome') AND
    (remote_id = '' OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND
    (expected_digest = '' OR expected_digest ~ '^[0-9a-f]{64}$') AND (observed_digest = '' OR observed_digest ~ '^[0-9a-f]{64}$') AND
    (observed_state = '' OR observed_state IN ('draft','preflight','queued','sending','accepted','processing','published','rejected','unknown','needs_attention','cancelled'))
  )
);

CREATE FUNCTION marketplace_publication_evidence_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace publication evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER marketplace_publication_events_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_publication_operation_events FOR EACH STATEMENT EXECUTE FUNCTION marketplace_publication_evidence_no_mutation();
CREATE TRIGGER marketplace_publication_observations_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_publication_observations FOR EACH STATEMENT EXECUTE FUNCTION marketplace_publication_evidence_no_mutation();
CREATE TRIGGER marketplace_publication_drifts_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_publication_drifts FOR EACH STATEMENT EXECUTE FUNCTION marketplace_publication_evidence_no_mutation();
REVOKE DELETE,TRUNCATE ON marketplace_publication_operation_events,marketplace_publication_observations,marketplace_publication_drifts FROM PUBLIC;

DO $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY['marketplace_publication_snapshots','marketplace_publication_operations','marketplace_publication_operation_events','marketplace_publication_observations','marketplace_publication_drifts'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
    EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (organization_id=current_setting(''app.organization_id'',true) AND workspace_id=current_setting(''app.workspace_id'',true)) WITH CHECK (organization_id=current_setting(''app.organization_id'',true) AND workspace_id=current_setting(''app.workspace_id'',true))', table_name || '_tenant_policy', table_name);
  END LOOP;
END $$;

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
