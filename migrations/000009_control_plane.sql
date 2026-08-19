BEGIN;

-- TORGNEXA pre-v1 baseline component 000009: control_plane.
-- Squashed, statement-order-preserving source range: legacy 000055..000063.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000055_connector_account_settings.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 105 aligns the database family vocabulary with the already reviewed
-- provider-neutral CRM SDK family. No provider id is admitted by this check.
ALTER TABLE connector_accounts DROP CONSTRAINT connector_accounts_family_v1_chk;
ALTER TABLE connector_accounts ADD CONSTRAINT connector_accounts_family_v1_chk CHECK (
  family IN ('marketplace','classified','social','erp','edo','government','payment','logistics','pickup','fx','notification','crm')
);

-- Community development needs one explicit tenant target for the local OIDC
-- administrator. Production tenant scope must come from reviewed OIDC claims;
-- these stable synthetic ids are not a production fallback.
INSERT INTO organizations (id, name)
VALUES ('0198b8d0-0000-7000-8000-000000000001', 'TORGNEXA Community')
ON CONFLICT (id) DO NOTHING;
INSERT INTO workspaces (id, organization_id, name)
VALUES ('0198b8d0-0000-7000-8000-000000000002', '0198b8d0-0000-7000-8000-000000000001', 'Community workspace')
ON CONFLICT (id) DO NOTHING;

-- SOURCE 000056_demo_dataset_tombstone.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE demo_dataset_tombstones (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  deleted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id, id) ON DELETE RESTRICT
);
ALTER TABLE demo_dataset_tombstones ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_dataset_tombstones FORCE ROW LEVEL SECURITY;
CREATE POLICY demo_dataset_tombstones_tenant_select ON demo_dataset_tombstones FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_insert ON demo_dataset_tombstones FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_update ON demo_dataset_tombstones FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY demo_dataset_tombstones_tenant_delete ON demo_dataset_tombstones FOR DELETE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
COMMENT ON TABLE demo_dataset_tombstones IS 'Tenant-scoped logical removal of synthetic demo data; immutable order history is retained.';

-- SOURCE 000057_catalog_product_images.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE catalog_product_images (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  url text NOT NULL CHECK (char_length(url) <= 2039 AND url ~ '^https://[^[:space:]]+$'),
  alt_text text NOT NULL DEFAULT '' CHECK (alt_text = btrim(alt_text) AND char_length(alt_text) <= 300),
  position smallint NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 255),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT catalog_product_images_product_fk FOREIGN KEY (organization_id, workspace_id, product_id)
    REFERENCES products (organization_id, workspace_id, id) ON DELETE RESTRICT,
  UNIQUE (organization_id, workspace_id, product_id, url)
);

CREATE INDEX catalog_product_images_product_idx
  ON catalog_product_images (organization_id, workspace_id, product_id, position, id);

ALTER TABLE catalog_product_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_product_images FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_product_images_tenant_all ON catalog_product_images
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON catalog_product_images FROM PUBLIC;

CREATE FUNCTION catalog_product_images_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new product image must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.product_id IS DISTINCT FROM OLD.product_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''product image identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''product image version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER catalog_product_images_guard_insert BEFORE INSERT ON catalog_product_images FOR EACH ROW EXECUTE FUNCTION catalog_product_images_guard();
CREATE TRIGGER catalog_product_images_guard_update BEFORE UPDATE ON catalog_product_images FOR EACH ROW EXECUTE FUNCTION catalog_product_images_guard();

