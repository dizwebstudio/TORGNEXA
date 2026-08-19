BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION social_valid_capabilities(value jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT
AS 'DECLARE
  item text;
  previous text := '''';
  seen text[] := ARRAY[]::text[];
BEGIN
  IF jsonb_typeof(value) <> ''array'' OR jsonb_array_length(value) < 1 OR jsonb_array_length(value) > 8 THEN
    RETURN false;
  END IF;
  FOR item IN SELECT jsonb_array_elements_text(value) LOOP
    IF item NOT IN (
      ''social.post.text'',''social.post.media'',''social.post.video'',''social.post.edit'',
      ''social.post.delete'',''social.comments.read'',''social.comments.reply'',''social.analytics.read''
    ) THEN RETURN false; END IF;
    IF item = ANY(seen) THEN RETURN false; END IF;
    IF previous <> '''' AND item <= previous THEN RETURN false; END IF;
    seen := array_append(seen,item);
    previous := item;
  END LOOP;
  RETURN true;
END';

CREATE TABLE social_contents (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  title text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_contents_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_contents_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_contents_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_contents_title_chk CHECK(title=btrim(title) AND char_length(title)<=300),
  CONSTRAINT social_contents_body_chk CHECK(body=btrim(body) AND char_length(body)<=50000),
  CONSTRAINT social_contents_nonempty_chk CHECK(title<>'' OR body<>''),
  CONSTRAINT social_contents_status_chk CHECK(status IN ('draft','ready','archived')),
  CONSTRAINT social_contents_version_chk CHECK(version>=1),
  CONSTRAINT social_contents_time_chk CHECK(updated_at>=created_at)
);
CREATE INDEX social_contents_status_idx ON social_contents(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_content_variants (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  content_id text NOT NULL,
  format text NOT NULL,
  title text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_variants_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_variants_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variants_content_fk FOREIGN KEY(organization_id,workspace_id,content_id) REFERENCES social_contents(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variants_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_variants_format_chk CHECK(format IN ('text','image','gallery','video','article')),
  CONSTRAINT social_variants_title_chk CHECK(title=btrim(title) AND char_length(title)<=300),
  CONSTRAINT social_variants_body_chk CHECK(body=btrim(body) AND char_length(body)<=50000),
  CONSTRAINT social_variants_version_chk CHECK(version=1)
);
CREATE INDEX social_variants_content_idx ON social_content_variants(organization_id,workspace_id,content_id,created_at DESC,id DESC);

CREATE TABLE social_variant_media_refs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  variant_id text NOT NULL,
  position smallint NOT NULL,
  upload_id text NOT NULL,
  kind text NOT NULL,
  alt_text text NOT NULL DEFAULT '',
  PRIMARY KEY(organization_id,workspace_id,variant_id,position),
  CONSTRAINT social_variant_media_unique UNIQUE(organization_id,workspace_id,variant_id,upload_id),
  CONSTRAINT social_variant_media_variant_fk FOREIGN KEY(organization_id,workspace_id,variant_id) REFERENCES social_content_variants(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_variant_media_upload_fk FOREIGN KEY(upload_id,organization_id,workspace_id) REFERENCES uploads(id,organization_id,workspace_id) ON DELETE RESTRICT,
  CONSTRAINT social_variant_media_position_chk CHECK(position BETWEEN 0 AND 19),
  CONSTRAINT social_variant_media_kind_chk CHECK(kind IN ('image','video')),
  CONSTRAINT social_variant_media_alt_chk CHECK(alt_text=btrim(alt_text) AND char_length(alt_text)<=1000)
);

CREATE TABLE social_channel_accounts (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  display_name text NOT NULL,
  capabilities jsonb NOT NULL,
  status text NOT NULL DEFAULT 'disabled',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT social_channel_accounts_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_channel_accounts_connector_key UNIQUE(organization_id,workspace_id,connector_account_id),
  CONSTRAINT social_channel_accounts_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_channel_accounts_connector_fk FOREIGN KEY(organization_id,workspace_id,connector_account_id) REFERENCES connector_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_channel_accounts_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_channel_accounts_name_chk CHECK(display_name=btrim(display_name) AND char_length(display_name) BETWEEN 1 AND 300),
  CONSTRAINT social_channel_accounts_capabilities_chk CHECK(social_valid_capabilities(capabilities)),
  CONSTRAINT social_channel_accounts_status_chk CHECK(status IN ('disabled','active')),
  CONSTRAINT social_channel_accounts_version_chk CHECK(version>=1),
  CONSTRAINT social_channel_accounts_time_chk CHECK(updated_at>=created_at)
);
CREATE INDEX social_channel_accounts_status_idx ON social_channel_accounts(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_publications (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  variant_id text NOT NULL,
  channel_account_id text NOT NULL,
  schedule_mode text NOT NULL,
  scheduled_at timestamptz,
  status text NOT NULL,
  attempt integer NOT NULL DEFAULT 0,
  reason_code text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at timestamptz,
  PRIMARY KEY(id),
  CONSTRAINT social_publications_tenant_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT social_publications_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_variant_fk FOREIGN KEY(organization_id,workspace_id,variant_id) REFERENCES social_content_variants(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_channel_fk FOREIGN KEY(organization_id,workspace_id,channel_account_id) REFERENCES social_channel_accounts(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_publications_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT social_publications_schedule_chk CHECK((schedule_mode='immediate' AND scheduled_at IS NULL) OR (schedule_mode='at' AND scheduled_at IS NOT NULL)),
  CONSTRAINT social_publications_status_chk CHECK(status IN ('scheduled','ready','publishing','published','failed','cancelled')),
  CONSTRAINT social_publications_attempt_chk CHECK(attempt BETWEEN 0 AND 1000),
  CONSTRAINT social_publications_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT social_publications_failure_shape_chk CHECK((status='failed' AND reason_code IS NOT NULL) OR (status<>'failed' AND reason_code IS NULL)),
  CONSTRAINT social_publications_published_shape_chk CHECK((status='published' AND published_at IS NOT NULL) OR (status<>'published' AND published_at IS NULL)),
  CONSTRAINT social_publications_version_chk CHECK(version>=1),
  CONSTRAINT social_publications_time_chk CHECK(updated_at>=created_at AND (published_at IS NULL OR published_at>=created_at))
);
CREATE INDEX social_publications_due_idx ON social_publications(organization_id,workspace_id,scheduled_at,id) WHERE status='scheduled';
CREATE INDEX social_publications_status_idx ON social_publications(organization_id,workspace_id,status,updated_at DESC,id DESC);

CREATE TABLE social_publication_status_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  publication_id text NOT NULL,
  publication_version bigint NOT NULL,
  status text NOT NULL,
  attempt integer NOT NULL,
  reason_code text,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,event_id),
  CONSTRAINT social_status_events_publication_fk FOREIGN KEY(organization_id,workspace_id,publication_id) REFERENCES social_publications(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT social_status_events_event_chk CHECK(event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT social_status_events_version_chk CHECK(publication_version>=1),
  CONSTRAINT social_status_events_status_chk CHECK(status IN ('scheduled','ready','publishing','published','failed','cancelled')),
  CONSTRAINT social_status_events_attempt_chk CHECK(attempt BETWEEN 0 AND 1000),
  CONSTRAINT social_status_events_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT social_status_events_failure_shape_chk CHECK((status='failed' AND reason_code IS NOT NULL) OR (status<>'failed' AND reason_code IS NULL)),
  CONSTRAINT social_status_events_correlation_chk CHECK(correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')
);
CREATE INDEX social_status_events_history_idx ON social_publication_status_events(organization_id,workspace_id,publication_id,publication_version,event_id);

ALTER TABLE social_contents ENABLE ROW LEVEL SECURITY; ALTER TABLE social_contents FORCE ROW LEVEL SECURITY;
ALTER TABLE social_content_variants ENABLE ROW LEVEL SECURITY; ALTER TABLE social_content_variants FORCE ROW LEVEL SECURITY;
ALTER TABLE social_variant_media_refs ENABLE ROW LEVEL SECURITY; ALTER TABLE social_variant_media_refs FORCE ROW LEVEL SECURITY;
ALTER TABLE social_channel_accounts ENABLE ROW LEVEL SECURITY; ALTER TABLE social_channel_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE social_publications ENABLE ROW LEVEL SECURITY; ALTER TABLE social_publications FORCE ROW LEVEL SECURITY;
ALTER TABLE social_publication_status_events ENABLE ROW LEVEL SECURITY; ALTER TABLE social_publication_status_events FORCE ROW LEVEL SECURITY;

CREATE POLICY social_contents_tenant_all ON social_contents FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_variants_tenant_all ON social_content_variants FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_variant_media_tenant_all ON social_variant_media_refs FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_channel_accounts_tenant_all ON social_channel_accounts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_publications_tenant_all ON social_publications FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY social_status_events_tenant_all ON social_publication_status_events FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION social_contents_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN
    IF NEW.status<>''draft'' OR NEW.version<>1 THEN RAISE EXCEPTION ''new social content must start draft at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social content identity is immutable''; END IF;
  IF OLD.status=''archived'' THEN RAISE EXCEPTION ''archived social content is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social content version transition invalid''; END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    IF NOT ((OLD.status=''draft'' AND NEW.status IN (''ready'',''archived'')) OR (OLD.status=''ready'' AND NEW.status IN (''draft'',''archived''))) THEN RAISE EXCEPTION ''social content lifecycle transition invalid''; END IF;
    IF NEW.title IS DISTINCT FROM OLD.title OR NEW.body IS DISTINCT FROM OLD.body THEN RAISE EXCEPTION ''social content status change cannot mutate body''; END IF;
  ELSEIF OLD.status<>''draft'' AND (NEW.title IS DISTINCT FROM OLD.title OR NEW.body IS DISTINCT FROM OLD.body) THEN
    RAISE EXCEPTION ''social content must be draft before editing'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_contents_guard_insert BEFORE INSERT ON social_contents FOR EACH ROW EXECUTE FUNCTION social_contents_guard();
CREATE TRIGGER social_contents_guard_update BEFORE UPDATE ON social_contents FOR EACH ROW EXECUTE FUNCTION social_contents_guard();

CREATE FUNCTION social_variants_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE parent_status text;
BEGIN
  SELECT status INTO parent_status FROM social_contents WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.content_id;
  IF parent_status IS NULL OR parent_status=''archived'' THEN RAISE EXCEPTION ''social variant parent unavailable''; END IF;
  IF NEW.version<>1 THEN RAISE EXCEPTION ''social variants are immutable version-1 snapshots''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_variants_guard_insert BEFORE INSERT ON social_content_variants FOR EACH ROW EXECUTE FUNCTION social_variants_guard();

CREATE FUNCTION social_variant_media_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE upload_state text;
BEGIN
  SELECT state INTO upload_state FROM uploads WHERE id=NEW.upload_id AND organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id;
  IF upload_state IS DISTINCT FROM ''released'' THEN RAISE EXCEPTION ''social media requires current released upload''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_variant_media_guard_insert BEFORE INSERT ON social_variant_media_refs FOR EACH ROW EXECUTE FUNCTION social_variant_media_guard();

CREATE FUNCTION social_channel_accounts_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE connector_family text; connector_status text;
BEGIN
  SELECT family,status INTO connector_family,connector_status FROM connector_accounts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.connector_account_id;
  IF connector_family<>''social'' THEN RAISE EXCEPTION ''social channel requires social connector account''; END IF;
  IF TG_OP=''INSERT'' THEN
    IF NEW.status<>''disabled'' OR NEW.version<>1 THEN RAISE EXCEPTION ''new social channel must start disabled at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social channel identity is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social channel version transition invalid''; END IF;
  IF NEW.status=''active'' AND connector_status<>''active'' THEN RAISE EXCEPTION ''active social channel requires active connector account''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_channel_accounts_guard_insert BEFORE INSERT ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_channel_accounts_guard();
CREATE TRIGGER social_channel_accounts_guard_update BEFORE UPDATE ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_channel_accounts_guard();

CREATE FUNCTION social_publications_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE variant_format text; content_status text; channel_status text; channel_caps jsonb; required_cap text;
BEGIN
  SELECT v.format,c.status INTO variant_format,content_status FROM social_content_variants v JOIN social_contents c ON c.organization_id=v.organization_id AND c.workspace_id=v.workspace_id AND c.id=v.content_id WHERE v.organization_id=NEW.organization_id AND v.workspace_id=NEW.workspace_id AND v.id=NEW.variant_id;
  SELECT status,capabilities INTO channel_status,channel_caps FROM social_channel_accounts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.channel_account_id;
  IF variant_format IN (''text'',''article'') THEN required_cap:=''social.post.text''; ELSIF variant_format IN (''image'',''gallery'') THEN required_cap:=''social.post.media''; ELSIF variant_format=''video'' THEN required_cap:=''social.post.video''; ELSE RAISE EXCEPTION ''unsupported social variant format''; END IF;
  IF TG_OP=''INSERT'' THEN
    IF content_status<>''ready'' THEN RAISE EXCEPTION ''publication requires ready content''; END IF;
    IF channel_status<>''active'' THEN RAISE EXCEPTION ''publication requires active channel''; END IF;
    IF NOT channel_caps ? required_cap THEN RAISE EXCEPTION ''social publication capability missing''; END IF;
    IF NEW.version<>1 OR NEW.attempt<>0 OR NEW.reason_code IS NOT NULL OR NEW.published_at IS NOT NULL THEN RAISE EXCEPTION ''new social publication state invalid''; END IF;
    IF NEW.schedule_mode=''immediate'' AND NEW.status<>''ready'' THEN RAISE EXCEPTION ''immediate publication must start ready''; END IF;
    IF NEW.schedule_mode=''at'' AND NEW.status<>''scheduled'' THEN RAISE EXCEPTION ''scheduled publication must start scheduled''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.variant_id IS DISTINCT FROM OLD.variant_id OR NEW.channel_account_id IS DISTINCT FROM OLD.channel_account_id OR NEW.schedule_mode IS DISTINCT FROM OLD.schedule_mode OR NEW.scheduled_at IS DISTINCT FROM OLD.scheduled_at OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''social publication identity and schedule are immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''social publication version transition invalid''; END IF;
  IF NOT ((OLD.status=''scheduled'' AND NEW.status IN (''ready'',''cancelled'')) OR (OLD.status=''ready'' AND NEW.status IN (''publishing'',''cancelled'')) OR (OLD.status=''publishing'' AND NEW.status IN (''published'',''failed'')) OR (OLD.status=''failed'' AND NEW.status IN (''ready'',''cancelled''))) THEN RAISE EXCEPTION ''social publication lifecycle transition invalid''; END IF;
  IF NEW.status IN (''ready'',''publishing'') THEN
    IF content_status<>''ready'' THEN RAISE EXCEPTION ''publication requires ready content''; END IF;
    IF channel_status<>''active'' THEN RAISE EXCEPTION ''publication requires active channel''; END IF;
    IF NOT channel_caps ? required_cap THEN RAISE EXCEPTION ''social publication capability missing''; END IF;
  END IF;
  IF NEW.status=''publishing'' THEN
    IF NEW.attempt<>OLD.attempt+1 THEN RAISE EXCEPTION ''publishing transition must increment attempt''; END IF;
  ELSIF NEW.attempt<>OLD.attempt THEN RAISE EXCEPTION ''non-publishing transition cannot change attempt''; END IF;
  IF NEW.status=''published'' THEN
    IF NEW.published_at IS NULL THEN RAISE EXCEPTION ''published publication needs timestamp''; END IF;
  ELSIF NEW.published_at IS NOT NULL THEN RAISE EXCEPTION ''only published publication may have published_at''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER social_publications_guard_insert BEFORE INSERT ON social_publications FOR EACH ROW EXECUTE FUNCTION social_publications_guard();
CREATE TRIGGER social_publications_guard_update BEFORE UPDATE ON social_publications FOR EACH ROW EXECUTE FUNCTION social_publications_guard();

CREATE FUNCTION social_status_events_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''social publication status history is append-only''; END';
CREATE TRIGGER social_status_events_no_update BEFORE UPDATE OR DELETE ON social_publication_status_events FOR EACH ROW EXECUTE FUNCTION social_status_events_append_only();

REVOKE UPDATE, DELETE, TRUNCATE ON social_content_variants, social_variant_media_refs, social_publication_status_events FROM PUBLIC;
REVOKE DELETE, TRUNCATE ON social_contents, social_channel_accounts, social_publications FROM PUBLIC;

CREATE FUNCTION social_reject_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''social core history cannot be hard-deleted''; END';
CREATE TRIGGER social_contents_no_delete BEFORE DELETE ON social_contents FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_contents_no_clear BEFORE TRUNCATE ON social_contents FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variants_no_delete BEFORE DELETE ON social_content_variants FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variants_no_clear BEFORE TRUNCATE ON social_content_variants FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variant_media_no_delete BEFORE DELETE ON social_variant_media_refs FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_variant_media_no_clear BEFORE TRUNCATE ON social_variant_media_refs FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_channel_accounts_no_delete BEFORE DELETE ON social_channel_accounts FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_channel_accounts_no_clear BEFORE TRUNCATE ON social_channel_accounts FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_publications_no_delete BEFORE DELETE ON social_publications FOR EACH ROW EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_publications_no_clear BEFORE TRUNCATE ON social_publications FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();
CREATE TRIGGER social_status_events_no_clear BEFORE TRUNCATE ON social_publication_status_events FOR EACH STATEMENT EXECUTE FUNCTION social_reject_delete();

COMMENT ON TABLE social_contents IS 'Provider-neutral master social/content records. Remote IDs and provider fields are forbidden.';
COMMENT ON TABLE social_content_variants IS 'Immutable channel-ready content snapshots; new edits create a new variant.';
COMMENT ON TABLE social_variant_media_refs IS 'Only Task-088 UploadIDs are persisted. Raw object keys, signed URLs and filenames are forbidden publication authority.';
COMMENT ON TABLE social_channel_accounts IS 'Trusted projection of one social connector account plus canonical capability snapshot.';
COMMENT ON TABLE social_publications IS 'Canonical TORGNEXA schedule and publication state. Provider-native scheduling is not the source of truth.';
COMMENT ON TABLE social_publication_status_events IS 'Append-only normalized publication status history; raw remote error bodies are forbidden.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
