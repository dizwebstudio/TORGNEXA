BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 170.7: local WMS batch/pack handoff. A batch is an internal grouping
-- of completed pick tasks; it is not a marketplace shipment or label.
CREATE TABLE wms_execution_batches (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  idempotency_key text NOT NULL,
  kind text NOT NULL DEFAULT 'pack_handoff',
  state text NOT NULL DEFAULT 'ready',
  warehouse_id text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,batch_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces (organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses (organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT wms_execution_batches_kind_chk CHECK (kind = 'pack_handoff'),
  CONSTRAINT wms_execution_batches_state_chk CHECK (state IN ('ready','handed_off','exception','cancelled')),
  CONSTRAINT wms_execution_batches_ref_chk CHECK (batch_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT wms_execution_batches_version_chk CHECK (version >= 1),
  CONSTRAINT wms_execution_batches_time_chk CHECK (updated_at >= created_at)
);

CREATE TABLE wms_execution_batch_tasks (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  batch_id text NOT NULL,
  task_id text NOT NULL,
  position smallint NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,batch_id,task_id),
  UNIQUE (organization_id,workspace_id,task_id),
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES wms_execution_batches (organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,task_id) REFERENCES wms_execution_tasks (organization_id,workspace_id,task_id) ON DELETE RESTRICT,
  CONSTRAINT wms_execution_batch_tasks_position_chk CHECK (position BETWEEN 1 AND 50)
);

CREATE TABLE wms_execution_batch_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  batch_id text NOT NULL,
  idempotency_key text NOT NULL,
  kind text NOT NULL,
  actor_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,event_id),
  UNIQUE (organization_id,workspace_id,idempotency_key),
  FOREIGN KEY (organization_id,workspace_id,batch_id) REFERENCES wms_execution_batches (organization_id,workspace_id,batch_id) ON DELETE RESTRICT,
  CONSTRAINT wms_execution_batch_events_kind_chk CHECK (kind IN ('created','handed_off')),
  CONSTRAINT wms_execution_batch_events_ref_chk CHECK (event_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
);

CREATE INDEX wms_execution_batches_queue_idx ON wms_execution_batches (organization_id,workspace_id,state,warehouse_id,updated_at,batch_id);
CREATE INDEX wms_execution_batch_tasks_task_idx ON wms_execution_batch_tasks (organization_id,workspace_id,task_id);

CREATE FUNCTION wms_execution_batch_events_no_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''wms batch history is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER wms_execution_batch_events_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON wms_execution_batch_events FOR EACH STATEMENT EXECUTE FUNCTION wms_execution_batch_events_no_mutation();

ALTER TABLE wms_execution_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_execution_batches FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_execution_batches_tenant_all ON wms_execution_batches FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_execution_batch_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_execution_batch_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_execution_batch_tasks_tenant_all ON wms_execution_batch_tasks FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE wms_execution_batch_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE wms_execution_batch_events FORCE ROW LEVEL SECURITY;
CREATE POLICY wms_execution_batch_events_tenant_all ON wms_execution_batch_events FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON wms_execution_batch_events FROM PUBLIC;

COMMENT ON TABLE wms_execution_batches IS 'Tenant-scoped internal WMS pack handoff groups; not a marketplace shipment.';
COMMENT ON TABLE wms_execution_batch_events IS 'Immutable tenant-scoped WMS batch history.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