-- SOURCE 000058_workspace_members.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workspace_members (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  email text NOT NULL CHECK (email = lower(btrim(email)) AND char_length(email) BETWEEN 3 AND 254),
  display_name text NOT NULL DEFAULT '' CHECK (display_name = btrim(display_name) AND char_length(display_name) <= 160),
  oidc_subject text CHECK (oidc_subject IS NULL OR (oidc_subject = btrim(oidc_subject) AND char_length(oidc_subject) BETWEEN 1 AND 255)),
  role_code text NOT NULL CHECK (role_code IN ('admin','manager','operator','viewer')),
  status text NOT NULL CHECK (status IN ('invited','active','disabled')),
  invitation_key text NOT NULL CHECK (char_length(invitation_key) BETWEEN 1 AND 128),
  last_mutation_key text CHECK (last_mutation_key IS NULL OR char_length(last_mutation_key) BETWEEN 1 AND 128),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  invited_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces (organization_id, id),
  UNIQUE (organization_id, workspace_id, email),
  UNIQUE (organization_id, workspace_id, invitation_key),
  UNIQUE (organization_id, workspace_id, oidc_subject)
);
CREATE INDEX workspace_members_page_idx ON workspace_members (organization_id, workspace_id, id);
CREATE INDEX workspace_members_admin_idx ON workspace_members (organization_id, workspace_id, role_code, status);

ALTER TABLE workspace_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_members FORCE ROW LEVEL SECURITY;
CREATE POLICY workspace_members_tenant_all ON workspace_members
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE, TRUNCATE ON workspace_members FROM PUBLIC;

CREATE FUNCTION workspace_members_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.email IS DISTINCT FROM OLD.email OR NEW.invitation_key IS DISTINCT FROM OLD.invitation_key OR NEW.invited_at IS DISTINCT FROM OLD.invited_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''member version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER workspace_members_guard_insert BEFORE INSERT ON workspace_members FOR EACH ROW EXECUTE FUNCTION workspace_members_guard();
CREATE TRIGGER workspace_members_guard_update BEFORE UPDATE ON workspace_members FOR EACH ROW EXECUTE FUNCTION workspace_members_guard();

