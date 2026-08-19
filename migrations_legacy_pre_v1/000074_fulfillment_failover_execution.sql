BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fulfillment_allocations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  allocation_id text NOT NULL,
  idempotency_key text NOT NULL,
  order_id text NOT NULL,
  order_item_id text NOT NULL,
  offer_id text NOT NULL,
  warehouse_id text NOT NULL,
  quantity_coefficient bigint NOT NULL CHECK(quantity_coefficient>0),
  quantity_scale smallint NOT NULL CHECK(quantity_scale BETWEEN 0 AND 9),
  unit text NOT NULL CHECK(unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  state text NOT NULL CHECK(state IN ('reserved','released','consumed','cancelled')),
  reason_code text NOT NULL DEFAULT '',
  incident_id text,
  replaces_allocation_id text,
  version bigint NOT NULL DEFAULT 1 CHECK(version>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(organization_id,workspace_id,allocation_id),
  UNIQUE(organization_id,workspace_id,idempotency_key),
  FOREIGN KEY(organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,order_item_id) REFERENCES order_items(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY(organization_id,workspace_id,incident_id) REFERENCES warehouse_incidents(organization_id,workspace_id,incident_id),
  FOREIGN KEY(organization_id,workspace_id,replaces_allocation_id) REFERENCES fulfillment_allocations(organization_id,workspace_id,allocation_id),
  CHECK(quantity_scale=0 OR quantity_coefficient % 10 <> 0),
  CHECK(reason_code='' OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  CHECK(replaces_allocation_id IS NULL OR replaces_allocation_id<>allocation_id),
  CHECK(updated_at>=created_at)
);
CREATE UNIQUE INDEX fulfillment_allocations_one_reserved_item_idx
  ON fulfillment_allocations(organization_id,workspace_id,order_item_id)
  WHERE state='reserved';
CREATE INDEX fulfillment_allocations_warehouse_offer_idx
  ON fulfillment_allocations(organization_id,workspace_id,warehouse_id,offer_id,state,order_item_id);
CREATE INDEX fulfillment_allocations_incident_idx
  ON fulfillment_allocations(organization_id,workspace_id,incident_id,created_at,allocation_id)
  WHERE incident_id IS NOT NULL;

CREATE FUNCTION fulfillment_allocations_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE
  item_order text; item_offer text; item_q bigint; item_s smallint; item_unit text; order_state text;
BEGIN
  SELECT i.order_id,i.offer_id,i.quantity_coefficient,i.quantity_scale,i.unit,o.status
    INTO item_order,item_offer,item_q,item_s,item_unit,order_state
    FROM order_items i JOIN orders o ON o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id AND o.id=i.order_id
    WHERE i.organization_id=NEW.organization_id AND i.workspace_id=NEW.workspace_id AND i.id=NEW.order_item_id;
  IF item_order IS NULL OR item_order<>NEW.order_id OR item_offer<>NEW.offer_id OR item_q<>NEW.quantity_coefficient OR item_s<>NEW.quantity_scale OR item_unit<>NEW.unit THEN
    RAISE EXCEPTION USING ERRCODE=''23514'',MESSAGE=''fulfillment allocation must exactly match immutable order item'';
  END IF;
  IF TG_OP=''INSERT'' THEN
    IF NEW.state<>''reserved'' OR NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new fulfillment allocation must start reserved at version 1''; END IF;
    IF order_state IN (''fulfilled'',''cancelled'') THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''terminal order cannot receive allocation''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.allocation_id IS DISTINCT FROM OLD.allocation_id OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR NEW.order_id IS DISTINCT FROM OLD.order_id OR NEW.order_item_id IS DISTINCT FROM OLD.order_item_id OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.warehouse_id IS DISTINCT FROM OLD.warehouse_id OR NEW.quantity_coefficient IS DISTINCT FROM OLD.quantity_coefficient OR NEW.quantity_scale IS DISTINCT FROM OLD.quantity_scale OR NEW.unit IS DISTINCT FROM OLD.unit OR NEW.incident_id IS DISTINCT FROM OLD.incident_id OR NEW.replaces_allocation_id IS DISTINCT FROM OLD.replaces_allocation_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''fulfillment allocation identity is immutable'';
  END IF;
  IF OLD.state<>''reserved'' OR NEW.state NOT IN (''released'',''consumed'',''cancelled'') OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''fulfillment allocation state transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER fulfillment_allocations_guard_insert BEFORE INSERT ON fulfillment_allocations FOR EACH ROW EXECUTE FUNCTION fulfillment_allocations_guard();
CREATE TRIGGER fulfillment_allocations_guard_update BEFORE UPDATE ON fulfillment_allocations FOR EACH ROW EXECUTE FUNCTION fulfillment_allocations_guard();

ALTER TABLE fulfillment_allocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE fulfillment_allocations FORCE ROW LEVEL SECURITY;
CREATE POLICY fulfillment_allocations_tenant_all ON fulfillment_allocations FOR ALL
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON fulfillment_allocations FROM PUBLIC;

ALTER TABLE warehouse_incident_decisions
  ADD COLUMN execution_status text NOT NULL DEFAULT 'not_required' CHECK(execution_status IN ('not_required','rerouted','needs_attention')),
  ADD COLUMN execution_reason text NOT NULL DEFAULT '' CHECK(execution_reason='' OR execution_reason IN ('untracked_reservation','insufficient_capacity','allocation_conflict')),
  ADD COLUMN rerouted_allocations integer NOT NULL DEFAULT 0 CHECK(rerouted_allocations>=0);
ALTER TABLE warehouse_incidents
  ADD COLUMN rerouted_allocation_count integer NOT NULL DEFAULT 0 CHECK(rerouted_allocation_count>=0),
  ADD COLUMN execution_attention_count integer NOT NULL DEFAULT 0 CHECK(execution_attention_count>=0);

COMMENT ON TABLE fulfillment_allocations IS 'Durable order-item reservation ownership. Warehouse failover releases the source allocation and creates a new destination allocation; physical on-hand stock is never transferred by this table.';
COMMENT ON COLUMN fulfillment_allocations.replaces_allocation_id IS 'Immutable lineage pointer used by failover rerouting. A reroute creates a new allocation rather than rewriting warehouse identity.';
COMMENT ON COLUMN warehouse_incident_decisions.execution_status IS 'Execution evidence for tracked reservations. needs_attention is fail-closed and never fabricates a destination reservation.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
