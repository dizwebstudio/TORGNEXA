BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