-- SOURCE 000059_notification_preferences_ui.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE notification_preferences DROP CONSTRAINT notification_preferences_channel_chk;
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_channel_chk CHECK (channel IN ('web_ui','webhook','email','sms'));
ALTER TABLE notification_preferences ADD COLUMN categories jsonb NOT NULL DEFAULT '["commerce","inventory","integrations","compliance","security","system"]'::jsonb;
ALTER TABLE notification_preferences ADD COLUMN quiet_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE notification_preferences ADD COLUMN quiet_start text NOT NULL DEFAULT '22:00' CHECK (quiet_start ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$');
ALTER TABLE notification_preferences ADD COLUMN quiet_end text NOT NULL DEFAULT '08:00' CHECK (quiet_end ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$');
ALTER TABLE notification_preferences ADD COLUMN timezone text NOT NULL DEFAULT 'Europe/Moscow' CHECK (timezone = btrim(timezone) AND char_length(timezone) BETWEEN 1 AND 64);
ALTER TABLE notification_preferences ADD CONSTRAINT notification_preferences_categories_chk CHECK (jsonb_typeof(categories)='array' AND jsonb_array_length(categories) BETWEEN 1 AND 6 AND categories <@ '["commerce","inventory","integrations","compliance","security","system"]'::jsonb);

-- SOURCE 000060_connector_account_capabilities.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 107 stores immutable, complete capability snapshots. No row for an
-- account means default deny; a later revision never rewrites prior evidence.
CREATE TABLE connector_account_capability_history (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL,
  capability text NOT NULL,
  direction text NOT NULL,
  risk_class text NOT NULL,
  approval_required boolean NOT NULL,
  enabled boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT connector_account_capability_history_pkey PRIMARY KEY (
    organization_id, workspace_id, connector_account_id, account_version, capability
  ),
  CONSTRAINT connector_account_capability_history_account_fk FOREIGN KEY (
    connector_account_id, organization_id, workspace_id
  ) REFERENCES connector_accounts (id, organization_id, workspace_id),
  CONSTRAINT connector_account_capability_history_version_chk CHECK (account_version >= 2),
  CONSTRAINT connector_account_capability_history_name_chk CHECK (
    capability ~ '^[a-z][a-z0-9._-]{0,127}$'
  ),
  CONSTRAINT connector_account_capability_history_policy_chk CHECK (
    (direction = 'read' AND risk_class = 'read' AND approval_required = false)
    OR
    (direction = 'write' AND risk_class = 'write_sensitive' AND approval_required = true)
  )
);

CREATE INDEX connector_account_capability_history_current_idx
  ON connector_account_capability_history (
    organization_id, workspace_id, connector_account_id, account_version DESC, capability
  );

ALTER TABLE connector_account_capability_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_account_capability_history FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_account_capability_history_tenant_isolation
  ON connector_account_capability_history
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

REVOKE UPDATE, DELETE, TRUNCATE ON connector_account_capability_history FROM PUBLIC;

CREATE FUNCTION connector_account_capabilities_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account capability history is append-only'';
  RETURN NULL;
END';

CREATE TRIGGER connector_account_capability_history_no_update
  BEFORE UPDATE ON connector_account_capability_history
  FOR EACH ROW EXECUTE FUNCTION connector_account_capabilities_reject_mutation();
CREATE TRIGGER connector_account_capability_history_no_delete
  BEFORE DELETE ON connector_account_capability_history
  FOR EACH ROW EXECUTE FUNCTION connector_account_capabilities_reject_mutation();
CREATE TRIGGER connector_account_capability_history_no_clear
  BEFORE TRUNCATE ON connector_account_capability_history
  FOR EACH STATEMENT EXECUTE FUNCTION connector_account_capabilities_reject_mutation();

COMMENT ON TABLE connector_account_capability_history IS
  'Append-only tenant-scoped account capability snapshots; absence means default deny.';
COMMENT ON COLUMN connector_account_capability_history.approval_required IS
  'Host policy classification. Every remote write requires approval before execution.';

-- SOURCE 000061_settings_security.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 103 deliberately stores irreversible references only. Raw OIDC sid/sub,
-- bearer tokens, IP addresses and User-Agent strings never enter these tables.
CREATE TABLE settings_identity_sessions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  session_ref text NOT NULL,
  subject_ref text NOT NULL,
  status text NOT NULL,
  client_kind text NOT NULL,
  authenticated_at timestamptz NOT NULL,
  first_seen_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CONSTRAINT settings_identity_sessions_pkey PRIMARY KEY (organization_id,workspace_id,session_ref),
  CONSTRAINT settings_identity_sessions_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id),
  CONSTRAINT settings_identity_sessions_ref_chk CHECK (session_ref ~ '^[0-9a-f]{64}$' AND subject_ref ~ '^[0-9a-f]{64}$'),
  CONSTRAINT settings_identity_sessions_status_chk CHECK (status IN ('active','revoked')),
  CONSTRAINT settings_identity_sessions_client_chk CHECK (client_kind IN ('browser','mobile','api','unknown')),
  CONSTRAINT settings_identity_sessions_time_chk CHECK (authenticated_at <= first_seen_at AND first_seen_at <= last_seen_at AND expires_at > authenticated_at),
  CONSTRAINT settings_identity_sessions_revoke_chk CHECK ((status='active' AND revoked_at IS NULL) OR (status='revoked' AND revoked_at IS NOT NULL))
);
CREATE INDEX settings_identity_sessions_recent_idx ON settings_identity_sessions (organization_id,workspace_id,last_seen_at DESC,session_ref DESC);

