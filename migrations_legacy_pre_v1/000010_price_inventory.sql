BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE prices (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  offer_id text NOT NULL,
  kind text NOT NULL,
  minor_units bigint NOT NULL,
  currency text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT prices_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT prices_kind_chk CHECK (kind IN ('regular','compare_at','cost')),
  CONSTRAINT prices_amount_chk CHECK (minor_units >= 0),
  CONSTRAINT prices_currency_chk CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT prices_version_chk CHECK (version >= 1),
  CONSTRAINT prices_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT prices_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT prices_offer_fk FOREIGN KEY (organization_id, workspace_id, offer_id)
    REFERENCES offers (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT prices_tenant_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT prices_offer_kind_currency_key UNIQUE (organization_id, workspace_id, offer_id, kind, currency)
);

CREATE TABLE warehouses (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT warehouses_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT warehouses_code_chk CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT warehouses_name_chk CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 300),
  CONSTRAINT warehouses_status_chk CHECK (status IN ('active','disabled')),
  CONSTRAINT warehouses_version_chk CHECK (version >= 1),
  CONSTRAINT warehouses_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT warehouses_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT warehouses_tenant_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT warehouses_tenant_code_key UNIQUE (organization_id, workspace_id, code)
);

CREATE TABLE inventory_positions (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  offer_id text NOT NULL,
  warehouse_id text NOT NULL,
  on_hand_coefficient bigint NOT NULL DEFAULT 0,
  on_hand_scale smallint NOT NULL DEFAULT 0,
  reserved_coefficient bigint NOT NULL DEFAULT 0,
  reserved_scale smallint NOT NULL DEFAULT 0,
  unit text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT inventory_positions_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT inventory_positions_unit_chk CHECK (unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT inventory_positions_scale_chk CHECK (on_hand_scale BETWEEN 0 AND 9 AND reserved_scale BETWEEN 0 AND 9),
  CONSTRAINT inventory_positions_nonnegative_chk CHECK (on_hand_coefficient >= 0 AND reserved_coefficient >= 0),
  CONSTRAINT inventory_positions_canonical_on_hand_chk CHECK (
    (on_hand_coefficient = 0 AND on_hand_scale = 0)
    OR (on_hand_coefficient <> 0 AND (on_hand_scale = 0 OR on_hand_coefficient % 10 <> 0))
  ),
  CONSTRAINT inventory_positions_canonical_reserved_chk CHECK (
    (reserved_coefficient = 0 AND reserved_scale = 0)
    OR (reserved_coefficient <> 0 AND (reserved_scale = 0 OR reserved_coefficient % 10 <> 0))
  ),
  CONSTRAINT inventory_positions_reserved_lte_on_hand_chk CHECK (
    reserved_coefficient::numeric * power(10::numeric, on_hand_scale)
    <= on_hand_coefficient::numeric * power(10::numeric, reserved_scale)
  ),
  CONSTRAINT inventory_positions_version_chk CHECK (version >= 1),
  CONSTRAINT inventory_positions_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT inventory_positions_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT inventory_positions_offer_fk FOREIGN KEY (organization_id, workspace_id, offer_id)
    REFERENCES offers (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT inventory_positions_warehouse_fk FOREIGN KEY (organization_id, workspace_id, warehouse_id)
    REFERENCES warehouses (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT inventory_positions_tenant_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT inventory_positions_offer_warehouse_key UNIQUE (organization_id, workspace_id, offer_id, warehouse_id)
);

CREATE INDEX prices_offer_idx ON prices (organization_id, workspace_id, offer_id, kind, currency);
CREATE INDEX inventory_positions_offer_idx ON inventory_positions (organization_id, workspace_id, offer_id, warehouse_id);
CREATE INDEX inventory_positions_warehouse_idx ON inventory_positions (organization_id, workspace_id, warehouse_id, offer_id);

ALTER TABLE prices ENABLE ROW LEVEL SECURITY;
ALTER TABLE prices FORCE ROW LEVEL SECURITY;
CREATE POLICY prices_tenant_select ON prices FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY prices_tenant_insert ON prices FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY prices_tenant_update ON prices FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

ALTER TABLE warehouses ENABLE ROW LEVEL SECURITY;
ALTER TABLE warehouses FORCE ROW LEVEL SECURITY;
CREATE POLICY warehouses_tenant_select ON warehouses FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouses_tenant_insert ON warehouses FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY warehouses_tenant_update ON warehouses FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

ALTER TABLE inventory_positions ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory_positions FORCE ROW LEVEL SECURITY;
CREATE POLICY inventory_positions_tenant_select ON inventory_positions FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY inventory_positions_tenant_insert ON inventory_positions FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY inventory_positions_tenant_update ON inventory_positions FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON prices, warehouses, inventory_positions FROM PUBLIC;

CREATE FUNCTION price_inventory_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''price/inventory state cannot be hard-deleted''; RETURN NULL; END';
CREATE TRIGGER prices_no_delete BEFORE DELETE ON prices FOR EACH ROW EXECUTE FUNCTION price_inventory_reject_delete();
CREATE TRIGGER warehouses_no_delete BEFORE DELETE ON warehouses FOR EACH ROW EXECUTE FUNCTION price_inventory_reject_delete();
CREATE TRIGGER inventory_positions_no_delete BEFORE DELETE ON inventory_positions FOR EACH ROW EXECUTE FUNCTION price_inventory_reject_delete();
CREATE TRIGGER prices_no_clear BEFORE TRUNCATE ON prices FOR EACH STATEMENT EXECUTE FUNCTION price_inventory_reject_delete();
CREATE TRIGGER warehouses_no_clear BEFORE TRUNCATE ON warehouses FOR EACH STATEMENT EXECUTE FUNCTION price_inventory_reject_delete();
CREATE TRIGGER inventory_positions_no_clear BEFORE TRUNCATE ON inventory_positions FOR EACH STATEMENT EXECUTE FUNCTION price_inventory_reject_delete();

CREATE FUNCTION price_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE offer_status text;
BEGIN
  SELECT status INTO offer_status FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.offer_id;
  IF offer_status IS NULL OR offer_status = ''archived'' THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''price requires non-archived offer'';
  END IF;
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new price must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.currency IS DISTINCT FROM OLD.currency
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''price identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''price version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER prices_guard_insert BEFORE INSERT ON prices FOR EACH ROW EXECUTE FUNCTION price_guard();
CREATE TRIGGER prices_guard_update BEFORE UPDATE ON prices FOR EACH ROW EXECUTE FUNCTION price_guard();

CREATE FUNCTION warehouse_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.status <> ''active'' OR NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new warehouse must start active at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.code IS DISTINCT FROM OLD.code OR NEW.name IS DISTINCT FROM OLD.name OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''warehouse identity/content is immutable in Task 005'';
  END IF;
  IF NEW.status = OLD.status OR NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''warehouse status transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER warehouses_guard_insert BEFORE INSERT ON warehouses FOR EACH ROW EXECUTE FUNCTION warehouse_guard();
CREATE TRIGGER warehouses_guard_update BEFORE UPDATE ON warehouses FOR EACH ROW EXECUTE FUNCTION warehouse_guard();

CREATE FUNCTION inventory_position_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE offer_status text; warehouse_status text;
BEGIN
  SELECT status INTO offer_status FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.offer_id;
  SELECT status INTO warehouse_status FROM warehouses WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.warehouse_id;
  IF offer_status IS NULL OR warehouse_status IS NULL THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position parent is unavailable'';
  END IF;
  IF TG_OP = ''INSERT'' THEN
    IF offer_status = ''archived'' OR warehouse_status <> ''active'' THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position requires active parents'';
    END IF;
    IF NEW.version <> 1 OR NEW.on_hand_coefficient <> 0 OR NEW.on_hand_scale <> 0 OR NEW.reserved_coefficient <> 0 OR NEW.reserved_scale <> 0 THEN
      RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new inventory position must start zero at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.offer_id IS DISTINCT FROM OLD.offer_id OR NEW.warehouse_id IS DISTINCT FROM OLD.warehouse_id OR NEW.unit IS DISTINCT FROM OLD.unit
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inventory position version transition is invalid'';
  END IF;
  IF (offer_status = ''archived'' OR warehouse_status <> ''active'') AND NOT (
    NEW.on_hand_coefficient = OLD.on_hand_coefficient AND NEW.on_hand_scale = OLD.on_hand_scale
    AND (NEW.reserved_coefficient::numeric * power(10::numeric, OLD.reserved_scale)
         <= OLD.reserved_coefficient::numeric * power(10::numeric, NEW.reserved_scale))
  ) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''inactive parent permits reservation release only'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER inventory_positions_guard_insert BEFORE INSERT ON inventory_positions FOR EACH ROW EXECUTE FUNCTION inventory_position_guard();
CREATE TRIGGER inventory_positions_guard_update BEFORE UPDATE ON inventory_positions FOR EACH ROW EXECUTE FUNCTION inventory_position_guard();

COMMENT ON TABLE prices IS 'Canonical provider-neutral prices. Money is exact bigint minor units + currency; provider price IDs never belong here.';
COMMENT ON TABLE inventory_positions IS 'Canonical exact current stock position. Append-only WMS movement ledger remains Task 054.';
COMMENT ON TABLE warehouses IS 'Minimal canonical warehouse identity required by InventoryPosition; WMS-specific workflows remain Task 054.';

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
