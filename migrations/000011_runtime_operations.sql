BEGIN;

-- TORGNEXA pre-v1 baseline component 000011: runtime_operations.
-- Squashed, statement-order-preserving source range: legacy 000065..000074.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000065_identity_provider_settings.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE settings_identity_providers (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  current_revision bigint NOT NULL CHECK (current_revision >= 1),
  active_revision bigint,
  enabled boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  last_correlation_id text NOT NULL CHECK (char_length(last_correlation_id) BETWEEN 1 AND 128),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,provider_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id),
  CHECK (provider_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
  CHECK (active_revision IS NULL OR active_revision >= 1),
  CHECK (NOT enabled OR active_revision IS NOT NULL)
);

CREATE TABLE settings_identity_provider_revisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision >= 1),
  protocol text NOT NULL CHECK (protocol = 'oidc'),
  display_name text NOT NULL CHECK (display_name=btrim(display_name) AND char_length(display_name) BETWEEN 1 AND 160),
  issuer_url text NOT NULL CHECK (issuer_url=btrim(issuer_url) AND char_length(issuer_url) BETWEEN 9 AND 2048),
  client_id text NOT NULL CHECK (client_id=btrim(client_id) AND char_length(client_id) BETWEEN 1 AND 256),
  callback_url text NOT NULL CHECK (callback_url=btrim(callback_url) AND char_length(callback_url) BETWEEN 9 AND 2048),
  client_secret_reference text,
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,provider_id,revision),
  FOREIGN KEY (organization_id,workspace_id,provider_id) REFERENCES settings_identity_providers (organization_id,workspace_id,provider_id),
  FOREIGN KEY (client_secret_reference,organization_id,workspace_id) REFERENCES secret_references (reference,organization_id,workspace_id)
);

ALTER TABLE settings_identity_providers
  ADD CONSTRAINT settings_identity_providers_current_revision_fk
    FOREIGN KEY (organization_id,workspace_id,provider_id,current_revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision)
    DEFERRABLE INITIALLY DEFERRED,
  ADD CONSTRAINT settings_identity_providers_active_revision_fk
    FOREIGN KEY (organization_id,workspace_id,provider_id,active_revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE settings_identity_provider_validations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  provider_id text NOT NULL,
  revision bigint NOT NULL,
  status text NOT NULL CHECK (status IN ('valid','invalid')),
  reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  metadata_digest text CHECK (metadata_digest IS NULL OR metadata_digest ~ '^[0-9a-f]{64}$'),
  issuer_url text,
  authorization_url text,
  token_url text,
  jwks_url text,
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
  checked_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,provider_id,revision)
    REFERENCES settings_identity_provider_revisions (organization_id,workspace_id,provider_id,revision),
  UNIQUE (organization_id,workspace_id,provider_id,correlation_id),
  CHECK ((status='valid' AND reason_code='validated' AND metadata_digest IS NOT NULL AND issuer_url IS NOT NULL AND authorization_url IS NOT NULL AND token_url IS NOT NULL AND jwks_url IS NOT NULL)
      OR (status='invalid' AND metadata_digest IS NULL AND issuer_url IS NULL AND authorization_url IS NULL AND token_url IS NULL AND jwks_url IS NULL))
);

CREATE INDEX settings_identity_providers_list_idx ON settings_identity_providers (organization_id,workspace_id,provider_id);
CREATE INDEX settings_identity_provider_validations_latest_idx ON settings_identity_provider_validations (organization_id,workspace_id,provider_id,revision,checked_at DESC,id DESC);

ALTER TABLE settings_identity_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_providers_tenant_all ON settings_identity_providers
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_identity_provider_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_provider_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_provider_revisions_tenant_all ON settings_identity_provider_revisions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_identity_provider_validations ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_provider_validations FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_provider_validations_tenant_all ON settings_identity_provider_validations
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON settings_identity_providers FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON settings_identity_provider_revisions,settings_identity_provider_validations FROM PUBLIC;

CREATE FUNCTION settings_identity_provider_reject_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''identity provider evidence is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER settings_identity_provider_revisions_no_update_delete BEFORE UPDATE OR DELETE ON settings_identity_provider_revisions FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_revisions_no_clear BEFORE TRUNCATE ON settings_identity_provider_revisions FOR EACH STATEMENT EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_validations_no_update_delete BEFORE UPDATE OR DELETE ON settings_identity_provider_validations FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();
CREATE TRIGGER settings_identity_provider_validations_no_clear BEFORE TRUNCATE ON settings_identity_provider_validations FOR EACH STATEMENT EXECUTE FUNCTION settings_identity_provider_reject_evidence_mutation();