CREATE TABLE settings_login_events (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  session_ref text NOT NULL,
  event_type text NOT NULL,
  client_kind text NOT NULL,
  occurred_at timestamptz NOT NULL,
  CONSTRAINT settings_login_events_pkey PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT settings_login_events_session_fk FOREIGN KEY (organization_id,workspace_id,session_ref) REFERENCES settings_identity_sessions (organization_id,workspace_id,session_ref),
  CONSTRAINT settings_login_events_unique_kind UNIQUE (organization_id,workspace_id,session_ref,event_type),
  CONSTRAINT settings_login_events_id_chk CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  CONSTRAINT settings_login_events_type_chk CHECK (event_type IN ('session_observed','session_revoked')),
  CONSTRAINT settings_login_events_client_chk CHECK (client_kind IN ('browser','mobile','api','unknown'))
);
CREATE INDEX settings_login_events_recent_idx ON settings_login_events (organization_id,workspace_id,id DESC);

ALTER TABLE settings_identity_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_identity_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_identity_sessions_tenant_isolation ON settings_identity_sessions
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE settings_login_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings_login_events FORCE ROW LEVEL SECURITY;
CREATE POLICY settings_login_events_tenant_isolation ON settings_login_events
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON settings_identity_sessions FROM PUBLIC;
REVOKE UPDATE,DELETE,TRUNCATE ON settings_login_events FROM PUBLIC;

CREATE FUNCTION settings_identity_sessions_enforce_transition() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.session_ref<>OLD.session_ref OR NEW.subject_ref<>OLD.subject_ref OR NEW.client_kind<>OLD.client_kind OR NEW.authenticated_at<>OLD.authenticated_at OR NEW.first_seen_at<>OLD.first_seen_at OR NEW.last_seen_at<OLD.last_seen_at OR NEW.expires_at<OLD.expires_at OR OLD.status=''revoked'' OR (OLD.status=''active'' AND NEW.status NOT IN (''active'',''revoked'')) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid identity session transition'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER settings_identity_sessions_transition BEFORE UPDATE ON settings_identity_sessions FOR EACH ROW EXECUTE FUNCTION settings_identity_sessions_enforce_transition();

CREATE FUNCTION settings_security_reject_event_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''settings login events are append-only''; RETURN NULL; END';
CREATE TRIGGER settings_login_events_no_update BEFORE UPDATE ON settings_login_events FOR EACH ROW EXECUTE FUNCTION settings_security_reject_event_mutation();
CREATE TRIGGER settings_login_events_no_delete BEFORE DELETE ON settings_login_events FOR EACH ROW EXECUTE FUNCTION settings_security_reject_event_mutation();
CREATE TRIGGER settings_login_events_no_clear BEFORE TRUNCATE ON settings_login_events FOR EACH STATEMENT EXECUTE FUNCTION settings_security_reject_event_mutation();

COMMENT ON TABLE settings_identity_sessions IS 'Minimized tenant-scoped OIDC sessions observed by TORGNEXA; application revocation does not claim provider-wide logout.';
COMMENT ON COLUMN settings_identity_sessions.session_ref IS 'SHA-256 of issuer, subject and OIDC sid (or issuance fallback); raw provider identifiers are prohibited.';
COMMENT ON COLUMN settings_identity_sessions.subject_ref IS 'SHA-256 of issuer and OIDC sub; display and logging are prohibited.';
COMMENT ON TABLE settings_login_events IS 'Append-only TORGNEXA-observed session history. Retention class: security evidence, 180 days minimum, policy-controlled thereafter.';

-- SOURCE 000062_connector_bootstrap_schedule.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE connector_bootstrap_previews (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  account_version bigint NOT NULL,
  policy_count integer NOT NULL,
  read_count integer NOT NULL,
  write_count integer NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CONSTRAINT connector_bootstrap_previews_pkey PRIMARY KEY (id),
  CONSTRAINT connector_bootstrap_previews_tenant_identity UNIQUE (id,organization_id,workspace_id),
  CONSTRAINT connector_bootstrap_previews_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_bootstrap_previews_id_chk CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT connector_bootstrap_previews_counts_chk CHECK (account_version >= 1 AND policy_count BETWEEN 1 AND 200 AND read_count BETWEEN 0 AND policy_count AND write_count BETWEEN 0 AND policy_count),
  CONSTRAINT connector_bootstrap_previews_time_chk CHECK (expires_at > created_at AND expires_at <= created_at + interval '1 hour' AND (consumed_at IS NULL OR consumed_at >= created_at))
);

