BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- Public marketplace versions are immutable reviewed facts. Private packages are
-- deliberately stored separately so global catalog rows never need tenant RLS exceptions.
CREATE TABLE plugin_marketplace_versions (
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  publisher_id text NOT NULL CHECK(publisher_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  publisher_key_id text NOT NULL CHECK(publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
  publisher_key_fingerprint_sha256 text NOT NULL CHECK(publisher_key_fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  trust text NOT NULL CHECK(trust IN ('official','verified','community')),
  license_expression text NOT NULL CHECK(char_length(license_expression) BETWEEN 1 AND 256),
  security_contact text NOT NULL CHECK(char_length(security_contact) BETWEEN 3 AND 320),
  security_descriptor jsonb NOT NULL CHECK(jsonb_typeof(security_descriptor)='object'),
  review_evidence jsonb NOT NULL CHECK(jsonb_typeof(review_evidence)='object'),
  published_at timestamptz NOT NULL,
  PRIMARY KEY(plugin_id,plugin_version,artifact_sha256)
);
CREATE INDEX plugin_marketplace_versions_latest_idx ON plugin_marketplace_versions(plugin_id,published_at DESC,plugin_version DESC);
CREATE INDEX plugin_marketplace_versions_trust_idx ON plugin_marketplace_versions(trust,published_at DESC,plugin_id);

CREATE TABLE plugin_private_versions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  publisher_id text NOT NULL CHECK(publisher_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  publisher_key_id text NOT NULL CHECK(publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
  publisher_key_fingerprint_sha256 text NOT NULL CHECK(publisher_key_fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  trust text NOT NULL CHECK(trust='private'),
  license_expression text NOT NULL CHECK(char_length(license_expression) BETWEEN 1 AND 256),
  security_contact text NOT NULL CHECK(char_length(security_contact) BETWEEN 3 AND 320),
  security_descriptor jsonb NOT NULL CHECK(jsonb_typeof(security_descriptor)='object'),
  review_evidence jsonb NOT NULL CHECK(jsonb_typeof(review_evidence)='object'),
  published_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,plugin_id,plugin_version,artifact_sha256),
  CONSTRAINT plugin_private_versions_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE INDEX plugin_private_versions_latest_idx ON plugin_private_versions(organization_id,workspace_id,plugin_id,published_at DESC,plugin_version DESC);

-- Consent is an immutable exact-artifact grant. A new digest/version always needs a
-- new row, even when authority does not grow; Task 078 separately surfaces escalation.
CREATE TABLE plugin_marketplace_consents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  consent_id text NOT NULL CHECK(char_length(consent_id) BETWEEN 1 AND 160),
  plugin_id text NOT NULL CHECK(plugin_id ~ '^[a-z][a-z0-9-]{1,62}$'),
  plugin_version text NOT NULL CHECK(plugin_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'),
  artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
  permission_grant jsonb NOT NULL CHECK(jsonb_typeof(permission_grant)='object'),
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  granted_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,consent_id),
  CONSTRAINT plugin_marketplace_consents_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE INDEX plugin_marketplace_consents_plugin_idx ON plugin_marketplace_consents(organization_id,workspace_id,plugin_id,plugin_version,artifact_sha256,granted_at DESC);

-- Global security revocations are append-only and affect every tenant immediately.
CREATE TABLE plugin_marketplace_revocations (
  revocation_id text PRIMARY KEY CHECK(char_length(revocation_id) BETWEEN 1 AND 160),
  kind text NOT NULL CHECK(kind IN ('artifact','publisher_key')),
  plugin_id text,
  artifact_sha256 text,
  publisher_id text,
  publisher_key_id text,
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 512),
  revoked_at timestamptz NOT NULL,
  CONSTRAINT plugin_marketplace_revocation_target CHECK(
    (kind='artifact' AND plugin_id ~ '^[a-z][a-z0-9-]{1,62}$' AND artifact_sha256 ~ '^[0-9a-f]{64}$' AND publisher_id IS NULL AND publisher_key_id IS NULL)
    OR
    (kind='publisher_key' AND plugin_id IS NULL AND artifact_sha256 IS NULL AND publisher_id ~ '^[a-z][a-z0-9-]{1,62}$' AND publisher_key_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$')
  )
);
CREATE INDEX plugin_marketplace_revocations_artifact_idx ON plugin_marketplace_revocations(plugin_id,artifact_sha256) WHERE kind='artifact';
CREATE INDEX plugin_marketplace_revocations_key_idx ON plugin_marketplace_revocations(publisher_id,publisher_key_id) WHERE kind='publisher_key';

CREATE TABLE plugin_installation_revocations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  revocation_id text NOT NULL CHECK(char_length(revocation_id) BETWEEN 1 AND 160),
  consent_id text NOT NULL CHECK(char_length(consent_id) BETWEEN 1 AND 160),
  actor_id text NOT NULL CHECK(char_length(actor_id) BETWEEN 1 AND 256),
  reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 512),
  revoked_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,revocation_id),
  UNIQUE(organization_id,workspace_id,consent_id),
  CONSTRAINT plugin_installation_revocations_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT plugin_installation_revocations_consent_fk FOREIGN KEY(organization_id,workspace_id,consent_id) REFERENCES plugin_marketplace_consents(organization_id,workspace_id,consent_id)
);

CREATE FUNCTION plugin_marketplace_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''plugin marketplace governance evidence is append-only''; END';
CREATE TRIGGER plugin_marketplace_versions_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_versions FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_private_versions_append_only BEFORE UPDATE OR DELETE ON plugin_private_versions FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_marketplace_consents_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_consents FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_marketplace_revocations_append_only BEFORE UPDATE OR DELETE ON plugin_marketplace_revocations FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();
CREATE TRIGGER plugin_installation_revocations_append_only BEFORE UPDATE OR DELETE ON plugin_installation_revocations FOR EACH ROW EXECUTE FUNCTION plugin_marketplace_append_only();

ALTER TABLE plugin_private_versions ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_private_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE plugin_marketplace_consents ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_marketplace_consents FORCE ROW LEVEL SECURITY;
ALTER TABLE plugin_installation_revocations ENABLE ROW LEVEL SECURITY; ALTER TABLE plugin_installation_revocations FORCE ROW LEVEL SECURITY;

CREATE POLICY plugin_private_versions_select ON plugin_private_versions FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_private_versions_insert ON plugin_private_versions FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_marketplace_consents_select ON plugin_marketplace_consents FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_marketplace_consents_insert ON plugin_marketplace_consents FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_installation_revocations_select ON plugin_installation_revocations FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY plugin_installation_revocations_insert ON plugin_installation_revocations FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE UPDATE, DELETE, TRUNCATE ON plugin_marketplace_versions, plugin_private_versions, plugin_marketplace_consents, plugin_marketplace_revocations, plugin_installation_revocations FROM PUBLIC;

COMMENT ON TABLE plugin_marketplace_versions IS 'Immutable public official/verified/community plugin versions with reviewed trust metadata and signed Task-025 descriptor.';
COMMENT ON TABLE plugin_private_versions IS 'Tenant-private immutable plugin versions; FORCE RLS prevents cross-tenant discovery.';
COMMENT ON TABLE plugin_marketplace_consents IS 'Explicit tenant consent bound to exact plugin version and artifact digest; never inherited by a new artifact.';
COMMENT ON TABLE plugin_marketplace_revocations IS 'Global append-only artifact/publisher-key revocations applied before runtime admission.';
COMMENT ON TABLE plugin_installation_revocations IS 'Tenant-scoped append-only consent revocation; history is retained for auditability.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