CREATE FUNCTION settings_identity_provider_head_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.provider_id<>OLD.provider_id OR NEW.version<>OLD.version+1 OR NEW.current_revision<OLD.current_revision OR NEW.current_revision>OLD.current_revision+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''identity provider transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER settings_identity_provider_head_guard BEFORE UPDATE ON settings_identity_providers FOR EACH ROW EXECUTE FUNCTION settings_identity_provider_head_transition();

COMMENT ON TABLE settings_identity_provider_revisions IS 'Immutable provider-neutral OIDC configuration revisions. VK and other providers are labels/configuration, never Core branches.';
COMMENT ON COLUMN settings_identity_provider_revisions.client_secret_reference IS 'Opaque SecretProvider reference; client secret plaintext is forbidden.';
COMMENT ON TABLE settings_identity_provider_validations IS 'Append-only bounded discovery validation evidence; provider response bodies are not stored.';

-- SOURCE 000066_connector_oauth_sessions.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE secret_references DROP CONSTRAINT secret_references_class_chk;
ALTER TABLE secret_references ADD CONSTRAINT secret_references_class_chk CHECK (class IN (
  'connector_token','oauth_client','oauth_state','oauth_refresh','erp_credential',
  'webhook_signing','certificate','storage_credential'
));

CREATE TABLE connector_oauth_sessions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL CHECK (account_version >= 1),
  actor_id text NOT NULL CHECK (actor_id=btrim(actor_id) AND char_length(actor_id) BETWEEN 1 AND 512),
  state_sha256 text NOT NULL CHECK (state_sha256 ~ '^[0-9a-f]{64}$'),
  pending_secret_reference text NOT NULL,
  callback_url text NOT NULL CHECK (callback_url=btrim(callback_url) AND char_length(callback_url) BETWEEN 9 AND 2048),
  correlation_id text NOT NULL CHECK (correlation_id=btrim(correlation_id) AND char_length(correlation_id) BETWEEN 1 AND 128),
  status text NOT NULL CHECK (status IN ('pending','consumed')),
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,connector_account_id)
    REFERENCES connector_accounts (organization_id,workspace_id,id),
  FOREIGN KEY (pending_secret_reference,organization_id,workspace_id)
    REFERENCES secret_references (reference,organization_id,workspace_id),
  UNIQUE (organization_id,workspace_id,state_sha256),
  UNIQUE (organization_id,workspace_id,actor_id,correlation_id),
  CHECK (expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
  CHECK ((status='pending' AND consumed_at IS NULL) OR (status='consumed' AND consumed_at IS NOT NULL))
);

CREATE INDEX connector_oauth_sessions_expiry_idx
  ON connector_oauth_sessions (organization_id,workspace_id,status,expires_at);

ALTER TABLE connector_oauth_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_oauth_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_oauth_sessions_tenant_all ON connector_oauth_sessions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON connector_oauth_sessions FROM PUBLIC;

CREATE FUNCTION connector_oauth_session_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id
     OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.account_version<>OLD.account_version
     OR NEW.actor_id<>OLD.actor_id OR NEW.state_sha256<>OLD.state_sha256
     OR NEW.pending_secret_reference<>OLD.pending_secret_reference OR NEW.callback_url<>OLD.callback_url
     OR NEW.correlation_id<>OLD.correlation_id OR NEW.created_at<>OLD.created_at OR NEW.expires_at<>OLD.expires_at
     OR OLD.status<>''pending'' OR NEW.status<>''consumed'' OR OLD.consumed_at IS NOT NULL
     OR NEW.consumed_at IS NULL OR NEW.consumed_at<OLD.created_at OR NEW.consumed_at>OLD.expires_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''connector oauth session transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_oauth_sessions_transition_guard BEFORE UPDATE ON connector_oauth_sessions
  FOR EACH ROW EXECUTE FUNCTION connector_oauth_session_transition();

COMMENT ON TABLE connector_oauth_sessions IS 'One-time OAuth state evidence. Plain state and PKCE verifier exist only in encrypted SecretProvider material.';
COMMENT ON COLUMN connector_oauth_sessions.state_sha256 IS 'SHA-256 digest only; raw OAuth state is forbidden in PostgreSQL.';

-- SOURCE 000067_worker_runtime_dispatch.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Runtime leases are deliberately separate from domain state. They let a pool
-- of workers claim cross-tenant background work without weakening FORCE RLS on
-- the domain tables themselves.
CREATE TABLE worker_runtime_jobs (
  kind text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  item_id text NOT NULL,
  available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  lease_owner text,
  lease_token text,
  lease_until timestamptz,
  attempt_count integer NOT NULL DEFAULT 0,
  last_error_code text,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT worker_runtime_jobs_pkey PRIMARY KEY (kind,organization_id,workspace_id,item_id),
  CONSTRAINT worker_runtime_jobs_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT worker_runtime_jobs_kind_chk CHECK (kind IN ('reconciliation','upload')),
  CONSTRAINT worker_runtime_jobs_item_chk CHECK (item_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT worker_runtime_jobs_lease_chk CHECK ((lease_owner IS NULL AND lease_token IS NULL AND lease_until IS NULL) OR (length(lease_owner) BETWEEN 1 AND 128 AND lease_token ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND lease_until IS NOT NULL)),
  CONSTRAINT worker_runtime_jobs_attempt_chk CHECK (attempt_count BETWEEN 0 AND 1000000),
  CONSTRAINT worker_runtime_jobs_error_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9._-]{0,63}$')
);
CREATE INDEX worker_runtime_jobs_due_idx ON worker_runtime_jobs(kind,available_at,lease_until,updated_at,item_id);
ALTER TABLE worker_runtime_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_runtime_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY worker_runtime_jobs_tenant_all ON worker_runtime_jobs FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

