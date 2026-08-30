BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 164: cancellations, returns, refund allocations and bounded evidence.
-- These tables deliberately do not rewrite orders, payments or inventory facts.

-- Extend the existing lower-level refund state without creating a parallel
-- payment mutation path. Unknown/manual states intentionally carry no remote
-- id until reconciliation or an operator establishes one.
ALTER TABLE payment_refunds DROP CONSTRAINT payment_refunds_status_chk;
ALTER TABLE payment_refunds ADD CONSTRAINT payment_refunds_status_chk CHECK(status IN ('pending','accepted','succeeded','failed','unknown','manual_attention'));
ALTER TABLE payment_refunds DROP CONSTRAINT payment_refunds_remote_shape_chk;
ALTER TABLE payment_refunds ADD CONSTRAINT payment_refunds_remote_shape_chk CHECK((status IN ('pending','unknown','manual_attention') AND remote_refund_id IS NULL) OR (status IN ('accepted','succeeded','failed') AND remote_refund_id IS NOT NULL));

CREATE TABLE order_cancellations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  order_id text NOT NULL,
  status text NOT NULL DEFAULT 'requested',
  reason_code text NOT NULL,
  source text NOT NULL,
  idempotency_key text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT order_cancellations_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT order_cancellations_idempotency_key UNIQUE(organization_id,workspace_id,idempotency_key),
  CONSTRAINT order_cancellations_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT order_cancellations_order_fk FOREIGN KEY(organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT order_cancellations_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT order_cancellations_ref_chk CHECK(order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT order_cancellations_status_chk CHECK(status IN ('requested','approved','executing','cancelled','rejected','failed','unknown')),
  CONSTRAINT order_cancellations_reason_chk CHECK(reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT order_cancellations_source_chk CHECK(source ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT order_cancellations_version_chk CHECK(version >= 1),
  CONSTRAINT order_cancellations_time_chk CHECK(updated_at >= created_at)
);

CREATE TABLE commerce_returns (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  order_id text NOT NULL,
  status text NOT NULL DEFAULT 'requested',
  reason_code text NOT NULL,
  source text NOT NULL,
  currency char(3) NOT NULL,
  requested_shipping_minor bigint NOT NULL DEFAULT 0,
  requested_tax_minor bigint NOT NULL DEFAULT 0,
  idempotency_key text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT commerce_returns_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT commerce_returns_idempotency_key UNIQUE(organization_id,workspace_id,idempotency_key),
  CONSTRAINT commerce_returns_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT commerce_returns_order_fk FOREIGN KEY(organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT commerce_returns_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT commerce_returns_ref_chk CHECK(order_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT commerce_returns_status_chk CHECK(status IN ('requested','approved','authorized','in_transit','received','inspecting','accepted','partially_accepted','rejected','closed','cancelled','expired')),
  CONSTRAINT commerce_returns_reason_chk CHECK(reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT commerce_returns_source_chk CHECK(source ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT commerce_returns_currency_chk CHECK(currency ~ '^[A-Z]{3}$'),
  CONSTRAINT commerce_returns_amount_chk CHECK(requested_shipping_minor >= 0 AND requested_tax_minor >= 0),
  CONSTRAINT commerce_returns_version_chk CHECK(version >= 1),
  CONSTRAINT commerce_returns_time_chk CHECK(updated_at >= created_at)
);

CREATE TABLE cancellation_state_history (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  cancellation_id text NOT NULL,
  from_status text,
  to_status text NOT NULL,
  reason_code text NOT NULL,
  actor_id text NOT NULL,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(id),
  CONSTRAINT cancellation_state_history_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT cancellation_state_history_fk FOREIGN KEY(organization_id,workspace_id,cancellation_id) REFERENCES order_cancellations(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT cancellation_state_history_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT cancellation_state_history_reason_chk CHECK(reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT cancellation_state_history_ref_chk CHECK(actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
);

CREATE TABLE return_items (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  return_id text NOT NULL,
  order_item_id text NOT NULL,
  requested_coefficient bigint NOT NULL,
  requested_scale smallint NOT NULL,
  received_coefficient bigint NOT NULL DEFAULT 0,
  received_scale smallint NOT NULL,
  accepted_coefficient bigint NOT NULL DEFAULT 0,
  accepted_scale smallint NOT NULL,
  unit text NOT NULL,
  disposition text NOT NULL DEFAULT 'quarantine',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT return_items_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT return_items_return_line_key UNIQUE(organization_id,workspace_id,return_id,order_item_id),
  CONSTRAINT return_items_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_items_return_fk FOREIGN KEY(organization_id,workspace_id,return_id) REFERENCES commerce_returns(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_items_order_item_fk FOREIGN KEY(organization_id,workspace_id,order_item_id) REFERENCES order_items(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_items_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT return_items_quantity_chk CHECK(requested_coefficient > 0 AND received_coefficient >= 0 AND accepted_coefficient >= 0 AND requested_scale BETWEEN 0 AND 9 AND received_scale BETWEEN 0 AND 9 AND accepted_scale BETWEEN 0 AND 9 AND (requested_scale = 0 OR requested_coefficient % 10 <> 0) AND (received_scale = 0 OR received_coefficient = 0 OR received_coefficient % 10 <> 0) AND (accepted_scale = 0 OR accepted_coefficient = 0 OR accepted_coefficient % 10 <> 0) AND received_coefficient::numeric * power(10::numeric, requested_scale) <= requested_coefficient::numeric * power(10::numeric, received_scale) AND accepted_coefficient::numeric * power(10::numeric, requested_scale) <= received_coefficient::numeric * power(10::numeric, accepted_scale)),
  CONSTRAINT return_items_unit_chk CHECK(unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT return_items_disposition_chk CHECK(disposition IN ('restock','quarantine','scrap','replace')),
  CONSTRAINT return_items_version_chk CHECK(version >= 1),
  CONSTRAINT return_items_time_chk CHECK(updated_at >= created_at)
);

CREATE TABLE return_state_history (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  return_id text NOT NULL,
  from_status text,
  to_status text NOT NULL,
  reason_code text NOT NULL,
  actor_id text NOT NULL,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(id),
  CONSTRAINT return_state_history_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT return_state_history_return_fk FOREIGN KEY(organization_id,workspace_id,return_id) REFERENCES commerce_returns(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_state_history_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT return_state_history_reason_chk CHECK(reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT return_state_history_ref_chk CHECK(actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
);

CREATE TABLE return_inspections (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  return_id text NOT NULL,
  return_item_id text NOT NULL,
  outcome text NOT NULL,
  condition_code text NOT NULL,
  discrepancy_code text,
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  unit text NOT NULL,
  disposition text NOT NULL,
  artifact_ref text,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(id),
  CONSTRAINT return_inspections_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT return_inspections_return_fk FOREIGN KEY(organization_id,workspace_id,return_id) REFERENCES commerce_returns(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_inspections_item_fk FOREIGN KEY(organization_id,workspace_id,return_item_id) REFERENCES return_items(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT return_inspections_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT return_inspections_outcome_chk CHECK(outcome IN ('accepted','partially_accepted','rejected')),
  CONSTRAINT return_inspections_reason_chk CHECK(condition_code ~ '^[a-z][a-z0-9_]{0,63}$' AND (discrepancy_code IS NULL OR discrepancy_code ~ '^[a-z][a-z0-9_]{0,63}$')),
  CONSTRAINT return_inspections_quantity_chk CHECK(quantity_coefficient >= 0 AND quantity_scale BETWEEN 0 AND 9),
  CONSTRAINT return_inspections_unit_chk CHECK(unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT return_inspections_disposition_chk CHECK(disposition IN ('restock','quarantine','scrap','replace')),
  CONSTRAINT return_inspections_artifact_chk CHECK(artifact_ref IS NULL OR artifact_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$')
);

CREATE TABLE refund_allocations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  payment_id text NOT NULL,
  refund_id text NOT NULL,
  return_id text NOT NULL,
  order_item_id text,
  component text NOT NULL,
  amount_minor_units bigint NOT NULL,
  currency char(3) NOT NULL,
  idempotency_key text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(id),
  CONSTRAINT refund_allocations_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT refund_allocations_idempotency_key UNIQUE(organization_id,workspace_id,idempotency_key),
  CONSTRAINT refund_allocations_payment_fk FOREIGN KEY(organization_id,workspace_id,payment_id) REFERENCES payments(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT refund_allocations_refund_fk FOREIGN KEY(organization_id,workspace_id,refund_id) REFERENCES payment_refunds(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT refund_allocations_return_fk FOREIGN KEY(organization_id,workspace_id,return_id) REFERENCES commerce_returns(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT refund_allocations_order_item_fk FOREIGN KEY(organization_id,workspace_id,order_item_id) REFERENCES order_items(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT refund_allocations_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT refund_allocations_ref_chk CHECK(payment_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND refund_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND return_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT refund_allocations_component_chk CHECK(component IN ('line','shipping','tax','discount')),
  CONSTRAINT refund_allocations_amount_chk CHECK(amount_minor_units > 0 AND currency ~ '^[A-Z]{3}$'),
  CONSTRAINT refund_allocations_version_chk CHECK(version >= 1)
);

CREATE TABLE commerce_operation_evidence (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  operation_type text NOT NULL,
  operation_id text NOT NULL,
  outcome text NOT NULL,
  reason_code text,
  remote_id text,
  digest char(64) NOT NULL,
  correlation_id text NOT NULL,
  causation_id text,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY(id),
  CONSTRAINT commerce_operation_evidence_tenant_id_key UNIQUE(organization_id,workspace_id,id),
  CONSTRAINT commerce_operation_evidence_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT commerce_operation_evidence_id_chk CHECK(id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT commerce_operation_evidence_type_chk CHECK(operation_type ~ '^[a-z][a-z0-9_]{0,63}$' AND operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND outcome ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT commerce_operation_evidence_reason_chk CHECK(reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CONSTRAINT commerce_operation_evidence_remote_chk CHECK(remote_id IS NULL OR remote_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'),
  CONSTRAINT commerce_operation_evidence_digest_chk CHECK(digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT commerce_operation_evidence_ref_chk CHECK(correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND (causation_id IS NULL OR causation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'))
);

CREATE INDEX order_cancellations_due_idx ON order_cancellations(organization_id,workspace_id,status,updated_at,id);
CREATE INDEX commerce_returns_status_idx ON commerce_returns(organization_id,workspace_id,status,updated_at,id);
CREATE INDEX return_items_return_idx ON return_items(organization_id,workspace_id,return_id,id);
CREATE INDEX refund_allocations_payment_idx ON refund_allocations(organization_id,workspace_id,payment_id,created_at,id);
CREATE INDEX commerce_operation_evidence_operation_idx ON commerce_operation_evidence(organization_id,workspace_id,operation_type,operation_id,occurred_at,id);

ALTER TABLE order_cancellations ENABLE ROW LEVEL SECURITY; ALTER TABLE order_cancellations FORCE ROW LEVEL SECURITY;
ALTER TABLE commerce_returns ENABLE ROW LEVEL SECURITY; ALTER TABLE commerce_returns FORCE ROW LEVEL SECURITY;
ALTER TABLE cancellation_state_history ENABLE ROW LEVEL SECURITY; ALTER TABLE cancellation_state_history FORCE ROW LEVEL SECURITY;
ALTER TABLE return_items ENABLE ROW LEVEL SECURITY; ALTER TABLE return_items FORCE ROW LEVEL SECURITY;
ALTER TABLE return_state_history ENABLE ROW LEVEL SECURITY; ALTER TABLE return_state_history FORCE ROW LEVEL SECURITY;
ALTER TABLE return_inspections ENABLE ROW LEVEL SECURITY; ALTER TABLE return_inspections FORCE ROW LEVEL SECURITY;
ALTER TABLE refund_allocations ENABLE ROW LEVEL SECURITY; ALTER TABLE refund_allocations FORCE ROW LEVEL SECURITY;
ALTER TABLE commerce_operation_evidence ENABLE ROW LEVEL SECURITY; ALTER TABLE commerce_operation_evidence FORCE ROW LEVEL SECURITY;

CREATE POLICY order_cancellations_tenant_all ON order_cancellations FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY commerce_returns_tenant_all ON commerce_returns FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY cancellation_state_history_tenant_all ON cancellation_state_history FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY return_items_tenant_all ON return_items FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY return_state_history_tenant_all ON return_state_history FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY return_inspections_tenant_all ON return_inspections FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY refund_allocations_tenant_all ON refund_allocations FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY commerce_operation_evidence_tenant_all ON commerce_operation_evidence FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION commerce_returns_reject_mutation() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''return evidence is append-only''; RETURN NULL; END';
CREATE TRIGGER cancellation_state_history_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON cancellation_state_history FOR EACH STATEMENT EXECUTE FUNCTION commerce_returns_reject_mutation();
CREATE TRIGGER return_state_history_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON return_state_history FOR EACH STATEMENT EXECUTE FUNCTION commerce_returns_reject_mutation();
CREATE TRIGGER return_inspections_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON return_inspections FOR EACH STATEMENT EXECUTE FUNCTION commerce_returns_reject_mutation();
CREATE TRIGGER operation_evidence_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON commerce_operation_evidence FOR EACH STATEMENT EXECUTE FUNCTION commerce_returns_reject_mutation();
REVOKE DELETE,TRUNCATE ON cancellation_state_history,return_state_history,return_inspections,commerce_operation_evidence FROM PUBLIC;

COMMENT ON TABLE order_cancellations IS 'Tenant-scoped cancellation state; remote unknown outcomes require reconciliation and are never blindly retried.';
COMMENT ON TABLE commerce_returns IS 'Tenant-scoped return approval and inspection lifecycle, separate from order snapshot and payment state.';
COMMENT ON TABLE refund_allocations IS 'Append-oriented links between a payment refund and order/return components; exact amounts only.';
COMMENT ON TABLE commerce_operation_evidence IS 'Bounded opaque evidence for remote operations; raw provider payloads and credentials are excluded.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
