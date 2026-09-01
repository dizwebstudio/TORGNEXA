BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 229: mobile is a command/observation surface over the canonical WMS,
-- allocation and fulfillment records. None of these tables is an inventory
-- ledger or a replacement for marketplace shipment truth.
CREATE TABLE mobile_fulfillment_plans (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  plan_id text NOT NULL,
  idempotency_key text NOT NULL,
  order_id text,
  warehouse_id text,
  mode text NOT NULL,
  owner text NOT NULL,
  stage text NOT NULL,
  state text NOT NULL DEFAULT 'active',
  local_execution boolean NOT NULL DEFAULT false,
  remote_reference_digest varchar(64) NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,plan_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses (organization_id,workspace_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,order_id) REFERENCES orders (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT mobile_fulfillment_plans_mode_chk CHECK (mode IN ('fbs','fbo','hybrid','split')),
  CONSTRAINT mobile_fulfillment_plans_owner_chk CHECK (owner IN ('seller_warehouse','marketplace','carrier')),
  CONSTRAINT mobile_fulfillment_plans_stage_chk CHECK (stage IN ('pick','pack','print','handoff','tracking','remote_visibility','complete','manual_attention')),
  CONSTRAINT mobile_fulfillment_plans_state_chk CHECK (state IN ('active','pending','unknown','complete','cancelled','manual_attention')),
  CONSTRAINT mobile_fulfillment_plans_mode_owner_chk CHECK ((mode='fbo' AND owner='marketplace' AND local_execution=false) OR (mode<>'fbo' AND (local_execution=true OR owner<>'marketplace'))),
  CONSTRAINT mobile_fulfillment_plans_digest_chk CHECK (remote_reference_digest = '' OR remote_reference_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_fulfillment_plans_ref_chk CHECK (plan_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (warehouse_id IS NULL OR warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$') AND (order_id IS NULL OR order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')),
  CONSTRAINT mobile_fulfillment_plans_time_chk CHECK (version >= 1 AND updated_at >= created_at)
);

CREATE TABLE mobile_devices (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  device_id text NOT NULL,
  idempotency_key text NOT NULL,
  label text NOT NULL,
  warehouse_id text NOT NULL,
  zone_code text NOT NULL DEFAULT '',
  device_kind text NOT NULL,
  state text NOT NULL DEFAULT 'active',
  operator_id text NOT NULL,
  last_seen_at timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,device_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT mobile_devices_kind_chk CHECK (device_kind IN ('handheld','camera','scanner_station','scale_station')),
  CONSTRAINT mobile_devices_state_chk CHECK (state IN ('active','revoked','expired')),
  CONSTRAINT mobile_devices_ref_chk CHECK (device_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND operator_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT mobile_devices_text_chk CHECK (label = btrim(label) AND char_length(label) BETWEEN 1 AND 160 AND char_length(zone_code) <= 128),
  CONSTRAINT mobile_devices_time_chk CHECK (version >= 1 AND updated_at >= created_at AND (last_seen_at IS NULL OR last_seen_at >= created_at))
);

CREATE TABLE mobile_pick_batches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  idempotency_key text NOT NULL,
  plan_id text NOT NULL,
  warehouse_id text NOT NULL,
  strategy text NOT NULL,
  state text NOT NULL DEFAULT 'ready',
  route_digest varchar(64) NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,batch_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT mobile_pick_batches_strategy_chk CHECK (strategy IN ('order','wave','zone','batch')),
  CONSTRAINT mobile_pick_batches_state_chk CHECK (state IN ('ready','in_progress','complete','exception','cancelled')),
  CONSTRAINT mobile_pick_batches_digest_chk CHECK (route_digest = '' OR route_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_pick_batches_ref_chk CHECK (batch_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND plan_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT mobile_pick_batches_time_chk CHECK (version >= 1 AND updated_at >= created_at)
);

CREATE TABLE mobile_pick_batch_tasks (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  task_id text NOT NULL,
  position smallint NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,batch_id,task_id),
  UNIQUE (organization_id,workspace_id,task_id),
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES mobile_pick_batches (organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,task_id) REFERENCES wms_execution_tasks (organization_id,workspace_id,task_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_pick_batch_tasks_position_chk CHECK (position BETWEEN 1 AND 100)
);

CREATE TABLE mobile_scan_evidence (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  scan_id text NOT NULL,
  idempotency_key text NOT NULL,
  plan_id text NOT NULL,
  task_id text NOT NULL,
  device_id text NOT NULL,
  kind text NOT NULL,
  code_digest varchar(64) NOT NULL,
  location_code text NOT NULL DEFAULT '',
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  unit text NOT NULL,
  result text NOT NULL DEFAULT 'applied',
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,scan_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,task_id) REFERENCES wms_execution_tasks (organization_id,workspace_id,task_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,device_id) REFERENCES mobile_devices (organization_id,workspace_id,device_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_scan_evidence_kind_chk CHECK (kind IN ('product','location','package','serial','label')),
  CONSTRAINT mobile_scan_evidence_result_chk CHECK (result IN ('applied','rejected','unknown','pending')),
  CONSTRAINT mobile_scan_evidence_digest_chk CHECK (code_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_scan_evidence_ref_chk CHECK (scan_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND device_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  CONSTRAINT mobile_scan_evidence_quantity_chk CHECK (quantity_coefficient > 0 AND quantity_scale BETWEEN 0 AND 9 AND (quantity_scale = 0 OR quantity_coefficient % 10 <> 0)),
  CONSTRAINT mobile_scan_evidence_time_chk CHECK (occurred_at = timezone('UTC', occurred_at))
);

CREATE TABLE mobile_pack_sessions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  pack_id text NOT NULL,
  idempotency_key text NOT NULL,
  plan_id text NOT NULL,
  batch_id text,
  package_count integer NOT NULL,
  weight_grams bigint NOT NULL,
  length_mm bigint NOT NULL,
  width_mm bigint NOT NULL,
  height_mm bigint NOT NULL,
  packaging_type text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'open',
  facts_digest varchar(64) NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,pack_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES mobile_pick_batches (organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_pack_sessions_state_chk CHECK (state IN ('open','closed','repacked','exception','cancelled')),
  CONSTRAINT mobile_pack_sessions_digest_chk CHECK (facts_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_pack_sessions_ref_chk CHECK (pack_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT mobile_pack_sessions_facts_chk CHECK (package_count BETWEEN 1 AND 100 AND weight_grams BETWEEN 0 AND 2000000 AND length_mm BETWEEN 0 AND 10000 AND width_mm BETWEEN 0 AND 10000 AND height_mm BETWEEN 0 AND 10000),
  CONSTRAINT mobile_pack_sessions_time_chk CHECK (version >= 1 AND updated_at >= created_at)
);

CREATE TABLE mobile_print_jobs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  print_job_id text NOT NULL,
  idempotency_key text NOT NULL,
  plan_id text NOT NULL,
  pack_id text,
  printer_id text NOT NULL,
  document_type text NOT NULL,
  template_version text NOT NULL,
  media_size text NOT NULL DEFAULT '',
  language text NOT NULL DEFAULT 'ru-RU',
  copies integer NOT NULL,
  state text NOT NULL DEFAULT 'queued',
  reprint boolean NOT NULL DEFAULT false,
  reason_code text NOT NULL DEFAULT '',
  artifact_digest varchar(64) NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,print_job_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,pack_id) REFERENCES mobile_pack_sessions (organization_id,workspace_id,pack_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_print_jobs_document_chk CHECK (document_type IN ('label','pick_list','packing_slip','manifest','internal_barcode')),
  CONSTRAINT mobile_print_jobs_state_chk CHECK (state IN ('queued','printed','failed','unknown','cancelled')),
  CONSTRAINT mobile_print_jobs_ref_chk CHECK (print_job_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND printer_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT mobile_print_jobs_text_chk CHECK (template_version = btrim(template_version) AND char_length(template_version) BETWEEN 1 AND 64 AND char_length(media_size) <= 32 AND char_length(language) BETWEEN 2 AND 16 AND char_length(reason_code) <= 128 AND char_length(error_code) <= 128),
  CONSTRAINT mobile_print_jobs_digest_chk CHECK (artifact_digest = '' OR artifact_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_print_jobs_copies_chk CHECK (copies BETWEEN 1 AND 20),
  CONSTRAINT mobile_print_jobs_time_chk CHECK (version >= 1 AND updated_at >= created_at)
);

CREATE TABLE mobile_offline_intents (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  intent_id text NOT NULL,
  idempotency_key text NOT NULL,
  device_id text NOT NULL,
  plan_id text NOT NULL,
  operation text NOT NULL,
  payload_digest varchar(64) NOT NULL,
  sequence_no bigint NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  error_code text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,intent_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,device_id) REFERENCES mobile_devices (organization_id,workspace_id,device_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_offline_intents_operation_chk CHECK (operation IN ('scan','pack','print')),
  CONSTRAINT mobile_offline_intents_state_chk CHECK (state IN ('pending','applied','rejected','unknown','needs_attention')),
  CONSTRAINT mobile_offline_intents_digest_chk CHECK (payload_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_offline_intents_ref_chk CHECK (intent_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND device_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  CONSTRAINT mobile_offline_intents_sequence_chk CHECK (sequence_no >= 0),
  CONSTRAINT mobile_offline_intents_time_chk CHECK (updated_at >= created_at)
);

CREATE TABLE mobile_remote_observations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  observation_id text NOT NULL,
  idempotency_key text NOT NULL,
  plan_id text NOT NULL,
  stage text NOT NULL,
  state text NOT NULL,
  remote_reference_digest varchar(64) NOT NULL DEFAULT '',
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,observation_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,plan_id) REFERENCES mobile_fulfillment_plans (organization_id,workspace_id,plan_id) ON DELETE RESTRICT,
  CONSTRAINT mobile_remote_observations_stage_chk CHECK (stage IN ('remote_visibility','handoff','tracking','complete','manual_attention')),
  CONSTRAINT mobile_remote_observations_state_chk CHECK (state IN ('pending','observed','accepted','unknown','manual_attention')),
  CONSTRAINT mobile_remote_observations_digest_chk CHECK (remote_reference_digest = '' OR remote_reference_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT mobile_remote_observations_ref_chk CHECK (observation_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT mobile_remote_observations_time_chk CHECK (observed_at = timezone('UTC', observed_at))
);

CREATE INDEX mobile_plans_queue_idx ON mobile_fulfillment_plans (organization_id,workspace_id,state,mode,updated_at,plan_id);
CREATE INDEX mobile_devices_warehouse_idx ON mobile_devices (organization_id,workspace_id,warehouse_id,state,updated_at,device_id);
CREATE INDEX mobile_pick_batches_queue_idx ON mobile_pick_batches (organization_id,workspace_id,plan_id,state,updated_at,batch_id);
CREATE INDEX mobile_scan_evidence_plan_idx ON mobile_scan_evidence (organization_id,workspace_id,plan_id,occurred_at,scan_id);
CREATE INDEX mobile_pack_sessions_plan_idx ON mobile_pack_sessions (organization_id,workspace_id,plan_id,updated_at,pack_id);
CREATE INDEX mobile_print_jobs_queue_idx ON mobile_print_jobs (organization_id,workspace_id,state,printer_id,updated_at,print_job_id);
CREATE INDEX mobile_offline_intents_queue_idx ON mobile_offline_intents (organization_id,workspace_id,device_id,state,sequence_no,intent_id);
CREATE INDEX mobile_remote_observations_plan_idx ON mobile_remote_observations (organization_id,workspace_id,plan_id,observed_at,observation_id);

CREATE FUNCTION mobile_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''mobile evidence is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER mobile_scan_evidence_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON mobile_scan_evidence FOR EACH STATEMENT EXECUTE FUNCTION mobile_append_only();
CREATE TRIGGER mobile_remote_observations_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON mobile_remote_observations FOR EACH STATEMENT EXECUTE FUNCTION mobile_append_only();

ALTER TABLE mobile_fulfillment_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_fulfillment_plans FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_devices FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_pick_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_pick_batches FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_pick_batch_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_pick_batch_tasks FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_scan_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_scan_evidence FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_pack_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_pack_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_print_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_print_jobs FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_offline_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_offline_intents FORCE ROW LEVEL SECURITY;
ALTER TABLE mobile_remote_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_remote_observations FORCE ROW LEVEL SECURITY;

CREATE POLICY mobile_fulfillment_plans_tenant_all ON mobile_fulfillment_plans FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_devices_tenant_all ON mobile_devices FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_pick_batches_tenant_all ON mobile_pick_batches FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_pick_batch_tasks_tenant_all ON mobile_pick_batch_tasks FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_scan_evidence_tenant_all ON mobile_scan_evidence FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_pack_sessions_tenant_all ON mobile_pack_sessions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_print_jobs_tenant_all ON mobile_print_jobs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_offline_intents_tenant_all ON mobile_offline_intents FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY mobile_remote_observations_tenant_all ON mobile_remote_observations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE,TRUNCATE ON mobile_scan_evidence FROM PUBLIC;
REVOKE DELETE,TRUNCATE ON mobile_remote_observations FROM PUBLIC;

COMMENT ON TABLE mobile_fulfillment_plans IS 'Tenant-scoped FBS/FBO/hybrid mobile projection; not an inventory or order ledger.';
COMMENT ON TABLE mobile_scan_evidence IS 'Append-only mobile scan evidence with SHA-256 digest only; raw codes are transient.';
COMMENT ON TABLE mobile_offline_intents IS 'Tenant-scoped offline command receipts; payloads are represented by digest only.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
