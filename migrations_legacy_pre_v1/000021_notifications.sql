BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE notifications (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recipient_id text NOT NULL,
  dedupe_key text NOT NULL,
  severity text NOT NULL,
  title text NOT NULL,
  body text NOT NULL DEFAULT '',
  entity_type text,
  entity_id text,
  source_event_id text,
  source_event_type text,
  occurrence_count integer NOT NULL DEFAULT 1,
  first_occurred_at timestamptz NOT NULL,
  last_occurred_at timestamptz NOT NULL,
  read_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT notifications_pkey PRIMARY KEY (id),
  CONSTRAINT notifications_tenant_key UNIQUE (id, organization_id, workspace_id),
  CONSTRAINT notifications_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT notifications_dedupe_uniq UNIQUE (organization_id, workspace_id, recipient_id, dedupe_key),
  CONSTRAINT notifications_id_chk CHECK (id = btrim(id) AND char_length(id) BETWEEN 1 AND 128 AND id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_recipient_chk CHECK (recipient_id = btrim(recipient_id) AND char_length(recipient_id) BETWEEN 1 AND 128 AND recipient_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_dedupe_chk CHECK (dedupe_key = btrim(dedupe_key) AND char_length(dedupe_key) BETWEEN 1 AND 200 AND dedupe_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notifications_severity_chk CHECK (severity IN ('info','warning','critical')),
  CONSTRAINT notifications_title_chk CHECK (title=btrim(title) AND char_length(title) BETWEEN 1 AND 200),
  CONSTRAINT notifications_body_chk CHECK (body=btrim(body) AND char_length(body) <= 4000),
  CONSTRAINT notifications_entity_pair_chk CHECK ((entity_type IS NULL)=(entity_id IS NULL)),
  CONSTRAINT notifications_source_pair_chk CHECK ((source_event_id IS NULL)=(source_event_type IS NULL)),
  CONSTRAINT notifications_entity_type_chk CHECK (entity_type IS NULL OR (entity_type=btrim(entity_type) AND char_length(entity_type) BETWEEN 1 AND 128 AND entity_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_entity_id_chk CHECK (entity_id IS NULL OR (entity_id=btrim(entity_id) AND char_length(entity_id) BETWEEN 1 AND 128 AND entity_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_source_event_id_chk CHECK (source_event_id IS NULL OR (source_event_id=btrim(source_event_id) AND char_length(source_event_id) BETWEEN 1 AND 128 AND source_event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$')),
  CONSTRAINT notifications_source_event_type_chk CHECK (source_event_type IS NULL OR (char_length(source_event_type) BETWEEN 8 AND 255 AND source_event_type ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$')),
  CONSTRAINT notifications_occurrence_chk CHECK (occurrence_count >= 1),
  CONSTRAINT notifications_time_chk CHECK (last_occurred_at >= first_occurred_at AND updated_at >= created_at AND (read_at IS NULL OR read_at >= created_at))
);
CREATE INDEX notifications_inbox_idx ON notifications(organization_id,workspace_id,recipient_id,last_occurred_at DESC,id DESC);
CREATE INDEX notifications_unread_idx ON notifications(organization_id,workspace_id,recipient_id,last_occurred_at DESC) WHERE read_at IS NULL;

CREATE TABLE notification_preferences (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  recipient_id text NOT NULL,
  channel text NOT NULL,
  enabled boolean NOT NULL,
  min_severity text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL,
  CONSTRAINT notification_preferences_pkey PRIMARY KEY (organization_id,workspace_id,recipient_id,channel),
  CONSTRAINT notification_preferences_workspace_fk FOREIGN KEY (organization_id,workspace_id)
    REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT notification_preferences_recipient_chk CHECK (recipient_id=btrim(recipient_id) AND char_length(recipient_id) BETWEEN 1 AND 128 AND recipient_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]*$'),
  CONSTRAINT notification_preferences_channel_chk CHECK (channel IN ('web_ui','webhook')),
  CONSTRAINT notification_preferences_severity_chk CHECK (min_severity IN ('info','warning','critical')),
  CONSTRAINT notification_preferences_version_chk CHECK (version >= 1)
);

CREATE TABLE notification_deliveries (
  notification_id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  channel text NOT NULL,
  status text NOT NULL,
  error_code text,
  occurrence integer NOT NULL,
  attempt integer NOT NULL,
  attempted_at timestamptz NOT NULL,
  CONSTRAINT notification_deliveries_pkey PRIMARY KEY (notification_id,channel,occurrence,attempt),
  CONSTRAINT notification_deliveries_notification_fk FOREIGN KEY (notification_id,organization_id,workspace_id)
    REFERENCES notifications(id,organization_id,workspace_id) ON DELETE RESTRICT,
  CONSTRAINT notification_deliveries_channel_chk CHECK (channel IN ('web_ui','webhook')),
  CONSTRAINT notification_deliveries_status_chk CHECK (status IN ('succeeded','suppressed','failed')),
  CONSTRAINT notification_deliveries_error_chk CHECK ((status='failed' AND error_code ~ '^[a-z][a-z0-9_]{0,63}$') OR (status<>'failed' AND error_code IS NULL)),
  CONSTRAINT notification_deliveries_occurrence_chk CHECK (occurrence >= 1),
  CONSTRAINT notification_deliveries_attempt_chk CHECK (attempt BETWEEN 1 AND 64)
);
CREATE INDEX notification_deliveries_history_idx ON notification_deliveries(organization_id,workspace_id,notification_id,occurrence,channel,attempt);

CREATE FUNCTION notifications_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.recipient_id<>OLD.recipient_id OR NEW.dedupe_key<>OLD.dedupe_key OR NEW.first_occurred_at<>OLD.first_occurred_at OR NEW.created_at<>OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification identity is immutable'';
  END IF;
  IF NEW.occurrence_count<OLD.occurrence_count THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification occurrence count cannot decrease'';
  END IF;
  IF NEW.last_occurred_at<OLD.last_occurred_at OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification timestamps cannot move backwards'';
  END IF;
  IF (CASE OLD.severity WHEN ''info'' THEN 1 WHEN ''warning'' THEN 2 WHEN ''critical'' THEN 3 ELSE 99 END) > (CASE NEW.severity WHEN ''info'' THEN 1 WHEN ''warning'' THEN 2 WHEN ''critical'' THEN 3 ELSE 0 END) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification severity cannot decrease'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER notifications_guard_update BEFORE UPDATE ON notifications
  FOR EACH ROW EXECUTE FUNCTION notifications_guard_update();

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY notifications_tenant_all ON notifications FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_preferences_tenant_all ON notification_preferences FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_deliveries_tenant_select ON notification_deliveries FOR SELECT
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY notification_deliveries_tenant_insert ON notification_deliveries FOR INSERT
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON notifications, notification_preferences, notification_deliveries FROM PUBLIC;
REVOKE UPDATE ON notification_deliveries FROM PUBLIC;

CREATE FUNCTION notification_deliveries_reject_mutation() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''notification delivery history is immutable'';
  RETURN NULL;
END';
CREATE TRIGGER notification_deliveries_no_update_delete BEFORE UPDATE OR DELETE ON notification_deliveries
  FOR EACH ROW EXECUTE FUNCTION notification_deliveries_reject_mutation();
CREATE TRIGGER notification_deliveries_no_clear BEFORE TRUNCATE ON notification_deliveries
  FOR EACH STATEMENT EXECUTE FUNCTION notification_deliveries_reject_mutation();

COMMENT ON TABLE notifications IS 'Tenant-scoped canonical notification inbox. Dedupe key collapses repeated conditions while occurrence_count and monotonic severity preserve material changes.';
COMMENT ON TABLE notification_preferences IS 'Per-recipient channel enablement and minimum severity. External webhook delivery is opt-in by default in application policy.';
COMMENT ON TABLE notification_deliveries IS 'Immutable per-occurrence channel attempt history; retries append attempts and remote bodies, headers, tokens and raw provider errors are forbidden.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
