BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 170: durable provider-neutral WMS execution. The older wms_tasks tables
-- are retained for compatibility with the pre-v1 reference model; production
-- operator commands use this tenant-scoped execution model.
CREATE TABLE wms_execution_tasks (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  task_id text NOT NULL,
  idempotency_key text NOT NULL,
  task_type text NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  warehouse_id text NOT NULL,
  sku text NOT NULL,
  unit text NOT NULL,
  order_id text,
  order_item_id text,
  fulfillment_allocation_id text,
  source_location_code text NOT NULL DEFAULT '',
  target_location_code text NOT NULL DEFAULT '',
  expected_quantity_coefficient bigint NOT NULL,
  expected_quantity_scale smallint NOT NULL,
  processed_quantity_coefficient bigint NOT NULL DEFAULT 0,
  processed_quantity_scale smallint NOT NULL DEFAULT 0,
  assigned_to text NOT NULL DEFAULT '',
  exception_code text NOT NULL DEFAULT '',
  cancel_reason text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  claimed_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,task_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses (organization_id,workspace_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,order_id) REFERENCES orders (organization_id,workspace_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,order_item_id) REFERENCES order_items (organization_id,workspace_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,fulfillment_allocation_id) REFERENCES fulfillment_allocations (organization_id,workspace_id,allocation_id) ON DELETE RESTRICT,
  CONSTRAINT wms_execution_tasks_type_chk CHECK (task_type IN ('receiving','put_away','pick','pack','cycle_count','transfer','return_receiving')),
  CONSTRAINT wms_execution_tasks_state_chk CHECK (state IN ('pending','in_progress','completed','cancelled','exception')),
  CONSTRAINT wms_execution_tasks_link_chk CHECK ((order_id IS NULL AND order_item_id IS NULL AND fulfillment_allocation_id IS NULL) OR (order_id IS NOT NULL AND order_item_id IS NOT NULL AND fulfillment_allocation_id IS NOT NULL)),
  CONSTRAINT wms_execution_tasks_quantity_chk CHECK (expected_quantity_coefficient > 0 AND expected_quantity_scale BETWEEN 0 AND 9 AND processed_quantity_coefficient >= 0 AND processed_quantity_scale BETWEEN 0 AND 9 AND (expected_quantity_scale = 0 OR expected_quantity_coefficient % 10 <> 0) AND (processed_quantity_scale = 0 OR processed_quantity_coefficient = 0 OR processed_quantity_coefficient % 10 <> 0) AND processed_quantity_coefficient::numeric * power(10::numeric,expected_quantity_scale) <= expected_quantity_coefficient::numeric * power(10::numeric,processed_quantity_scale)),
  CONSTRAINT wms_execution_tasks_ref_chk CHECK (task_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND warehouse_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT wms_execution_tasks_text_chk CHECK (char_length(source_location_code) <= 128 AND char_length(target_location_code) <= 128 AND assigned_to = btrim(assigned_to) AND char_length(assigned_to) <= 191 AND exception_code = btrim(exception_code) AND char_length(exception_code) <= 64 AND cancel_reason = btrim(cancel_reason) AND char_length(cancel_reason) <= 64),
  CONSTRAINT wms_execution_tasks_version_chk CHECK (version >= 1),
  CONSTRAINT wms_execution_tasks_time_chk CHECK (updated_at >= created_at AND (completed_at IS NULL OR completed_at >= created_at) AND (started_at IS NULL OR started_at >= created_at) AND (claimed_at IS NULL OR claimed_at >= created_at))
);

CREATE TABLE wms_execution_task_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  task_id text NOT NULL,
  idempotency_key text NOT NULL,
  kind text NOT NULL,
  barcode_digest varchar(64) NOT NULL DEFAULT '',
  location_code text NOT NULL DEFAULT '',
  quantity_coefficient bigint NOT NULL DEFAULT 0,
  quantity_scale smallint NOT NULL DEFAULT 0,
  unit text NOT NULL DEFAULT '',
  reason_code text NOT NULL DEFAULT '',
  actor_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,event_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,task_id) REFERENCES wms_execution_tasks (organization_id,workspace_id,task_id) ON DELETE RESTRICT,
  CONSTRAINT wms_execution_task_events_kind_chk CHECK (kind IN ('created','claimed','started','scan_applied','completed','exception','cancelled')),
  CONSTRAINT wms_execution_task_events_quantity_chk CHECK (quantity_coefficient >= 0 AND quantity_scale BETWEEN 0 AND 9 AND (quantity_scale = 0 OR quantity_coefficient = 0 OR quantity_coefficient % 10 <> 0)),
  CONSTRAINT wms_execution_task_events_digest_chk CHECK (barcode_digest = '' OR barcode_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT wms_execution_task_events_ref_chk CHECK (event_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(location_code) <= 128 AND char_length(reason_code) <= 64),
  CONSTRAINT wms_execution_task_events_unit_chk CHECK (unit = '' OR unit ~ '^[A-Z][A-Z0-9._-]{0,15}$')
);

CREATE INDEX wms_execution_tasks_queue_idx ON wms_execution_tasks (organization_id,workspace_id,state,warehouse_id,updated_at,task_id);
CREATE INDEX wms_execution_tasks_order_idx ON wms_execution_tasks (organization_id,workspace_id,order_id,order_item_id);
CREATE INDEX wms_execution_tasks_assignee_idx ON wms_execution_tasks (organization_id,workspace_id,assigned_to,state,updated_at,task_id);
CREATE INDEX wms_execution_task_events_task_idx ON wms_execution_task_events (organization_id,workspace_id,task_id,occurred_at,event_id);

CREATE FUNCTION wms_execution_task_events_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''wms task history is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER wms_execution_task_events_no_update_delete BEFORE UPDATE OR DELETE OR TRUNCATE ON wms_execution_task_events FOR EACH STATEMENT EXECUTE FUNCTION wms_execution_task_events_no_mutation();

ALTER TABLE wms_execution_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_execution_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_execution_tasks_tenant_all ON wms_execution_tasks FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_execution_task_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_execution_task_events FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_execution_task_events_tenant_all ON wms_execution_task_events FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON wms_execution_task_events FROM PUBLIC;

COMMENT ON TABLE wms_execution_tasks IS 'Tenant-scoped provider-neutral WMS operator tasks. PostgreSQL is the execution source of truth.';
COMMENT ON TABLE wms_execution_task_events IS 'Immutable tenant-scoped WMS command history; barcodes are stored only as SHA-256 digests.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