-- Discover only scopes that currently have outbox or webhook work. The caller
-- must immediately re-apply the returned tenant scope before touching rows.
CREATE FUNCTION list_worker_active_scopes(p_limit integer)
RETURNS TABLE(organization_id text, workspace_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker scope batch''; END IF;
  RETURN QUERY
    SELECT x.organization_id,x.workspace_id FROM (
      SELECT o.organization_id,o.workspace_id,MIN(o.created_at) AS due_at
      FROM public.outbox_events o
      WHERE o.published_at IS NULL AND (o.available_at IS NULL OR o.available_at<=clock_timestamp()) AND (o.lease_expires_at IS NULL OR o.lease_expires_at<=clock_timestamp())
      GROUP BY o.organization_id,o.workspace_id
      UNION ALL
      SELECT d.organization_id,d.workspace_id,MIN(d.available_at) AS due_at
      FROM public.webhook_deliveries d
      WHERE d.status IN (''pending'',''inflight'') AND d.available_at<=clock_timestamp() AND (d.lease_expires_at IS NULL OR d.lease_expires_at<=clock_timestamp())
      GROUP BY d.organization_id,d.workspace_id
    ) x
    GROUP BY x.organization_id,x.workspace_id
    ORDER BY MIN(x.due_at),x.organization_id,x.workspace_id
    LIMIT p_limit;
END';

-- Materialize eligible domain rows into a dedicated lease queue, then claim
-- them with SKIP LOCKED. This function exposes IDs and scopes only; domain data
-- remains protected by tenant RLS during execution.
CREATE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker claim'';
  END IF;

  IF p_kind=''reconciliation'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''reconciliation'',r.organization_id,r.workspace_id,r.id,clock_timestamp(),clock_timestamp()
      FROM public.reconciliation_runs r
      WHERE r.status IN (''running'',''interrupted'')
      ON CONFLICT (kind,organization_id,workspace_id,item_id) DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j
      WHERE j.kind=''reconciliation'' AND NOT EXISTS (
        SELECT 1 FROM public.reconciliation_runs r WHERE r.organization_id=j.organization_id AND r.workspace_id=j.workspace_id AND r.id=j.item_id AND r.status IN (''running'',''interrupted'')
      );
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''upload'',u.organization_id,u.workspace_id,u.id,clock_timestamp(),clock_timestamp()
      FROM public.uploads u
      WHERE u.state IN (''quarantined'',''validated'',''scanning'',''clean'')
      ON CONFLICT (kind,organization_id,workspace_id,item_id) DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j
      WHERE j.kind=''upload'' AND NOT EXISTS (
        SELECT 1 FROM public.uploads u WHERE u.organization_id=j.organization_id AND u.workspace_id=j.workspace_id AND u.id=j.item_id AND u.state IN (''quarantined'',''validated'',''scanning'',''clean'')
      );
  END IF;

  RETURN QUERY
  WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id
    FROM public.worker_runtime_jobs j
    WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp())
    ORDER BY j.available_at,j.updated_at,j.item_id
    FOR UPDATE SKIP LOCKED LIMIT p_batch
  )
  UPDATE public.worker_runtime_jobs j
  SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp()
  FROM due
  WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id
  RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release'';
  END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp()
  WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT;
  RETURN changed=1;
END';

CREATE FUNCTION complete_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  DELETE FROM public.worker_runtime_jobs
  WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT;
  RETURN changed=1;
END';

REVOKE DELETE, TRUNCATE ON worker_runtime_jobs FROM PUBLIC;

