BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE warehouse_operational_state (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  warehouse_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('active','degraded','unavailable','lost')),
  reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,warehouse_id),
  FOREIGN KEY (organization_id,workspace_id,warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id)
);
CREATE TABLE warehouse_failover_routes (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  source_warehouse_id text NOT NULL,
  destination_warehouse_id text NOT NULL,
  priority integer NOT NULL CHECK (priority BETWEEN 1 AND 10000),
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,source_warehouse_id,destination_warehouse_id),
  UNIQUE (organization_id,workspace_id,source_warehouse_id,priority),
  FOREIGN KEY (organization_id,workspace_id,source_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,destination_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  CHECK (source_warehouse_id<>destination_warehouse_id)
);
CREATE TABLE warehouse_failover_decisions (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  decision_id text NOT NULL,
  source_warehouse_id text NOT NULL,
  destination_warehouse_id text,
  offer_id text NOT NULL,
  result text NOT NULL CHECK (result IN ('routed','no_eligible_destination')),
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,decision_id),
  FOREIGN KEY (organization_id,workspace_id,source_warehouse_id) REFERENCES warehouses(organization_id,workspace_id,id),
  FOREIGN KEY (organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id)
);

ALTER TABLE warehouse_operational_state ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_operational_state FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_failover_routes ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_failover_routes FORCE ROW LEVEL SECURITY;
ALTER TABLE warehouse_failover_decisions ENABLE ROW LEVEL SECURITY; ALTER TABLE warehouse_failover_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY warehouse_operational_state_tenant_all ON warehouse_operational_state FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_failover_routes_tenant_all ON warehouse_failover_routes FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouse_failover_decisions_tenant_all ON warehouse_failover_decisions FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
REVOKE DELETE,TRUNCATE ON warehouse_operational_state,warehouse_failover_routes,warehouse_failover_decisions FROM PUBLIC;
REVOKE UPDATE ON warehouse_failover_decisions FROM PUBLIC;

CREATE OR REPLACE FUNCTION inventory_position_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE offer_status text; warehouse_status text; operational_state text;
BEGIN
  SELECT status INTO offer_status FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.offer_id;
  SELECT status INTO warehouse_status FROM warehouses WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.warehouse_id;
  SELECT state INTO operational_state FROM warehouse_operational_state WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND warehouse_id=NEW.warehouse_id;
  operational_state := COALESCE(operational_state,''active'');
  IF offer_status IS NULL OR warehouse_status IS NULL THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position parent is unavailable''; END IF;
  IF TG_OP = ''INSERT'' THEN
    IF offer_status = ''archived'' OR warehouse_status <> ''active'' OR operational_state IN (''unavailable'',''lost'') THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position requires active operational parents''; END IF;
    IF NEW.version <> 1 OR NEW.on_hand_coefficient <> 0 OR NEW.on_hand_scale <> 0 OR NEW.reserved_coefficient <> 0 OR NEW.reserved_scale <> 0 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position must start zero at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.warehouse_id IS DISTINCT FROM OLD.warehouse_id OR NEW.unit IS DISTINCT FROM OLD.unit OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position identity is immutable''; END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position version transition is invalid''; END IF;
  IF (offer_status = ''archived'' OR warehouse_status <> ''active'' OR operational_state IN (''unavailable'',''lost'')) AND NOT (NEW.on_hand_coefficient = OLD.on_hand_coefficient AND NEW.on_hand_scale = OLD.on_hand_scale AND (NEW.reserved_coefficient::numeric * power(10::numeric, OLD.reserved_scale) <= OLD.reserved_coefficient::numeric * power(10::numeric, NEW.reserved_scale))) THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inactive operational parent permits reservation release only''; END IF;
  RETURN NEW;
END';

COMMENT ON TABLE warehouse_operational_state IS 'Operational health state. LOST/UNAVAILABLE blocks new stock reservations but never fabricates stock movement.';
COMMENT ON TABLE warehouse_failover_routes IS 'Operator-approved failover candidates. Resolver still requires destination ACTIVE/DEGRADED and positive ATP for the same offer.';
COMMENT ON TABLE warehouse_failover_decisions IS 'Append-only evidence of routing decisions; no stock is transferred by this table.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
