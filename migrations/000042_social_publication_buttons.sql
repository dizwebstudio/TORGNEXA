BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 181: persist the provider-neutral HTTPS button snapshot with the
-- immutable content variant. No callback data or provider payload is stored.
CREATE OR REPLACE FUNCTION social_valid_capabilities(value jsonb) RETURNS boolean
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
      ''social.post.text'',''social.post.media'',''social.post.video'',''social.post.buttons'',''social.post.edit'',
      ''social.post.delete'',''social.comments.read'',''social.comments.reply'',''social.analytics.read''
    ) THEN RETURN false; END IF;
    IF item = ANY(seen) THEN RETURN false; END IF;
    IF previous <> '''' AND item <= previous THEN RETURN false; END IF;
    seen := array_append(seen,item);
    previous := item;
  END LOOP;
  RETURN true;
END';

CREATE FUNCTION social_valid_buttons(value jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT
AS 'DECLARE
  item jsonb;
  text_value text;
  url_value text;
BEGIN
  IF jsonb_typeof(value) <> ''array'' OR jsonb_array_length(value) > 8 THEN
    RETURN false;
  END IF;
  FOR item IN SELECT elements.item FROM jsonb_array_elements(value) AS elements(item) LOOP
    IF jsonb_typeof(item) <> ''object'' OR (item - ''text'' - ''url'') <> ''{}''::jsonb OR
       jsonb_typeof(item->''text'') <> ''string'' OR jsonb_typeof(item->''url'') <> ''string'' THEN
      RETURN false;
    END IF;
    text_value := item->>''text'';
    url_value := item->>''url'';
    IF text_value <> btrim(text_value) OR char_length(text_value) < 1 OR char_length(text_value) > 64 OR
       url_value !~ ''^https://[^[:space:]]+$'' OR char_length(url_value) > 2048 OR
       url_value ~ ''["<>\[\]{}]'' THEN
      RETURN false;
    END IF;
  END LOOP;
  RETURN true;
END';

ALTER TABLE social_content_variants
  ADD COLUMN buttons jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE social_content_variants
  ADD CONSTRAINT social_variants_buttons_chk CHECK (social_valid_buttons(buttons));

COMMENT ON COLUMN social_content_variants.buttons IS 'Immutable HTTPS link-button snapshot; callback data and provider payloads are excluded.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