-- SOURCE 000068_connector_runtime_config.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Host-owned, non-secret provider configuration used by the production
-- connector runtime. Credentials remain exclusively in SecretProvider.
CREATE TABLE connector_runtime_configs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  config jsonb NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT connector_runtime_configs_account_fk FOREIGN KEY (organization_id, workspace_id, connector_account_id)
    REFERENCES connector_accounts (organization_id, workspace_id, id),
  CONSTRAINT connector_runtime_configs_version_chk CHECK (version >= 1),
  CONSTRAINT connector_runtime_configs_size_chk CHECK (octet_length(config::text) BETWEEN 2 AND 32768),
  -- Defense in depth: secrets/API keys must never be persisted in this table.
  CONSTRAINT connector_runtime_configs_nonsecret_chk CHECK (
    config::text !~* '"(password|secret|token|api[_-]?key|access[_-]?key|consumer[_-]?(key|secret)|private[_-]?key|authorization)"[[:space:]]*:'
  ),
  CONSTRAINT connector_runtime_configs_timestamps_chk CHECK (updated_at >= created_at),
  PRIMARY KEY (organization_id, workspace_id, connector_account_id)
);

ALTER TABLE connector_runtime_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_runtime_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_runtime_configs_tenant_select ON connector_runtime_configs FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_runtime_configs_tenant_insert ON connector_runtime_configs FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_runtime_configs_tenant_update ON connector_runtime_configs FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
REVOKE DELETE, TRUNCATE ON connector_runtime_configs FROM PUBLIC;

CREATE FUNCTION connector_runtime_configs_guard_delete() RETURNS trigger
LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector runtime configs are retained; disable the account instead''; RETURN NULL; END';
CREATE TRIGGER connector_runtime_configs_no_delete BEFORE DELETE ON connector_runtime_configs
FOR EACH ROW EXECUTE FUNCTION connector_runtime_configs_guard_delete();

-- SOURCE 000069_connector_health_history.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE connector_health_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  sequence_id bigint GENERATED ALWAYS AS IDENTITY,
  status text NOT NULL CHECK (status IN ('healthy','degraded','unavailable')),
  category text NOT NULL CHECK (category IN ('healthy','configuration_error','authentication_error','rate_limited','remote_unavailable','degraded')),
  reason_code text CHECK (reason_code IS NULL OR (reason_code = btrim(reason_code) AND reason_code ~ '^[a-z][a-z0-9_]{0,63}$')),
  rate_limit_remaining bigint CHECK (rate_limit_remaining IS NULL OR rate_limit_remaining >= 0),
  rate_limit_reset_at timestamptz,
  checked_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, workspace_id, connector_account_id, sequence_id),
  FOREIGN KEY (connector_account_id, organization_id, workspace_id)
    REFERENCES connector_accounts (id, organization_id, workspace_id)
);
CREATE INDEX connector_health_history_recent_idx ON connector_health_history
  (organization_id,workspace_id,connector_account_id,checked_at DESC,sequence_id DESC);

ALTER TABLE connector_health_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_health_history FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_health_history_tenant_all ON connector_health_history
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE UPDATE, DELETE, TRUNCATE ON connector_health_history FROM PUBLIC;

COMMENT ON TABLE connector_health_history IS 'Bounded operational connector-health evidence. Stores normalized codes only; raw provider responses, credentials and PII are forbidden.';

-- SOURCE 000070_notification_destinations.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE notification_preferences DROP CONSTRAINT notification_preferences_channel_chk;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_channel_chk CHECK (channel IN ('web_ui','webhook','email','sms','chat'));

CREATE TABLE notification_destinations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recipient_id text NOT NULL,
  channel text NOT NULL CHECK (channel IN ('email','chat')),
  destination_secret_reference text NOT NULL CHECK (destination_secret_reference ~ '^sec:v1:[0-9a-f]{32}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,recipient_id,channel),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id)
);
ALTER TABLE notification_destinations ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_destinations FORCE ROW LEVEL SECURITY;
CREATE POLICY notification_destinations_tenant_all ON notification_destinations
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE, TRUNCATE ON notification_destinations FROM PUBLIC;
COMMENT ON TABLE notification_destinations IS 'Opaque references to encrypted notification destinations. Raw email/chat identifiers are forbidden in this table.';

-- SOURCE 000071_privacy_worker_execution.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE worker_runtime_jobs DROP CONSTRAINT worker_runtime_jobs_kind_chk;
ALTER TABLE worker_runtime_jobs ADD CONSTRAINT worker_runtime_jobs_kind_chk CHECK (kind IN ('reconciliation','upload','privacy'));

CREATE OR REPLACE FUNCTION workspace_members_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.invitation_key IS DISTINCT FROM OLD.invitation_key OR NEW.invited_at IS DISTINCT FROM OLD.invited_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member identity is immutable'';
  END IF;
  IF NEW.email IS DISTINCT FROM OLD.email AND current_setting(''app.privacy_execution'',true) <> ''on'' THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member email mutation requires privacy workflow'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member version transition is invalid'';
  END IF;
  RETURN NEW;
END';

