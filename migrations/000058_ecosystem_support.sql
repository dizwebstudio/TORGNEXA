BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 231: tenant-owned onboarding and partner qualification evidence.
-- Global connector/app catalogs remain checked-in or owned by their existing
-- bounded contexts. These tables contain only minimized references and
-- redacted evidence, never credentials, customer payloads or private keys.
CREATE TABLE ecosystem_onboarding_runs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  run_id text NOT NULL,
  resource_id text NOT NULL,
  state text NOT NULL,
  checks jsonb NOT NULL,
  owner_ref text NOT NULL,
  idempotency_key text NOT NULL,
  version bigint NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,run_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,idempotency_key),
  CONSTRAINT ecosystem_onboarding_ref_chk CHECK (
    run_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    resource_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    owner_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND
    state IN ('draft','running','ready','blocked','rolled_back') AND
    version >= 1 AND updated_at >= created_at AND
    jsonb_typeof(checks) = 'array' AND jsonb_array_length(checks) BETWEEN 1 AND 64 AND
    pg_column_size(checks) <= 1048576 AND
    checks::text !~* 'authorization|access_token|client_secret|private_key|raw_payload|password'
  )
);
CREATE INDEX ecosystem_onboarding_updated_idx ON ecosystem_onboarding_runs(organization_id,workspace_id,updated_at DESC,run_id DESC);

CREATE TABLE ecosystem_partner_certifications (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  certification_id text NOT NULL,
  partner_ref text NOT NULL,
  tier text NOT NULL,
  state text NOT NULL,
  evidence jsonb,
  expires_at timestamptz NOT NULL,
  idempotency_key text NOT NULL,
  version bigint NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,certification_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,idempotency_key),
  CONSTRAINT ecosystem_partner_certification_chk CHECK (
    certification_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    partner_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    tier IN ('referral','implementation','certified_solution','managed_operations','support_escalation') AND
    state IN ('applied','sandbox_ready','certified','suspended','revoked','expired') AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND version >= 1 AND
    expires_at IS NOT NULL AND
    (state <> 'certified' OR jsonb_typeof(evidence) = 'object') AND
    (evidence IS NULL OR (jsonb_typeof(evidence) = 'object' AND pg_column_size(evidence) <= 65536 AND evidence::text !~* 'authorization|access_token|client_secret|private_key|raw_payload|password'))
  )
);
CREATE INDEX ecosystem_partner_certification_updated_idx ON ecosystem_partner_certifications(organization_id,workspace_id,updated_at DESC,certification_id DESC);

CREATE FUNCTION ecosystem_support_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''ecosystem support evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER ecosystem_onboarding_runs_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON ecosystem_onboarding_runs FOR EACH STATEMENT EXECUTE FUNCTION ecosystem_support_append_only();
CREATE TRIGGER ecosystem_partner_certifications_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON ecosystem_partner_certifications FOR EACH STATEMENT EXECUTE FUNCTION ecosystem_support_append_only();

ALTER TABLE ecosystem_onboarding_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE ecosystem_onboarding_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE ecosystem_partner_certifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE ecosystem_partner_certifications FORCE ROW LEVEL SECURITY;

CREATE POLICY ecosystem_onboarding_runs_tenant_all ON ecosystem_onboarding_runs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY ecosystem_partner_certifications_tenant_all ON ecosystem_partner_certifications FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE UPDATE,DELETE,TRUNCATE ON ecosystem_onboarding_runs,ecosystem_partner_certifications FROM PUBLIC;

COMMENT ON TABLE ecosystem_onboarding_runs IS 'Append-only tenant onboarding evidence; status does not promote a connector to qualified or supported.';
COMMENT ON TABLE ecosystem_partner_certifications IS 'Append-only tenant partner certification evidence; production claims require retained UAT and rollback proof.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
