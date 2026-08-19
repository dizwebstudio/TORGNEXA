BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