CREATE OR REPLACE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker claim'';
  END IF;
  IF p_kind=''reconciliation'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''reconciliation'',r.organization_id,r.workspace_id,r.id,clock_timestamp(),clock_timestamp() FROM public.reconciliation_runs r WHERE r.status IN (''running'',''interrupted'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''reconciliation'' AND NOT EXISTS (SELECT 1 FROM public.reconciliation_runs r WHERE r.organization_id=j.organization_id AND r.workspace_id=j.workspace_id AND r.id=j.item_id AND r.status IN (''running'',''interrupted''));
  ELSIF p_kind=''upload'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''upload'',u.organization_id,u.workspace_id,u.id,clock_timestamp(),clock_timestamp() FROM public.uploads u WHERE u.state IN (''quarantined'',''validated'',''scanning'',''clean'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''upload'' AND NOT EXISTS (SELECT 1 FROM public.uploads u WHERE u.organization_id=j.organization_id AND u.workspace_id=j.workspace_id AND u.id=j.item_id AND u.state IN (''quarantined'',''validated'',''scanning'',''clean''));
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''privacy'',p.organization_id,p.workspace_id,p.job_id,clock_timestamp(),clock_timestamp() FROM public.privacy_execution_jobs p WHERE p.status IN (''pending'',''running'',''blocked'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''privacy'' AND NOT EXISTS (SELECT 1 FROM public.privacy_execution_jobs p WHERE p.organization_id=j.organization_id AND p.workspace_id=j.workspace_id AND p.job_id=j.item_id AND p.status IN (''pending'',''running'',''blocked''));
  END IF;
  RETURN QUERY WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id FROM public.worker_runtime_jobs j WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) ORDER BY j.available_at,j.updated_at,j.item_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ) UPDATE public.worker_runtime_jobs j SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp() FROM due WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE OR REPLACE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release''; END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp() WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT; RETURN changed=1;
END';

-- SOURCE 000072_warehouse_operational_failover.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE warehouse_operational_state (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  warehouse_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('active','degraded','unavailable','lost')),
  reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,warehouse_id),
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id)
);
CREATE TABLE warehouse_failover_routes (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  source_warehouse_id text NOT NULL,
  destination_warehouse_id text NOT NULL,
  priority integer NOT NULL CHECK (priority BETWEEN 1 AND 10000),
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,source_warehouse_id,destination_warehouse_id),
  UNIQUE (organization_id,workspace_id,source_warehouse_id,priority),
  FOREIGN KEY (organization_id,workspace_id,source_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,destination_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK (source_warehouse_id<>destination_warehouse_id)
);
CREATE TABLE warehouse_failover_decisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  decision_id text NOT NULL,
  source_warehouse_id text NOT NULL,
  destination_warehouse_id text,
  offer_id text NOT NULL,
  result text NOT NULL CHECK (result IN ('routed','no_eligible_destination')),
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,decision_id),
  FOREIGN KEY (organization_id,workspace_id,source_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id)
);

ALTER TABLE warehouse_operational_state ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_operational_state FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_failover_routes ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_failover_routes FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_failover_decisions ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_failover_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY warehouse_operational_state_tenant_all ON warehouse_operational_state FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_failover_routes_tenant_all ON warehouse_failover_routes FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_failover_decisions_tenant_all ON warehouse_failover_decisions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON warehouse_operational_state,warehouse_failover_routes,warehouse_failover_decisions FROM PUBLIC;
REVOKE UPDATE ON warehouse_failover_decisions FROM PUBLIC;

CREATE OR REPLACE FUNCTION inventory_position_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE offer_status text; warehouse_status text; operational_state text;
BEGIN
  SELECT status INTO offer_status FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.offer_id;
  SELECT status INTO warehouse_status FROM warehouses WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.warehouse_id;
  SELECT state INTO operational_state FROM warehouse_operational_state WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND warehouse_id=NEW.warehouse_id;
  operational_state := COALESCE(operational_state,''active'');
  IF offer_status IS NULL OR warehouse_status IS NULL THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position parent is unavailable''; END IF;
  IF TG_OP = ''INSERT'' THEN
    IF offer_status = ''archived'' OR warehouse_status <> ''active'' OR operational_state IN (''unavailable'',''lost'') THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position requires active operational parents''; END IF;
    IF NEW.version <> 1 OR NEW.on_hand_coefficient <> 0 OR NEW.on_hand_scale <> 0 OR NEW.reserved_coefficient <> 0 OR NEW.reserved_scale <> 0 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position must start zero at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.warehouse_id IS DISTINCT FROM OLD.warehouse_id OR NEW.unit IS DISTINCT FROM OLD.unit OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position identity is immutable''; END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position version transition is invalid''; END IF;
  IF (offer_status = ''archived'' OR warehouse_status <> ''active'' OR operational_state IN (''unavailable'',''lost'')) AND NOT (NEW.on_hand_coefficient = OLD.on_hand_coefficient AND NEW.on_hand_scale = OLD.on_hand_scale AND (NEW.reserved_coefficient::numeric * power(10::numeric, OLD.reserved_scale) <= OLD.reserved_coefficient::numeric * power(10::numeric, NEW.reserved_scale))) THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inactive operational parent permits reservation release only''; END IF;
  RETURN NEW;