CREATE TABLE connector_sync_schedules (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  mode text NOT NULL,
  interval_minutes integer NOT NULL,
  enabled boolean NOT NULL,
  next_run_at timestamptz,
  last_enqueued_at timestamptz,
  last_job_id text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT connector_sync_schedules_pkey PRIMARY KEY (organization_id,workspace_id,connector_account_id),
  CONSTRAINT connector_sync_schedules_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_schedules_mode_chk CHECK (mode IN ('incremental','scheduled_full')),
  CONSTRAINT connector_sync_schedules_interval_chk CHECK (interval_minutes BETWEEN 15 AND 10080),
  CONSTRAINT connector_sync_schedules_enabled_chk CHECK (enabled = (next_run_at IS NOT NULL)),
  CONSTRAINT connector_sync_schedules_version_chk CHECK (version >= 1 AND updated_at >= created_at),
  CONSTRAINT connector_sync_schedules_job_chk CHECK (last_job_id IS NULL OR last_job_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);

CREATE TABLE connector_sync_jobs (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  kind text NOT NULL,
  mode text NOT NULL,
  status text NOT NULL,
  preview_id text,
  checkpoint_policy_id text,
  started_runs integer NOT NULL DEFAULT 0,
  attempt_count integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5,
  available_at timestamptz NOT NULL,
  lease_owner text,
  lease_token text,
  lease_until timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT connector_sync_jobs_pkey PRIMARY KEY (id),
  CONSTRAINT connector_sync_jobs_tenant_identity UNIQUE (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_account_fk FOREIGN KEY (connector_account_id,organization_id,workspace_id)
    REFERENCES connector_accounts (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_preview_fk FOREIGN KEY (preview_id,organization_id,workspace_id)
    REFERENCES connector_bootstrap_previews (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_checkpoint_fk FOREIGN KEY (checkpoint_policy_id,organization_id,workspace_id)
    REFERENCES sync_policies (id,organization_id,workspace_id),
  CONSTRAINT connector_sync_jobs_kind_chk CHECK (kind IN ('initial_import','scheduled_sync')),
  CONSTRAINT connector_sync_jobs_mode_chk CHECK (mode IN ('incremental','scheduled_full')),
  CONSTRAINT connector_sync_jobs_status_chk CHECK (status IN ('pending','running','retry_wait','completed','failed')),
  CONSTRAINT connector_sync_jobs_preview_kind_chk CHECK ((kind='initial_import') = (preview_id IS NOT NULL)),
  CONSTRAINT connector_sync_jobs_counts_chk CHECK (started_runs BETWEEN 0 AND 200 AND attempt_count BETWEEN 0 AND max_attempts AND max_attempts BETWEEN 1 AND 5),
  CONSTRAINT connector_sync_jobs_lease_chk CHECK ((lease_token IS NULL AND lease_owner IS NULL AND lease_until IS NULL) OR (lease_token ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND length(lease_owner) BETWEEN 1 AND 128 AND lease_until IS NOT NULL)),
  CONSTRAINT connector_sync_jobs_error_chk CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9._-]{0,63}$'),
  CONSTRAINT connector_sync_jobs_time_chk CHECK (updated_at >= created_at AND (started_at IS NULL OR started_at >= created_at) AND (completed_at IS NULL OR (started_at IS NOT NULL AND completed_at >= started_at))),
  CONSTRAINT connector_sync_jobs_state_chk CHECK (((status IN ('completed','failed')) = (completed_at IS NOT NULL)) AND (status='pending' OR started_at IS NOT NULL))
);

CREATE INDEX connector_bootstrap_previews_account_idx ON connector_bootstrap_previews (organization_id,workspace_id,connector_account_id,created_at DESC,id);
CREATE INDEX connector_sync_schedules_due_idx ON connector_sync_schedules (next_run_at,organization_id,workspace_id,connector_account_id) WHERE enabled;
CREATE INDEX connector_sync_jobs_dispatch_idx ON connector_sync_jobs (available_at,created_at,id) WHERE status IN ('pending','retry_wait','running');
CREATE INDEX connector_sync_jobs_account_idx ON connector_sync_jobs (organization_id,workspace_id,connector_account_id,created_at DESC,id);

ALTER TABLE connector_bootstrap_previews ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_bootstrap_previews FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_schedules FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_sync_jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY connector_bootstrap_previews_tenant_all ON connector_bootstrap_previews
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY connector_sync_schedules_tenant_all ON connector_sync_schedules
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY connector_sync_jobs_tenant_all ON connector_sync_jobs
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION connector_bootstrap_preview_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN RETURN NEW; END IF;
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.account_version<>OLD.account_version OR NEW.policy_count<>OLD.policy_count OR NEW.read_count<>OLD.read_count OR NEW.write_count<>OLD.write_count OR NEW.created_at<>OLD.created_at OR NEW.expires_at<>OLD.expires_at OR OLD.consumed_at IS NOT NULL OR NEW.consumed_at IS NULL THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''bootstrap preview evidence is immutable'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_bootstrap_preview_guard_update BEFORE UPDATE ON connector_bootstrap_previews FOR EACH ROW EXECUTE FUNCTION connector_bootstrap_preview_guard();

CREATE FUNCTION connector_sync_schedule_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''schedule must start at version 1''; END IF; RETURN NEW; END IF;
  IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.connector_account_id<>OLD.connector_account_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''invalid schedule transition'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_sync_schedule_guard_insert BEFORE INSERT ON connector_sync_schedules FOR EACH ROW EXECUTE FUNCTION connector_sync_schedule_guard();
CREATE TRIGGER connector_sync_schedule_guard_update BEFORE UPDATE ON connector_sync_schedules FOR EACH ROW EXECUTE FUNCTION connector_sync_schedule_guard();

-- A scheduler must discover work across tenants while ordinary queries remain
-- FORCE-RLS tenant scoped. This narrow definer function only enqueues due rows
-- and leases bounded metadata; the worker reapplies the returned tenant scope.
CREATE FUNCTION claim_connector_sync_jobs(p_worker text,p_token text,p_batch integer,p_lease_seconds integer)
RETURNS TABLE(id text,organization_id text,workspace_id text,connector_account_id text,kind text,mode text,status text,preview_id text,checkpoint_policy_id text,started_runs integer,attempt_count integer,max_attempts integer,available_at timestamptz,created_at timestamptz,updated_at timestamptz,started_at timestamptz,completed_at timestamptz,last_error_code text,lease_token text,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public SET row_security=off AS 'BEGIN
  IF length(p_worker) NOT BETWEEN 1 AND 128 OR p_token !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'' OR p_batch NOT BETWEEN 1 AND 100 OR p_lease_seconds NOT BETWEEN 5 AND 300 THEN
    RAISE EXCEPTION USING ERRCODE=''22023'', MESSAGE=''invalid scheduler claim'';
  END IF;
  WITH due AS (
    SELECT s.organization_id,s.workspace_id,s.connector_account_id,s.mode,s.next_run_at
    FROM connector_sync_schedules s WHERE s.enabled AND s.next_run_at<=clock_timestamp()
    ORDER BY s.next_run_at,s.organization_id,s.workspace_id,s.connector_account_id FOR UPDATE SKIP LOCKED LIMIT p_batch
  ), inserted AS (
    INSERT INTO connector_sync_jobs(id,organization_id,workspace_id,connector_account_id,kind,mode,status,available_at,created_at,updated_at)
    SELECT ''schedule-''||md5(d.organization_id||'':''||d.workspace_id||'':''||d.connector_account_id||'':''||d.next_run_at::text),d.organization_id,d.workspace_id,d.connector_account_id,''scheduled_sync'',d.mode,''pending'',d.next_run_at,clock_timestamp(),clock_timestamp() FROM due d
    ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,connector_account_id
  )
  UPDATE connector_sync_schedules s SET last_enqueued_at=s.next_run_at,last_job_id=''schedule-''||md5(s.organization_id||'':''||s.workspace_id||'':''||s.connector_account_id||'':''||s.next_run_at::text),next_run_at=s.next_run_at+make_interval(mins=>s.interval_minutes),version=s.version+1,updated_at=clock_timestamp()
  FROM due d WHERE s.organization_id=d.organization_id AND s.workspace_id=d.workspace_id AND s.connector_account_id=d.connector_account_id;

  UPDATE connector_sync_jobs j SET status=''failed'',completed_at=clock_timestamp(),updated_at=clock_timestamp(),lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error_code=''attempts_exhausted''
  WHERE j.status=''running'' AND j.lease_until<clock_timestamp() AND j.attempt_count>=j.max_attempts;

  RETURN QUERY WITH candidates AS (
    SELECT j.id FROM connector_sync_jobs j
    WHERE ((j.status IN (''pending'',''retry_wait'') AND j.available_at<=clock_timestamp()) OR (j.status=''running'' AND j.lease_until<clock_timestamp())) AND j.attempt_count<j.max_attempts
    ORDER BY j.available_at,j.created_at,j.id FOR UPDATE SKIP LOCKED LIMIT p_batch
  )
  UPDATE connector_sync_jobs j SET status=''running'',attempt_count=j.attempt_count+1,lease_owner=p_worker,lease_token=p_token,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),started_at=coalesce(j.started_at,clock_timestamp()),completed_at=NULL,updated_at=clock_timestamp(),last_error_code=NULL
  FROM candidates c WHERE j.id=c.id
  RETURNING j.id,j.organization_id,j.workspace_id,j.connector_account_id,j.kind,j.mode,j.status,j.preview_id,j.checkpoint_policy_id,j.started_runs,j.attempt_count,j.max_attempts,j.available_at,j.created_at,j.updated_at,j.started_at,j.completed_at,j.last_error_code,j.lease_token,j.lease_until;
END';

REVOKE ALL ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) FROM PUBLIC;
REVOKE DELETE,TRUNCATE ON connector_bootstrap_previews,connector_sync_schedules,connector_sync_jobs FROM PUBLIC;

COMMENT ON TABLE connector_bootstrap_previews IS 'Task-108 immutable dry-run summaries; metadata only, no remote payloads or credentials; 30 minute authorization evidence.';
COMMENT ON TABLE connector_sync_schedules IS 'Task-108 durable per-account interval schedules with optimistic versions; never browser-local state.';
COMMENT ON TABLE connector_sync_jobs IS 'Task-108 tenant-scoped resumable initial-import and scheduled fan-out jobs. Entity/page cursors remain in reconciliation_runs.';
COMMENT ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) IS 'Bounded cross-tenant scheduler lease boundary; callers must reapply returned tenant scope before processing.';

-- SOURCE 000063_connector_sync_claim_fix.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- RETURNS TABLE output names are PL/pgSQL variables. Prefer qualified SQL
-- columns inside the already-bounded Task-108 claim function so PostgreSQL
-- does not reject its INSERT ... RETURNING clause as ambiguous.
ALTER FUNCTION claim_connector_sync_jobs(text,text,integer,integer)
  SET plpgsql.variable_conflict = 'use_column';

COMMENT ON FUNCTION claim_connector_sync_jobs(text,text,integer,integer) IS 'Bounded cross-tenant scheduler lease boundary; SQL column names take precedence over RETURNS TABLE output variables and callers must reapply returned tenant scope.';
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
