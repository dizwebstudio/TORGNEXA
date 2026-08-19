BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE lineage_records (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  source text NOT NULL CHECK (source ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  actor_id text,
  operation text NOT NULL CHECK (operation ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  output_system text NOT NULL CHECK (output_system ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  output_entity_type text NOT NULL CHECK (output_entity_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  output_entity_id text NOT NULL CHECK (char_length(output_entity_id) BETWEEN 1 AND 512),
  output_entity_version text,
  output_field text,
  output_observed_at timestamptz,
  transform_kind text NOT NULL CHECK (transform_kind ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  transform_id text NOT NULL CHECK (char_length(transform_id) BETWEEN 1 AND 256),
  transform_version text NOT NULL CHECK (char_length(transform_version) BETWEEN 1 AND 128),
  mapping_id text,
  rule_id text,
  correlation_id text NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 256),
  causation_id text,
  audit_id text NOT NULL REFERENCES audit_records(id),
  event_id text NOT NULL REFERENCES outbox_events(id),
  result text NOT NULL CHECK (result IN ('applied','observed','rejected')),
  fingerprint_sha256 text NOT NULL CHECK (fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT lineage_records_workspace_fk FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT lineage_records_optional_fields CHECK (
    (output_entity_version IS NULL OR char_length(output_entity_version) BETWEEN 1 AND 128) AND
    (output_field IS NULL OR output_field ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$') AND
    (actor_id IS NULL OR char_length(actor_id) BETWEEN 1 AND 256) AND
    (mapping_id IS NULL OR char_length(mapping_id) BETWEEN 1 AND 256) AND
    (rule_id IS NULL OR char_length(rule_id) BETWEEN 1 AND 256) AND
    (causation_id IS NULL OR char_length(causation_id) BETWEEN 1 AND 256)
  )
);
CREATE UNIQUE INDEX lineage_records_id_global_idx ON lineage_records(id);
CREATE INDEX lineage_records_timeline_idx ON lineage_records(organization_id,workspace_id,output_system,output_entity_type,output_entity_id,output_field,occurred_at DESC,id DESC);
CREATE INDEX lineage_records_correlation_idx ON lineage_records(organization_id,workspace_id,correlation_id,occurred_at DESC);

CREATE TABLE lineage_inputs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  record_id text NOT NULL,
  position smallint NOT NULL CHECK (position BETWEEN 1 AND 32),
  role text NOT NULL CHECK (role ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  source_system text NOT NULL CHECK (source_system ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  source_entity_type text NOT NULL CHECK (source_entity_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'),
  source_entity_id text NOT NULL CHECK (char_length(source_entity_id) BETWEEN 1 AND 512),
  source_entity_version text,
  source_field text,
  source_observed_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,record_id,position),
  CONSTRAINT lineage_inputs_record_fk FOREIGN KEY (organization_id,workspace_id,record_id) REFERENCES lineage_records(organization_id,workspace_id,id),
  CONSTRAINT lineage_inputs_optional_fields CHECK (
    (source_entity_version IS NULL OR char_length(source_entity_version) BETWEEN 1 AND 128) AND
    (source_field IS NULL OR source_field ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$')
  )
);
CREATE INDEX lineage_inputs_source_idx ON lineage_inputs(organization_id,workspace_id,source_system,source_entity_type,source_entity_id,source_field,record_id);

ALTER TABLE lineage_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE lineage_records FORCE ROW LEVEL SECURITY;
ALTER TABLE lineage_inputs ENABLE ROW LEVEL SECURITY;
ALTER TABLE lineage_inputs FORCE ROW LEVEL SECURITY;

CREATE POLICY lineage_records_select ON lineage_records FOR SELECT USING (
  organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY lineage_records_insert ON lineage_records FOR INSERT WITH CHECK (
  organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY lineage_inputs_select ON lineage_inputs FOR SELECT USING (
  organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY lineage_inputs_insert ON lineage_inputs FOR INSERT WITH CHECK (
  organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION lineage_record_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
  IF NOT EXISTS (SELECT 1 FROM audit_records WHERE id=NEW.audit_id AND organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id) THEN
    RAISE EXCEPTION ''lineage audit evidence must belong to same tenant'';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM outbox_events WHERE id=NEW.event_id AND organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id) THEN
    RAISE EXCEPTION ''lineage event evidence must belong to same tenant'';
  END IF;
  RETURN NEW;
END ';
CREATE TRIGGER lineage_record_evidence_validate BEFORE INSERT ON lineage_records FOR EACH ROW EXECUTE FUNCTION lineage_record_evidence_guard();

CREATE FUNCTION lineage_append_only() RETURNS trigger LANGUAGE plpgsql AS ' BEGIN RAISE EXCEPTION ''lineage evidence is append-only''; END ';
CREATE TRIGGER lineage_records_immutable BEFORE UPDATE OR DELETE ON lineage_records FOR EACH ROW EXECUTE FUNCTION lineage_append_only();
CREATE TRIGGER lineage_inputs_immutable BEFORE UPDATE OR DELETE ON lineage_inputs FOR EACH ROW EXECUTE FUNCTION lineage_append_only();
CREATE FUNCTION lineage_no_clear() RETURNS trigger LANGUAGE plpgsql AS ' BEGIN RAISE EXCEPTION ''lineage evidence cannot be cleared''; END ';
CREATE TRIGGER lineage_records_no_clear BEFORE TRUNCATE ON lineage_records EXECUTE FUNCTION lineage_no_clear();
CREATE TRIGGER lineage_inputs_no_clear BEFORE TRUNCATE ON lineage_inputs EXECUTE FUNCTION lineage_no_clear();

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