END';

COMMENT ON TABLE warehouse_operational_state IS 'Operational health state. LOST/UNAVAILABLE blocks new stock reservations but never fabricates stock movement.';
COMMENT ON TABLE warehouse_failover_routes IS 'Operator-approved failover candidates. Resolver still requires destination ACTIVE/DEGRADED and positive ATP for the same offer.';
COMMENT ON TABLE warehouse_failover_decisions IS 'Append-only evidence of routing decisions; no stock is transferred by this table.';

-- SOURCE 000073_warehouse_incident_automation.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE warehouse_incidents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  incident_id text NOT NULL CHECK (incident_id ~ '^whinc_[a-f0-9]{32}$'),
  warehouse_id text NOT NULL,
  operational_state text NOT NULL CHECK (operational_state IN ('unavailable','lost')),
  reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','processing','completed','needs_attention','resolved')),
  cursor_offer_id text,
  routed_count integer NOT NULL DEFAULT 0 CHECK (routed_count >= 0),
  no_route_count integer NOT NULL DEFAULT 0 CHECK (no_route_count >= 0),
  opened_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,incident_id),
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK ((status IN ('completed','needs_attention','resolved')) = (completed_at IS NOT NULL))
);
CREATE UNIQUE INDEX warehouse_incidents_one_active_idx ON warehouse_incidents(organization_id,workspace_id,warehouse_id) WHERE status IN ('open','processing','needs_attention');
CREATE INDEX warehouse_incidents_status_idx ON warehouse_incidents(organization_id,workspace_id,status,updated_at,incident_id);

CREATE TABLE warehouse_incident_decisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  incident_id text NOT NULL,
  offer_id text NOT NULL,
  destination_warehouse_id text,
  result text NOT NULL CHECK (result IN ('routed','no_eligible_destination')),
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,incident_id,offer_id),
  FOREIGN KEY (organization_id,workspace_id,incident_id) REFERENCES warehouse_incidents(organization_id,workspace_id,incident_id),
  FOREIGN KEY (organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,destination_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK ((result='routed') = (destination_warehouse_id IS NOT NULL))
);

ALTER TABLE warehouse_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incidents FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incident_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE warehouse_incident_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY warehouse_incidents_tenant_all ON warehouse_incidents FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_incident_decisions_tenant_all ON warehouse_incident_decisions FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON warehouse_incidents,warehouse_incident_decisions FROM PUBLIC;
REVOKE UPDATE ON warehouse_incident_decisions FROM PUBLIC;

ALTER TABLE worker_runtime_jobs DROP CONSTRAINT worker_runtime_jobs_kind_chk;
ALTER TABLE worker_runtime_jobs ADD CONSTRAINT worker_runtime_jobs_kind_chk CHECK (kind IN ('reconciliation','upload','privacy','warehouse_incident'));

