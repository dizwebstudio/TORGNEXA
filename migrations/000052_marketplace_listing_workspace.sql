BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 222: persist channel taxonomy and mass-edit qualification evidence.
-- These documents are provider-neutral and intentionally exclude credentials,
-- raw provider payloads, URLs and unreleased media objects.
CREATE TABLE marketplace_listing_taxonomies (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  taxonomy_id text NOT NULL,
  connector_id text NOT NULL,
  locale text NOT NULL,
  jurisdiction text NOT NULL,
  taxonomy_version bigint NOT NULL,
  source text NOT NULL,
  fingerprint char(64) NOT NULL,
  observed_at timestamptz NOT NULL,
  fresh_until timestamptz NOT NULL,
  taxonomy_document jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,taxonomy_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_listing_taxonomy_ref_chk CHECK (
    taxonomy_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    connector_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    locale ~ '^[a-z]{2}(?:-[A-Z]{2})?$' AND jurisdiction ~ '^[A-Z]{2}$' AND
    taxonomy_version >= 1 AND source <> '' AND length(source) <= 256 AND
    fingerprint ~ '^[0-9a-f]{64}$' AND fresh_until > observed_at AND
    jsonb_typeof(taxonomy_document) = 'object' AND pg_column_size(taxonomy_document) <= 4194304 AND
    taxonomy_document::text !~* 'https?://'
  ),
  CONSTRAINT marketplace_listing_taxonomy_version_uq UNIQUE (organization_id,workspace_id,connector_id,locale,jurisdiction,taxonomy_version)
);
CREATE INDEX marketplace_listing_taxonomies_fresh_idx ON marketplace_listing_taxonomies(organization_id,workspace_id,connector_id,locale,jurisdiction,fresh_until DESC);

CREATE TABLE marketplace_listing_batches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  preview_id text NOT NULL,
  idempotency_key text NOT NULL,
  approval_request_id text NOT NULL,
  state text NOT NULL,
  input_digest char(64) NOT NULL,
  affected_count integer NOT NULL,
  eligible_count integer NOT NULL,
  blocked_count integer NOT NULL,
  batch_document jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,batch_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT marketplace_listing_batch_ref_chk CHECK (
    batch_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    preview_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    approval_request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    state IN ('queued','processing','completed','partial','unknown','rejected') AND
    input_digest ~ '^[0-9a-f]{64}$' AND affected_count BETWEEN 1 AND 1000 AND
    eligible_count BETWEEN 0 AND affected_count AND blocked_count = affected_count - eligible_count AND
    jsonb_typeof(batch_document) = 'object' AND pg_column_size(batch_document) <= 16777216 AND
    created_at <= updated_at
  ),
  CONSTRAINT marketplace_listing_batch_idempotency_uq UNIQUE (organization_id,workspace_id,idempotency_key)
);
CREATE INDEX marketplace_listing_batches_updated_idx ON marketplace_listing_batches(organization_id,workspace_id,updated_at DESC,batch_id DESC);

CREATE FUNCTION marketplace_listing_workspace_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''marketplace listing qualification evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER marketplace_listing_taxonomies_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_listing_taxonomies FOR EACH STATEMENT EXECUTE FUNCTION marketplace_listing_workspace_no_mutation();
CREATE TRIGGER marketplace_listing_batches_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON marketplace_listing_batches FOR EACH STATEMENT EXECUTE FUNCTION marketplace_listing_workspace_no_mutation();
REVOKE UPDATE,DELETE,TRUNCATE ON marketplace_listing_taxonomies,marketplace_listing_batches FROM PUBLIC;

ALTER TABLE marketplace_listing_taxonomies ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_listing_taxonomies FORCE ROW LEVEL SECURITY;
ALTER TABLE marketplace_listing_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_listing_batches FORCE ROW LEVEL SECURITY;
CREATE POLICY marketplace_listing_taxonomies_tenant_all ON marketplace_listing_taxonomies FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY marketplace_listing_batches_tenant_all ON marketplace_listing_batches FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE marketplace_listing_taxonomies IS 'Tenant-scoped, immutable provider-neutral taxonomy versions used by Task 222 preflight and mapping.';
COMMENT ON TABLE marketplace_listing_batches IS 'Tenant-scoped, immutable before/after batch qualification evidence; apply remains approval-gated.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