CREATE OR REPLACE FUNCTION claim_worker_runtime_jobs(p_kind text,p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(kind text,organization_id text,workspace_id text,item_id text,lease_token text,lease_until timestamptz,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'') OR length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 1000 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker claim'';
  END IF;
  IF p_kind=''reconciliation'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''reconciliation'',r.organization_id,r.workspace_id,r.id,clock_timestamp(),clock_timestamp() FROM public.reconciliation_runs r WHERE r.status IN (''running'',''interrupted'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''reconciliation'' AND NOT EXISTS (SELECT 1 FROM public.reconciliation_runs r WHERE r.organization_id=j.organization_id AND r.workspace_id=j.workspace_id AND r.id=j.item_id AND r.status IN (''running'',''interrupted''));
  ELSIF p_kind=''upload'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''upload'',u.organization_id,u.workspace_id,u.id,clock_timestamp(),clock_timestamp() FROM public.uploads u WHERE u.state IN (''quarantined'',''validated'',''scanning'',''clean'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''upload'' AND NOT EXISTS (SELECT 1 FROM public.uploads u WHERE u.organization_id=j.organization_id AND u.workspace_id=j.workspace_id AND u.id=j.item_id AND u.state IN (''quarantined'',''validated'',''scanning'',''clean''));
  ELSIF p_kind=''privacy'' THEN
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''privacy'',p.organization_id,p.workspace_id,p.job_id,clock_timestamp(),clock_timestamp() FROM public.privacy_execution_jobs p WHERE p.status IN (''pending'',''running'',''blocked'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''privacy'' AND NOT EXISTS (SELECT 1 FROM public.privacy_execution_jobs p WHERE p.organization_id=j.organization_id AND p.workspace_id=j.workspace_id AND p.job_id=j.item_id AND p.status IN (''pending'',''running'',''blocked''));
  ELSE
    INSERT INTO public.worker_runtime_jobs(kind,organization_id,workspace_id,item_id,available_at,updated_at)
      SELECT ''warehouse_incident'',i.organization_id,i.workspace_id,i.incident_id,clock_timestamp(),clock_timestamp() FROM public.warehouse_incidents i WHERE i.status IN (''open'',''processing'') ON CONFLICT DO NOTHING;
    DELETE FROM public.worker_runtime_jobs j WHERE j.kind=''warehouse_incident'' AND NOT EXISTS (SELECT 1 FROM public.warehouse_incidents i WHERE i.organization_id=j.organization_id AND i.workspace_id=j.workspace_id AND i.incident_id=j.item_id AND i.status IN (''open'',''processing''));
  END IF;
  RETURN QUERY WITH due AS (
    SELECT j.kind,j.organization_id,j.workspace_id,j.item_id FROM public.worker_runtime_jobs j WHERE j.kind=p_kind AND j.available_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) ORDER BY j.available_at,j.updated_at,j.item_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ) UPDATE public.worker_runtime_jobs j SET lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt_count=j.attempt_count+1,updated_at=clock_timestamp() FROM due WHERE j.kind=due.kind AND j.organization_id=due.organization_id AND j.workspace_id=due.workspace_id AND j.item_id=due.item_id RETURNING j.kind,j.organization_id,j.workspace_id,j.item_id,j.lease_token,j.lease_until,j.attempt_count;
END';

CREATE OR REPLACE FUNCTION release_worker_runtime_job(p_kind text,p_organization_id text,p_workspace_id text,p_item_id text,p_token text,p_delay_seconds integer,p_error_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'DECLARE changed integer; BEGIN
  IF p_kind NOT IN (''reconciliation'',''upload'',''privacy'',''warehouse_incident'') OR p_delay_seconds NOT BETWEEN 0 AND 86400 OR (p_error_code IS NOT NULL AND p_error_code !~ ''^[a-z][a-z0-9._-]{0,63}$'') THEN RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid worker release''; END IF;
  UPDATE public.worker_runtime_jobs SET lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>p_delay_seconds),last_error_code=p_error_code,updated_at=clock_timestamp() WHERE kind=p_kind AND organization_id=p_organization_id AND workspace_id=p_workspace_id AND item_id=p_item_id AND lease_token=p_token;
  GET DIAGNOSTICS changed = ROW_COUNT; RETURN changed=1;
END';

COMMENT ON TABLE warehouse_incidents IS 'Persistent automation jobs for warehouse UNAVAILABLE/LOST transitions; no row represents stock transfer.';
COMMENT ON TABLE warehouse_incident_decisions IS 'Append-only per-offer failover evidence for one warehouse incident.';

-- SOURCE 000074_fulfillment_failover_execution.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fulfillment_allocations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  allocation_id text NOT NULL,
  idempotency_key text NOT NULL,
  order_id text NOT NULL,
  order_item_id text NOT NULL,
  offer_id text NOT NULL,
  warehouse_id text NOT NULL,
  quantity_coefficient bigint NOT NULL CHECK(quantity_coefficient>0),
  quantity_scale smallint NOT NULL CHECK(quantity_scale BETWEEN 0 AND 9),
  unit text NOT NULL CHECK(unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  state text NOT NULL CHECK(state IN ('reserved','released','consumed','cancelled')),
  reason_code text NOT NULL DEFAULT '',
  incident_id text,
  replaces_allocation_id text,
  version bigint NOT NULL DEFAULT 1 CHECK(version>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(organization_id,workspace_id,allocation_id),
  UNIQUE(organization_id,workspace_id,idempotency_key),
  FOREIGN KEY(organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,order_item_id) REFERENCES order_items(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,incident_id) REFERENCES warehouse_incidents(organization_id,workspace_id,incident_id),
  FOREIGN KEY(organization_id,workspace_id,replaces_allocation_id) REFERENCES fulfillment_allocations(organization_id,workspace_id,allocation_id),
  CHECK(quantity_scale=0 OR quantity_coefficient % 10 <> 0),
  CHECK(reason_code='' OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CHECK(replaces_allocation_id IS NULL OR replaces_allocation_id<>allocation_id),
  CHECK(updated_at>=created_at)
);
CREATE UNIQUE INDEX fulfillment_allocations_one_reserved_item_idx
  ON fulfillment_allocations(organization_id,workspace_id,order_item_id)
  WHERE state='reserved';
CREATE INDEX fulfillment_allocations_warehouse_offer_idx
  ON fulfillment_allocations(organization_id,workspace_id,warehouse_id,offer_id,state,order_item_id);
CREATE INDEX fulfillment_allocations_incident_idx
  ON fulfillment_allocations(organization_id,workspace_id,incident_id,created_at,allocation_id)
  WHERE incident_id IS NOT NULL;

CREATE FUNCTION fulfillment_allocations_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE
  item_order text; item_offer text; item_q bigint; item_s smallint; item_unit text; order_state text;
BEGIN
  SELECT i.order_id,i.offer_id,i.quantity_coefficient,i.quantity_scale,i.unit,o.status
    INTO item_order,item_offer,item_q,item_s,item_unit,order_state
    FROM order_items i JOIN orders o ON o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id AND o.id=i.order_id
    WHERE i.organization_id=NEW.organization_id AND i.workspace_id=NEW.workspace_id AND i.id=NEW.order_item_id;
  IF item_order IS NULL OR item_order<>NEW.order_id OR item_offer<>NEW.offer_id OR item_q<>NEW.quantity_coefficient OR item_s<>NEW.quantity_scale OR item_unit<>NEW.unit THEN
    RAISE EXCEPTION USING ERRCODE=''23514'',MESSAGE=''fulfillment allocation must exactly match immutable order item'';
  END IF;
  IF TG_OP=''INSERT'' THEN
    IF NEW.state<>''reserved'' OR NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new fulfillment allocation must start reserved at version 1''; END IF;
    IF order_state IN (''fulfilled'',''cancelled'') THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''terminal order cannot receive allocation''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.allocation_id IS DISTINCT FROM OLD.allocation_id OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR NEW.order_id IS DISTINCT FROM OLD.order_id OR NEW.order_item_id IS DISTINCT FROM OLD.order_item_id OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.warehouse_id IS DISTINCT FROM OLD.warehouse_id OR NEW.quantity_coefficient IS DISTINCT FROM OLD.quantity_coefficient OR NEW.quantity_scale IS DISTINCT FROM OLD.quantity_scale OR NEW.unit IS DISTINCT FROM OLD.unit OR NEW.incident_id IS DISTINCT FROM OLD.incident_id OR NEW.replaces_allocation_id IS DISTINCT FROM OLD.replaces_allocation_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''fulfillment allocation identity is immutable'';
  END IF;
  IF OLD.state<>''reserved'' OR NEW.state NOT IN (''released'',''consumed'',''cancelled'') OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''fulfillment allocation state transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER fulfillment_allocations_guard_insert BEFORE INSERT ON fulfillment_allocations FOR EACH ROW EXECUTE FUNCTION fulfillment_allocations_guard();
CREATE TRIGGER fulfillment_allocations_guard_update BEFORE UPDATE ON fulfillment_allocations FOR EACH ROW EXECUTE FUNCTION fulfillment_allocations_guard();

ALTER TABLE fulfillment_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE fulfillment_allocations FORCE ROW LEVEL SECURITY;
CREATE POLICY fulfillment_allocations_tenant_all ON fulfillment_allocations FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON fulfillment_allocations FROM PUBLIC;

ALTER TABLE warehouse_incident_decisions
  ADD COLUMN execution_status text NOT NULL DEFAULT 'not_required' CHECK(execution_status IN ('not_required','rerouted','needs_attention')),
  ADD COLUMN execution_reason text NOT NULL DEFAULT '' CHECK(execution_reason='' OR execution_reason IN ('untracked_reservation','insufficient_capacity','allocation_conflict')),
  ADD COLUMN rerouted_allocations integer NOT NULL DEFAULT 0 CHECK(rerouted_allocations>=0);
ALTER TABLE warehouse_incidents
  ADD COLUMN rerouted_allocation_count integer NOT NULL DEFAULT 0 CHECK(rerouted_allocation_count>=0),
  ADD COLUMN execution_attention_count integer NOT NULL DEFAULT 0 CHECK(execution_attention_count>=0);

COMMENT ON TABLE fulfillment_allocations IS 'Durable order-item reservation ownership. Warehouse failover releases the source allocation and creates a new destination allocation; physical on-hand stock is never transferred by this table.';
COMMENT ON COLUMN fulfillment_allocations.replaces_allocation_id IS 'Immutable lineage pointer used by failover rerouting. A reroute creates a new allocation rather than rewriting warehouse identity.';
COMMENT ON COLUMN warehouse_incident_decisions.execution_status IS 'Execution evidence for tracked reservations. needs_attention is fail-closed and never fabricates a destination reservation.';
-- BASELINE_SOURCE_END

INSERT INTO migration_history (
  version, name, file_name, phase, risk, checksum_sha256,
  application_version, execution_id, duration_ms
) VALUES (
  current_setting('torgnexa.migration_version')::integer,
  current_setting('torgnexa.migration_name'),
  current_setting('torgnexa.migration_file'),
  current_setting('torgnexa.migration_phase'),
  current_setting('torgnexa.migration_risk'),
  current_setting('torgnexa.migration_checksum'),
  current_setting('torgnexa.application_version'),
  current_setting('torgnexa.migration_execution_id'),
  current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
