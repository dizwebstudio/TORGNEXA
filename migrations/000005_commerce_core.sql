BEGIN;

-- TORGNEXA pre-v1 baseline component 000005: commerce_core.
-- Squashed, statement-order-preserving source range: legacy 000009..000017.
-- Do not edit by hand; regenerate with scripts/generate-pre-v1-baseline.py.

-- BASELINE_SOURCE_BEGIN

-- SOURCE 000009_catalog_domain.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION catalog_valid_gtin(value text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT
AS 'DECLARE
  idx integer;
  total integer := 0;
  weight integer := 3;
  expected integer;
BEGIN
  IF char_length(value) NOT IN (8,12,13,14) OR value !~ ''^[0-9]+$'' THEN
    RETURN false;
  END IF;
  idx := char_length(value) - 1;
  WHILE idx >= 1 LOOP
    total := total + substring(value FROM idx FOR 1)::integer * weight;
    IF weight = 3 THEN weight := 1; ELSE weight := 3; END IF;
    idx := idx - 1;
  END LOOP;
  expected := (10 - (total % 10)) % 10;
  RETURN expected = substring(value FROM char_length(value) FOR 1)::integer;
END';

CREATE TABLE products (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT products_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT products_code_chk CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT products_title_chk CHECK (title = btrim(title) AND char_length(title) BETWEEN 1 AND 300),
  CONSTRAINT products_description_chk CHECK (description = btrim(description) AND char_length(description) <= 20000),
  CONSTRAINT products_status_chk CHECK (status IN ('draft','active','archived')),
  CONSTRAINT products_version_chk CHECK (version >= 1),
  CONSTRAINT products_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT products_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT products_tenant_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT products_tenant_code_key UNIQUE (organization_id, workspace_id, code)
);

CREATE TABLE offers (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  sku text NOT NULL,
  gtin text,
  status text NOT NULL DEFAULT 'draft',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT offers_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT offers_sku_chk CHECK (sku ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT offers_gtin_chk CHECK (gtin IS NULL OR catalog_valid_gtin(gtin)),
  CONSTRAINT offers_status_chk CHECK (status IN ('draft','active','archived')),
  CONSTRAINT offers_version_chk CHECK (version >= 1),
  CONSTRAINT offers_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT offers_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT offers_product_fk FOREIGN KEY (organization_id, workspace_id, product_id)
    REFERENCES products (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT offers_tenant_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT offers_tenant_sku_key UNIQUE (organization_id, workspace_id, sku)
);
CREATE UNIQUE INDEX offers_tenant_gtin_key
  ON offers (organization_id, workspace_id, gtin) WHERE gtin IS NOT NULL;

-- Connector mappings are the only persistence bridge from provider-neutral
-- local identity to remote identity. Core Product/Offer rows never grow provider IDs.
ALTER TABLE connector_accounts
  ADD CONSTRAINT connector_accounts_tenant_id_key
  UNIQUE (organization_id, workspace_id, id);

CREATE TABLE connector_entity_mappings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  connector_account_id text NOT NULL,
  entity_type text NOT NULL,
  local_entity_id text NOT NULL,
  remote_id text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT connector_entity_mappings_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT connector_entity_mappings_account_fk FOREIGN KEY (organization_id, workspace_id, connector_account_id)
    REFERENCES connector_accounts (organization_id, workspace_id, id) ON DELETE RESTRICT,
  CONSTRAINT connector_entity_mappings_type_chk CHECK (entity_type IN ('product','offer')),
  CONSTRAINT connector_entity_mappings_local_id_chk CHECK (
    local_entity_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR local_entity_id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT connector_entity_mappings_remote_id_chk CHECK (
    remote_id = btrim(remote_id)
    AND char_length(remote_id) BETWEEN 1 AND 512
    AND remote_id !~ '[[:cntrl:]]'
  ),
  CONSTRAINT connector_entity_mappings_version_chk CHECK (version >= 1),
  CONSTRAINT connector_entity_mappings_timestamps_chk CHECK (updated_at >= created_at),
  PRIMARY KEY (organization_id, workspace_id, connector_account_id, entity_type, local_entity_id),
  UNIQUE (organization_id, workspace_id, connector_account_id, entity_type, remote_id)
);

CREATE INDEX products_tenant_status_idx ON products (organization_id, workspace_id, status, id);
CREATE INDEX offers_product_status_idx ON offers (organization_id, workspace_id, product_id, status, id);
CREATE INDEX connector_entity_mappings_local_idx ON connector_entity_mappings (organization_id, workspace_id, entity_type, local_entity_id);

ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE ROW LEVEL SECURITY;
CREATE POLICY products_tenant_select ON products FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY products_tenant_insert ON products FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY products_tenant_update ON products FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

ALTER TABLE offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE offers FORCE ROW LEVEL SECURITY;
CREATE POLICY offers_tenant_select ON offers FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY offers_tenant_insert ON offers FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY offers_tenant_update ON offers FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

ALTER TABLE connector_entity_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_entity_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_entity_mappings_tenant_select ON connector_entity_mappings FOR SELECT USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_entity_mappings_tenant_insert ON connector_entity_mappings FOR INSERT WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);
CREATE POLICY connector_entity_mappings_tenant_update ON connector_entity_mappings FOR UPDATE USING (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
) WITH CHECK (
  organization_id = current_setting('app.organization_id', true)
  AND workspace_id = current_setting('app.workspace_id', true)
);

REVOKE DELETE, TRUNCATE ON products, offers, connector_entity_mappings FROM PUBLIC;

CREATE FUNCTION catalog_product_guard_update() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.status <> ''draft'' OR NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''new product must start draft at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.code IS DISTINCT FROM OLD.code
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''product identity is immutable'';
  END IF;
  IF OLD.status = ''archived'' THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''archived product is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''product version transition is invalid'';
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    IF NOT ((OLD.status = ''draft'' AND NEW.status IN (''active'',''archived'')) OR (OLD.status = ''active'' AND NEW.status = ''archived'')) THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''product lifecycle transition is invalid'';
    END IF;
    IF NEW.title IS DISTINCT FROM OLD.title OR NEW.description IS DISTINCT FROM OLD.description THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''product status change cannot mutate content'';
    END IF;
    IF NEW.status = ''archived'' AND EXISTS (
      SELECT 1 FROM offers WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id
        AND product_id=OLD.id AND status <> ''archived''
    ) THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''product has non-archived offers'';
    END IF;
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER products_guard_insert BEFORE INSERT ON products FOR EACH ROW EXECUTE FUNCTION catalog_product_guard_update();
CREATE TRIGGER products_guard_update BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION catalog_product_guard_update();

CREATE FUNCTION catalog_offer_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE parent_status text;
BEGIN
  SELECT status INTO parent_status FROM products
    WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.product_id;
  IF parent_status IS NULL OR parent_status = ''archived'' THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''offer parent product is unavailable'';
  END IF;
  IF TG_OP = ''INSERT'' THEN
    IF NEW.status <> ''draft'' OR NEW.version <> 1 THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''new offer must start draft at version 1'';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.product_id IS DISTINCT FROM OLD.product_id
     OR NEW.sku IS DISTINCT FROM OLD.sku OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''offer identity is immutable'';
  END IF;
  IF OLD.status = ''archived'' THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''archived offer is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''offer version transition is invalid'';
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    IF NOT ((OLD.status = ''draft'' AND NEW.status IN (''active'',''archived'')) OR (OLD.status = ''active'' AND NEW.status = ''archived'')) THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''offer lifecycle transition is invalid'';
    END IF;
    IF NEW.gtin IS DISTINCT FROM OLD.gtin THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''offer status change cannot mutate identifiers'';
    END IF;
    IF NEW.status = ''active'' AND parent_status <> ''active'' THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''active offer requires active product'';
    END IF;
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER offers_guard_insert BEFORE INSERT ON offers FOR EACH ROW EXECUTE FUNCTION catalog_offer_guard();
CREATE TRIGGER offers_guard_update BEFORE UPDATE ON offers FOR EACH ROW EXECUTE FUNCTION catalog_offer_guard();

CREATE FUNCTION connector_entity_mappings_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'DECLARE present boolean;
BEGIN
  IF TG_OP = ''INSERT'' AND NEW.version <> 1 THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''new connector mapping must start at version 1'';
  END IF;
  IF TG_OP = ''UPDATE'' THEN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.entity_type IS DISTINCT FROM OLD.entity_type
       OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector mapping identity is immutable'';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector mapping version transition is invalid'';
    END IF;
  END IF;
  IF NEW.entity_type = ''product'' THEN
    SELECT EXISTS(SELECT 1 FROM products WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSE
    SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  END IF;
  IF NOT present THEN
    RAISE EXCEPTION USING ERRCODE = ''23503'', MESSAGE = ''connector mapping local entity does not exist'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER connector_entity_mappings_guard_insert BEFORE INSERT ON connector_entity_mappings FOR EACH ROW EXECUTE FUNCTION connector_entity_mappings_guard();
CREATE TRIGGER connector_entity_mappings_guard_update BEFORE UPDATE ON connector_entity_mappings FOR EACH ROW EXECUTE FUNCTION connector_entity_mappings_guard();

CREATE FUNCTION catalog_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''catalog history cannot be hard-deleted''; RETURN NULL; END';
CREATE TRIGGER products_no_delete BEFORE DELETE ON products FOR EACH ROW EXECUTE FUNCTION catalog_reject_delete();
CREATE TRIGGER offers_no_delete BEFORE DELETE ON offers FOR EACH ROW EXECUTE FUNCTION catalog_reject_delete();
CREATE TRIGGER connector_entity_mappings_no_delete BEFORE DELETE ON connector_entity_mappings FOR EACH ROW EXECUTE FUNCTION catalog_reject_delete();
CREATE TRIGGER products_no_clear BEFORE TRUNCATE ON products FOR EACH STATEMENT EXECUTE FUNCTION catalog_reject_delete();
CREATE TRIGGER offers_no_clear BEFORE TRUNCATE ON offers FOR EACH STATEMENT EXECUTE FUNCTION catalog_reject_delete();
CREATE TRIGGER connector_entity_mappings_no_clear BEFORE TRUNCATE ON connector_entity_mappings FOR EACH STATEMENT EXECUTE FUNCTION catalog_reject_delete();

COMMENT ON TABLE products IS 'Canonical tenant-scoped Product masters. Provider IDs/fields are forbidden; external identity belongs in connector_entity_mappings.';
COMMENT ON TABLE offers IS 'Canonical tenant-scoped sellable variations. Price and inventory remain separate Task-005 domains.';
COMMENT ON TABLE connector_entity_mappings IS 'Provider-neutral local-to-remote identity bridge owned by connector runtime; Product/Offer rows never contain provider-specific IDs.';

-- SOURCE 000010_price_inventory.sql
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

-- SOURCE 000011_orders.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE orders (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  order_number text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  currency text NOT NULL,
  subtotal_minor_units bigint NOT NULL,
  discount_minor_units bigint NOT NULL,
  tax_minor_units bigint NOT NULL,
  shipping_minor_units bigint NOT NULL,
  grand_minor_units bigint NOT NULL,
  placed_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT orders_id_sortable_chk CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT orders_number_chk CHECK (order_number ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT orders_status_chk CHECK (status IN ('pending','confirmed','processing','fulfilled','cancelled')),
  CONSTRAINT orders_currency_chk CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT orders_amounts_chk CHECK (subtotal_minor_units >= 0 AND discount_minor_units >= 0 AND tax_minor_units >= 0 AND shipping_minor_units >= 0 AND grand_minor_units >= 0 AND discount_minor_units <= subtotal_minor_units),
  CONSTRAINT orders_version_chk CHECK (version >= 1),
  CONSTRAINT orders_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT orders_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT orders_tenant_id_key UNIQUE (organization_id,workspace_id,id),
  CONSTRAINT orders_tenant_number_key UNIQUE (organization_id,workspace_id,order_number)
);

CREATE TABLE order_items (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  order_id text NOT NULL,
  position integer NOT NULL,
  offer_id text NOT NULL,
  sku_snapshot text NOT NULL,
  quantity_coefficient bigint NOT NULL,
  quantity_scale smallint NOT NULL,
  unit text NOT NULL,
  unit_price_minor_units bigint NOT NULL,
  subtotal_minor_units bigint NOT NULL,
  discount_minor_units bigint NOT NULL,
  tax_minor_units bigint NOT NULL,
  line_total_minor_units bigint NOT NULL,
  tax_jurisdiction text NOT NULL,
  tax_category text NOT NULL,
  tax_rate_coefficient bigint NOT NULL,
  tax_rate_scale smallint NOT NULL,
  price_includes_tax boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT order_items_id_sortable_chk CHECK (id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'),
  CONSTRAINT order_items_position_chk CHECK (position > 0),
  CONSTRAINT order_items_sku_chk CHECK (sku_snapshot ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  CONSTRAINT order_items_quantity_chk CHECK (quantity_coefficient > 0 AND quantity_scale BETWEEN 0 AND 9 AND (quantity_scale = 0 OR quantity_coefficient % 10 <> 0)),
  CONSTRAINT order_items_unit_chk CHECK (unit ~ '^[A-Z][A-Z0-9._-]{0,15}$'),
  CONSTRAINT order_items_amounts_chk CHECK (unit_price_minor_units >= 0 AND subtotal_minor_units >= 0 AND discount_minor_units >= 0 AND tax_minor_units >= 0 AND line_total_minor_units >= 0 AND discount_minor_units <= subtotal_minor_units),
  CONSTRAINT order_items_tax_chk CHECK (
    tax_jurisdiction ~ '^[A-Z]{2}(-[A-Z0-9]{1,12})?$'
    AND tax_category IN ('standard','reduced','zero')
    AND tax_rate_coefficient >= 0 AND tax_rate_scale BETWEEN 0 AND 9
    AND (tax_rate_scale = 0 OR tax_rate_coefficient % 10 <> 0 OR tax_rate_coefficient = 0)
    AND tax_rate_coefficient::numeric <= power(10::numeric,tax_rate_scale)
    AND (tax_category <> 'zero' OR tax_rate_coefficient = 0)
  ),
  CONSTRAINT order_items_line_total_chk CHECK (
    (price_includes_tax AND line_total_minor_units = subtotal_minor_units - discount_minor_units)
    OR
    (NOT price_includes_tax AND line_total_minor_units = subtotal_minor_units - discount_minor_units + tax_minor_units)
  ),
  CONSTRAINT order_items_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT order_items_order_fk FOREIGN KEY (organization_id,workspace_id,order_id) REFERENCES orders(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT order_items_offer_fk FOREIGN KEY (organization_id,workspace_id,offer_id) REFERENCES offers(organization_id,workspace_id,id) ON DELETE RESTRICT,
  CONSTRAINT order_items_tenant_id_key UNIQUE (organization_id,workspace_id,id),
  CONSTRAINT order_items_order_position_key UNIQUE (organization_id,workspace_id,order_id,position)
);

CREATE INDEX orders_tenant_status_idx ON orders(organization_id,workspace_id,status,placed_at,id);
CREATE INDEX order_items_order_idx ON order_items(organization_id,workspace_id,order_id,position,id);
CREATE INDEX order_items_offer_idx ON order_items(organization_id,workspace_id,offer_id,order_id);

ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE ROW LEVEL SECURITY;
CREATE POLICY orders_tenant_select ON orders FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY orders_tenant_insert ON orders FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY orders_tenant_update ON orders FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

ALTER TABLE order_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_items FORCE ROW LEVEL SECURITY;
CREATE POLICY order_items_tenant_select ON order_items FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY order_items_tenant_insert ON order_items FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON orders, order_items FROM PUBLIC;

CREATE FUNCTION orders_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.status <> ''pending'' OR NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new order must start pending at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.order_number IS DISTINCT FROM OLD.order_number OR NEW.currency IS DISTINCT FROM OLD.currency
     OR NEW.subtotal_minor_units IS DISTINCT FROM OLD.subtotal_minor_units OR NEW.discount_minor_units IS DISTINCT FROM OLD.discount_minor_units
     OR NEW.tax_minor_units IS DISTINCT FROM OLD.tax_minor_units OR NEW.shipping_minor_units IS DISTINCT FROM OLD.shipping_minor_units
     OR NEW.grand_minor_units IS DISTINCT FROM OLD.grand_minor_units OR NEW.placed_at IS DISTINCT FROM OLD.placed_at OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order commercial snapshot is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order version transition is invalid''; END IF;
  IF NOT ((OLD.status=''pending'' AND NEW.status IN (''confirmed'',''cancelled'')) OR (OLD.status=''confirmed'' AND NEW.status IN (''processing'',''cancelled'')) OR (OLD.status=''processing'' AND NEW.status IN (''fulfilled'',''cancelled''))) THEN
    RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order lifecycle transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER orders_guard_insert BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION orders_guard();
CREATE TRIGGER orders_guard_update BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION orders_guard();

CREATE FUNCTION order_items_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE offer_status text; BEGIN
  IF TG_OP <> ''INSERT'' THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order items are immutable''; END IF;
  SELECT status INTO offer_status FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.offer_id;
  IF offer_status IS DISTINCT FROM ''active'' THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order item requires active offer''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER order_items_guard_insert BEFORE INSERT ON order_items FOR EACH ROW EXECUTE FUNCTION order_items_guard();

CREATE FUNCTION validate_order_totals(org text, ws text, oid text) RETURNS void LANGUAGE plpgsql AS 'DECLARE s_sub bigint; s_disc bigint; s_tax bigint; s_line bigint; o_sub bigint; o_disc bigint; o_tax bigint; o_ship bigint; o_grand bigint; item_count bigint; BEGIN
  SELECT subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units INTO o_sub,o_disc,o_tax,o_ship,o_grand FROM orders WHERE organization_id=org AND workspace_id=ws AND id=oid;
  SELECT count(*),COALESCE(sum(subtotal_minor_units),0),COALESCE(sum(discount_minor_units),0),COALESCE(sum(tax_minor_units),0),COALESCE(sum(line_total_minor_units),0) INTO item_count,s_sub,s_disc,s_tax,s_line FROM order_items WHERE organization_id=org AND workspace_id=ws AND order_id=oid;
  IF item_count < 1 OR o_sub<>s_sub OR o_disc<>s_disc OR o_tax<>s_tax OR o_grand<>s_line+o_ship THEN RAISE EXCEPTION USING ERRCODE=''23514'',MESSAGE=''order totals do not match immutable items''; END IF;
END';
CREATE FUNCTION orders_totals_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN PERFORM validate_order_totals(NEW.organization_id,NEW.workspace_id,NEW.id); RETURN NEW; END';
CREATE FUNCTION order_items_totals_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN PERFORM validate_order_totals(NEW.organization_id,NEW.workspace_id,NEW.order_id); RETURN NEW; END';
CREATE CONSTRAINT TRIGGER orders_totals_guard AFTER INSERT ON orders DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION orders_totals_guard();
CREATE CONSTRAINT TRIGGER order_items_totals_guard AFTER INSERT ON order_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION order_items_totals_guard();

CREATE FUNCTION orders_reject_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''order history cannot be hard-deleted''; RETURN NULL; END';
CREATE TRIGGER orders_no_delete BEFORE DELETE ON orders FOR EACH ROW EXECUTE FUNCTION orders_reject_delete();
CREATE TRIGGER order_items_no_delete BEFORE DELETE ON order_items FOR EACH ROW EXECUTE FUNCTION orders_reject_delete();
CREATE TRIGGER orders_no_clear BEFORE TRUNCATE ON orders FOR EACH STATEMENT EXECUTE FUNCTION orders_reject_delete();
CREATE TRIGGER order_items_no_clear BEFORE TRUNCATE ON order_items FOR EACH STATEMENT EXECUTE FUNCTION orders_reject_delete();
CREATE TRIGGER order_items_no_update BEFORE UPDATE ON order_items FOR EACH ROW EXECUTE FUNCTION orders_reject_delete();

-- Expand the generic connector identity bridge to canonical Orders. The v1
-- Product/Offer contract remains valid; this only broadens the allowed type.
ALTER TABLE connector_entity_mappings DROP CONSTRAINT connector_entity_mappings_type_chk;
ALTER TABLE connector_entity_mappings ADD CONSTRAINT connector_entity_mappings_type_chk CHECK (entity_type IN ('product','offer','order'));

CREATE OR REPLACE FUNCTION connector_entity_mappings_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE present boolean; BEGIN
  IF TG_OP = ''INSERT'' AND NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new connector mapping must start at version 1''; END IF;
  IF TG_OP = ''UPDATE'' THEN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.entity_type IS DISTINCT FROM OLD.entity_type OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping identity is immutable''; END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping version transition is invalid''; END IF;
  END IF;
  IF NEW.entity_type=''product'' THEN SELECT EXISTS(SELECT 1 FROM products WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSIF NEW.entity_type=''offer'' THEN SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSE SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  END IF;
  IF NOT present THEN RAISE EXCEPTION USING ERRCODE=''23503'',MESSAGE=''connector mapping local entity does not exist''; END IF;
  RETURN NEW;
END';

COMMENT ON TABLE orders IS 'Canonical provider-neutral immutable commercial Order snapshot; provider statuses and remote IDs stay in connectors.';
COMMENT ON TABLE order_items IS 'Immutable canonical OrderItem snapshots with exact quantities, money and tax facts.';

-- SOURCE 000012_connector_sdk.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 010 stabilizes the tenant-owned Connector SDK account metadata without
-- introducing provider credential columns. The legacy `provider` column is the
-- canonical connector manifest id; a future contract migration may rename it,
-- but expand compatibility keeps existing readers/writers intact here.
ALTER TABLE connector_accounts
  ADD COLUMN version bigint NOT NULL DEFAULT 1,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN health_status text NOT NULL DEFAULT 'unknown',
  ADD COLUMN health_reason_code text,
  ADD COLUMN health_checked_at timestamptz;

ALTER TABLE connector_accounts
  ADD CONSTRAINT connector_accounts_family_v1_chk CHECK (
    family IN ('marketplace','classified','social','erp','edo','government','payment','logistics','pickup','fx','notification')
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_provider_v1_chk CHECK (
    provider ~ '^[a-z0-9][a-z0-9-]{0,62}$'
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_status_v1_chk CHECK (
    status IN ('disabled','active','suspended','error')
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_version_v1_chk CHECK (version >= 1) NOT VALID,
  ADD CONSTRAINT connector_accounts_updated_at_v1_chk CHECK (updated_at >= created_at) NOT VALID,
  ADD CONSTRAINT connector_accounts_health_v1_chk CHECK (
    (
      health_status = 'unknown'
      AND health_reason_code IS NULL
      AND health_checked_at IS NULL
    ) OR (
      health_status = 'healthy'
      AND health_reason_code IS NULL
      AND health_checked_at IS NOT NULL
    ) OR (
      health_status IN ('degraded','unavailable')
      AND health_reason_code ~ '^[a-z][a-z0-9._-]{0,63}$'
      AND health_checked_at IS NOT NULL
    )
  ) NOT VALID,
  ADD CONSTRAINT connector_accounts_health_time_v1_chk CHECK (
    health_checked_at IS NULL OR health_checked_at >= created_at
  ) NOT VALID;

ALTER TABLE connector_accounts
  VALIDATE CONSTRAINT connector_accounts_family_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_provider_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_status_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_version_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_updated_at_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_health_v1_chk,
  VALIDATE CONSTRAINT connector_accounts_health_time_v1_chk;

CREATE INDEX connector_accounts_manifest_status_idx
  ON connector_accounts (organization_id, workspace_id, provider, status, id);

CREATE INDEX connector_accounts_health_idx
  ON connector_accounts (organization_id, workspace_id, health_status, health_checked_at, id);

REVOKE DELETE, TRUNCATE ON connector_accounts FROM PUBLIC;

CREATE FUNCTION connector_accounts_sdk_guard() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 OR NEW.status <> ''disabled'' OR NEW.health_status <> ''unknown''
       OR NEW.health_reason_code IS NOT NULL OR NEW.health_checked_at IS NOT NULL THEN
      RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''new connector account must start disabled/unknown at version 1'';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.family IS DISTINCT FROM OLD.family
     OR NEW.provider IS DISTINCT FROM OLD.provider
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account identity is immutable'';
  END IF;

  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account version transition is invalid'';
  END IF;

  IF OLD.status = ''disabled'' AND NEW.status NOT IN (''disabled'',''active'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''active'' AND NEW.status NOT IN (''active'',''disabled'',''suspended'',''error'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''suspended'' AND NEW.status NOT IN (''suspended'',''active'',''disabled'',''error'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  ELSIF OLD.status = ''error'' AND NEW.status NOT IN (''error'',''active'',''disabled'',''suspended'') THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account lifecycle transition is invalid'';
  END IF;

  IF OLD.health_checked_at IS NOT NULL AND NEW.health_checked_at IS NOT NULL
     AND NEW.health_checked_at < OLD.health_checked_at THEN
    RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector health timestamp cannot move backwards'';
  END IF;

  RETURN NEW;
END';

CREATE TRIGGER connector_accounts_sdk_guard_insert
  BEFORE INSERT ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_sdk_guard();
CREATE TRIGGER connector_accounts_sdk_guard_update
  BEFORE UPDATE ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_sdk_guard();

CREATE FUNCTION connector_accounts_reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE = ''55000'', MESSAGE = ''connector account history cannot be hard-deleted'';
  RETURN NULL;
END';

CREATE TRIGGER connector_accounts_no_delete
  BEFORE DELETE ON connector_accounts
  FOR EACH ROW EXECUTE FUNCTION connector_accounts_reject_delete();
CREATE TRIGGER connector_accounts_no_clear
  BEFORE TRUNCATE ON connector_accounts
  FOR EACH STATEMENT EXECUTE FUNCTION connector_accounts_reject_delete();

COMMENT ON COLUMN connector_accounts.provider IS 'Canonical Connector SDK manifest id. Provider-specific branching in Core is forbidden.';
COMMENT ON COLUMN connector_accounts.secret_reference IS 'Opaque Task-021 secret handle only; plaintext credentials are forbidden.';
COMMENT ON COLUMN connector_accounts.health_reason_code IS 'Normalized safe reason code only. Raw provider errors/responses are forbidden.';
COMMENT ON TABLE connector_accounts IS 'Tenant-scoped Connector SDK account binding with optimistic version and normalized health metadata.';

-- SOURCE 000013_approval_engine.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE approval_policies (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  version bigint NOT NULL CHECK (version >= 1),
  name text NOT NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  minimum_risk text NOT NULL CHECK (minimum_risk IN ('read','write_safe','write_sensitive','legally_significant')),
  minimum_risk_rank smallint NOT NULL CHECK (minimum_risk_rank BETWEEN 1 AND 4),
  request_ttl_seconds integer NOT NULL CHECK (request_ttl_seconds BETWEEN 60 AND 2592000),
  escalate_after_seconds integer NOT NULL DEFAULT 0 CHECK (escalate_after_seconds >= 0 AND escalate_after_seconds < request_ttl_seconds),
  separation_of_duties boolean NOT NULL DEFAULT true,
  stages jsonb NOT NULL,
  active boolean NOT NULL DEFAULT true,
  retired_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id, version),
  CONSTRAINT approval_policies_workspace_fk FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT approval_policies_retirement CHECK ((active AND retired_at IS NULL) OR (NOT active AND retired_at IS NOT NULL)),
  CONSTRAINT approval_policies_stages_array CHECK (jsonb_typeof(stages) = 'array' AND jsonb_array_length(stages) BETWEEN 1 AND 16)
);

CREATE UNIQUE INDEX approval_policies_one_active_action
  ON approval_policies(organization_id, workspace_id, action, resource_type)
  WHERE active;
CREATE INDEX approval_policies_match_idx
  ON approval_policies(organization_id, workspace_id, action, resource_type, minimum_risk_rank DESC, version DESC)
  WHERE active;

CREATE TABLE approval_requests (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL,
  requester_id text NOT NULL,
  source text NOT NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id text NOT NULL,
  correlation_id text NOT NULL,
  risk text NOT NULL CHECK (risk IN ('read','write_safe','write_sensitive','legally_significant')),
  state text NOT NULL CHECK (state IN ('pending','approved','rejected','expired','cancelled','executing','completed','failed')),
  current_stage smallint NOT NULL CHECK (current_stage >= 1),
  expires_at timestamptz NOT NULL,
  next_escalation_at timestamptz,
  escalation_count integer NOT NULL DEFAULT 0 CHECK (escalation_count >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  requested_at timestamptz NOT NULL,
  approved_at timestamptz,
  rejected_at timestamptz,
  execution_started_at timestamptz,
  completed_at timestamptz,
  failure_code text,
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT approval_requests_workspace_fk FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT approval_requests_policy_fk FOREIGN KEY (organization_id, workspace_id, policy_id, policy_version) REFERENCES approval_policies(organization_id,workspace_id,id,version),
  CONSTRAINT approval_requests_expiry CHECK (expires_at > requested_at),
  CONSTRAINT approval_requests_escalation CHECK (next_escalation_at IS NULL OR (next_escalation_at > requested_at AND next_escalation_at < expires_at)),
  CONSTRAINT approval_requests_failure CHECK ((state = 'failed' AND failure_code IS NOT NULL) OR (state <> 'failed' AND failure_code IS NULL))
);
CREATE INDEX approval_requests_pending_idx ON approval_requests(organization_id,workspace_id,state,expires_at,next_escalation_at) WHERE state='pending';
CREATE INDEX approval_requests_resource_idx ON approval_requests(organization_id,workspace_id,resource_type,resource_id,requested_at DESC);

CREATE TABLE approval_decisions (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  request_id text NOT NULL,
  stage smallint NOT NULL CHECK (stage >= 1),
  actor_id text NOT NULL,
  decision text NOT NULL CHECK (decision IN ('approve','reject')),
  actor_scopes jsonb NOT NULL,
  comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 1024),
  decided_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT approval_decisions_request_fk FOREIGN KEY (organization_id,workspace_id,request_id) REFERENCES approval_requests(organization_id,workspace_id,id),
  UNIQUE (organization_id, workspace_id, request_id, stage, actor_id),
  CONSTRAINT approval_decisions_scopes_array CHECK (jsonb_typeof(actor_scopes)='array' AND jsonb_array_length(actor_scopes) BETWEEN 1 AND 128)
);
CREATE INDEX approval_decisions_request_idx ON approval_decisions(organization_id,workspace_id,request_id,stage,decided_at);

CREATE TABLE approval_escalations (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  request_id text NOT NULL,
  stage smallint NOT NULL CHECK (stage >= 1),
  escalation_number integer NOT NULL CHECK (escalation_number >= 1),
  escalated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT approval_escalations_request_fk FOREIGN KEY (organization_id,workspace_id,request_id) REFERENCES approval_requests(organization_id,workspace_id,id),
  UNIQUE (organization_id, workspace_id, request_id, escalation_number)
);

ALTER TABLE approval_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE approval_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_decisions FORCE ROW LEVEL SECURITY;
ALTER TABLE approval_escalations ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_escalations FORCE ROW LEVEL SECURITY;

CREATE POLICY approval_policies_select ON approval_policies FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_policies_insert ON approval_policies FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_policies_update ON approval_policies FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_requests_select ON approval_requests FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_requests_insert ON approval_requests FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_requests_update ON approval_requests FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_decisions_select ON approval_decisions FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_decisions_insert ON approval_decisions FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_escalations_select ON approval_escalations FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY approval_escalations_insert ON approval_escalations FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION approval_policy_validate_insert() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE
  expected_rank integer;
  stage_json jsonb;
  idx integer;
  scope_count integer;
  distinct_scope_count integer;
BEGIN
  expected_rank := CASE NEW.minimum_risk WHEN ''read'' THEN 1 WHEN ''write_safe'' THEN 2 WHEN ''write_sensitive'' THEN 3 WHEN ''legally_significant'' THEN 4 ELSE 0 END;
  IF NEW.minimum_risk_rank <> expected_rank THEN RAISE EXCEPTION ''approval policy risk rank mismatch''; END IF;
  FOR idx IN 0..jsonb_array_length(NEW.stages)-1 LOOP
    stage_json := NEW.stages -> idx;
    IF jsonb_typeof(stage_json) <> ''object'' THEN RAISE EXCEPTION ''approval stage must be object''; END IF;
    IF (stage_json->>''number'')::integer <> idx+1 THEN RAISE EXCEPTION ''approval stage numbers must be sequential''; END IF;
    IF COALESCE(stage_json->>''name'','''') !~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'' THEN RAISE EXCEPTION ''approval stage name invalid''; END IF;
    IF (stage_json->>''required_approvals'')::integer NOT BETWEEN 1 AND 32 THEN RAISE EXCEPTION ''approval stage quorum invalid''; END IF;
    IF jsonb_typeof(stage_json->''eligible_scopes'') <> ''array'' OR jsonb_array_length(stage_json->''eligible_scopes'') NOT BETWEEN 1 AND 64 THEN RAISE EXCEPTION ''approval stage scopes invalid''; END IF;
    SELECT count(*),count(DISTINCT scope) INTO scope_count,distinct_scope_count FROM jsonb_array_elements_text(stage_json->''eligible_scopes'') x(scope) WHERE scope ~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'';
    IF scope_count <> jsonb_array_length(stage_json->''eligible_scopes'') OR distinct_scope_count <> scope_count THEN RAISE EXCEPTION ''approval stage scopes must be canonical and unique''; END IF;
  END LOOP;
  RETURN NEW;
END ';
CREATE TRIGGER approval_policy_validate BEFORE INSERT ON approval_policies FOR EACH ROW EXECUTE FUNCTION approval_policy_validate_insert();

CREATE FUNCTION approval_request_validate_insert() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE
  p approval_policies%ROWTYPE;
  expected_rank integer;
  expected_escalation timestamptz;
BEGIN
  SELECT * INTO p FROM approval_policies WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.policy_id AND version=NEW.policy_version;
  IF NOT FOUND OR NOT p.active THEN RAISE EXCEPTION ''approval request requires active policy version''; END IF;
  expected_rank := CASE NEW.risk WHEN ''read'' THEN 1 WHEN ''write_safe'' THEN 2 WHEN ''write_sensitive'' THEN 3 WHEN ''legally_significant'' THEN 4 ELSE 0 END;
  IF NEW.action<>p.action OR NEW.resource_type<>p.resource_type OR expected_rank<p.minimum_risk_rank THEN RAISE EXCEPTION ''approval request does not match policy''; END IF;
  IF NEW.state<>''pending'' OR NEW.current_stage<>1 OR NEW.version<>1 OR NEW.escalation_count<>0 THEN RAISE EXCEPTION ''new approval request must start pending at stage/version 1''; END IF;
  IF NEW.expires_at<>NEW.requested_at + make_interval(secs=>p.request_ttl_seconds) THEN RAISE EXCEPTION ''approval request expiry must match policy TTL''; END IF;
  expected_escalation := CASE WHEN p.escalate_after_seconds=0 THEN NULL ELSE NEW.requested_at + make_interval(secs=>p.escalate_after_seconds) END;
  IF NEW.next_escalation_at IS DISTINCT FROM expected_escalation THEN RAISE EXCEPTION ''approval request escalation must match policy''; END IF;
  RETURN NEW;
END ';
CREATE TRIGGER approval_request_validate BEFORE INSERT ON approval_requests FOR EACH ROW EXECUTE FUNCTION approval_request_validate_insert();

CREATE FUNCTION approval_policy_guard() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
  IF TG_OP=''DELETE'' THEN RAISE EXCEPTION ''approval policies cannot be deleted''; END IF;
  IF OLD.organization_id<>NEW.organization_id OR OLD.workspace_id<>NEW.workspace_id OR OLD.id<>NEW.id OR OLD.version<>NEW.version OR OLD.name<>NEW.name OR OLD.action<>NEW.action OR OLD.resource_type<>NEW.resource_type OR OLD.minimum_risk<>NEW.minimum_risk OR OLD.minimum_risk_rank<>NEW.minimum_risk_rank OR OLD.request_ttl_seconds<>NEW.request_ttl_seconds OR OLD.escalate_after_seconds<>NEW.escalate_after_seconds OR OLD.separation_of_duties<>NEW.separation_of_duties OR OLD.stages<>NEW.stages OR OLD.created_at<>NEW.created_at THEN RAISE EXCEPTION ''approval policy version is immutable''; END IF;
  IF OLD.active=false OR NEW.active=true OR NEW.retired_at IS NULL THEN RAISE EXCEPTION ''approval policy can only transition active to retired''; END IF;
  RETURN NEW;
END ';
CREATE TRIGGER approval_policy_immutable BEFORE UPDATE OR DELETE ON approval_policies FOR EACH ROW EXECUTE FUNCTION approval_policy_guard();

CREATE FUNCTION approval_decision_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE
  requester text;
  request_state text;
  current_stage integer;
  separation boolean;
  stages_json jsonb;
  stage_json jsonb;
  eligible boolean;
  scope_count integer;
  distinct_scope_count integer;
BEGIN
  SELECT r.requester_id,r.state,r.current_stage,p.separation_of_duties,p.stages
    INTO requester,request_state,current_stage,separation,stages_json
  FROM approval_requests r
  JOIN approval_policies p ON p.organization_id=r.organization_id AND p.workspace_id=r.workspace_id AND p.id=r.policy_id AND p.version=r.policy_version
  WHERE r.organization_id=NEW.organization_id AND r.workspace_id=NEW.workspace_id AND r.id=NEW.request_id;
  IF requester IS NULL THEN RAISE EXCEPTION ''approval request/policy not found''; END IF;
  IF request_state<>''pending'' OR NEW.stage<>current_stage THEN RAISE EXCEPTION ''approval decision must target current pending stage''; END IF;
  SELECT count(*),count(DISTINCT scope) INTO scope_count,distinct_scope_count FROM jsonb_array_elements_text(NEW.actor_scopes) x(scope) WHERE scope ~ ''^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$'';
  IF scope_count<>jsonb_array_length(NEW.actor_scopes) OR distinct_scope_count<>scope_count THEN RAISE EXCEPTION ''approver scopes must be canonical and unique''; END IF;
  IF separation AND requester=NEW.actor_id THEN RAISE EXCEPTION ''requester cannot approve own request''; END IF;
  IF NEW.stage < 1 OR NEW.stage > jsonb_array_length(stages_json) THEN RAISE EXCEPTION ''approval decision stage out of range''; END IF;
  stage_json := stages_json -> (NEW.stage-1);
  SELECT EXISTS(
    SELECT 1 FROM jsonb_array_elements_text(NEW.actor_scopes) a(scope)
    JOIN jsonb_array_elements_text(stage_json->''eligible_scopes'') e(scope) USING(scope)
  ) INTO eligible;
  IF NOT eligible THEN RAISE EXCEPTION ''approver scope not eligible for stage''; END IF;
  RETURN NEW;
END ';
CREATE TRIGGER approval_decision_validate BEFORE INSERT ON approval_decisions FOR EACH ROW EXECUTE FUNCTION approval_decision_guard();

CREATE FUNCTION approval_evidence_immutable() RETURNS trigger LANGUAGE plpgsql AS ' BEGIN RAISE EXCEPTION ''approval evidence is append-only''; END ';
CREATE TRIGGER approval_decisions_immutable BEFORE UPDATE OR DELETE ON approval_decisions FOR EACH ROW EXECUTE FUNCTION approval_evidence_immutable();
CREATE TRIGGER approval_escalations_immutable BEFORE UPDATE OR DELETE ON approval_escalations FOR EACH ROW EXECUTE FUNCTION approval_evidence_immutable();

CREATE FUNCTION approval_request_guard() RETURNS trigger LANGUAGE plpgsql AS '
DECLARE
  stages_json jsonb;
  required_count integer;
  actual_count integer;
  stage_no integer;
BEGIN
  IF TG_OP=''DELETE'' THEN RAISE EXCEPTION ''approval requests cannot be deleted''; END IF;
  IF OLD.organization_id<>NEW.organization_id OR OLD.workspace_id<>NEW.workspace_id OR OLD.id<>NEW.id OR OLD.policy_id<>NEW.policy_id OR OLD.policy_version<>NEW.policy_version OR OLD.requester_id<>NEW.requester_id OR OLD.source<>NEW.source OR OLD.action<>NEW.action OR OLD.resource_type<>NEW.resource_type OR OLD.resource_id<>NEW.resource_id OR OLD.correlation_id<>NEW.correlation_id OR OLD.risk<>NEW.risk OR OLD.requested_at<>NEW.requested_at OR OLD.expires_at<>NEW.expires_at THEN RAISE EXCEPTION ''approval request identity is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.escalation_count<OLD.escalation_count OR NEW.current_stage<OLD.current_stage OR NEW.current_stage>OLD.current_stage+1 THEN RAISE EXCEPTION ''approval request progression invalid''; END IF;
  IF NOT (
    (OLD.state=''pending'' AND NEW.state IN (''pending'',''approved'',''rejected'',''expired'',''cancelled'')) OR
    (OLD.state=''approved'' AND NEW.state IN (''executing'',''expired'',''cancelled'')) OR
    (OLD.state=''executing'' AND NEW.state IN (''completed'',''failed''))
  ) THEN RAISE EXCEPTION ''approval state transition invalid''; END IF;

  SELECT stages INTO stages_json FROM approval_policies
   WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND id=OLD.policy_id AND version=OLD.policy_version;

  IF OLD.state=''pending'' AND NEW.current_stage=OLD.current_stage+1 THEN
    required_count := ((stages_json -> (OLD.current_stage-1) ->> ''required_approvals''))::integer;
    SELECT count(*) INTO actual_count FROM approval_decisions
      WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND request_id=OLD.id AND stage=OLD.current_stage AND decision=''approve'';
    IF actual_count < required_count THEN RAISE EXCEPTION ''approval stage quorum not satisfied''; END IF;
    IF EXISTS (SELECT 1 FROM approval_decisions WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND request_id=OLD.id AND stage=OLD.current_stage AND decision=''reject'') THEN RAISE EXCEPTION ''rejected approval stage cannot advance''; END IF;
  END IF;

  IF NEW.state=''approved'' THEN
    FOR stage_no IN 1..jsonb_array_length(stages_json) LOOP
      required_count := ((stages_json -> (stage_no-1) ->> ''required_approvals''))::integer;
      SELECT count(*) INTO actual_count FROM approval_decisions
       WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND request_id=OLD.id AND stage=stage_no AND decision=''approve'';
      IF actual_count < required_count THEN RAISE EXCEPTION ''approval request quorum incomplete''; END IF;
      IF EXISTS (SELECT 1 FROM approval_decisions WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND request_id=OLD.id AND stage=stage_no AND decision=''reject'') THEN RAISE EXCEPTION ''rejected request cannot be approved''; END IF;
    END LOOP;
  END IF;
  IF NEW.state=''rejected'' AND NOT EXISTS (SELECT 1 FROM approval_decisions WHERE organization_id=OLD.organization_id AND workspace_id=OLD.workspace_id AND request_id=OLD.id AND decision=''reject'') THEN
    RAISE EXCEPTION ''rejected state requires immutable reject decision'';
  END IF;
  IF NEW.state=''failed'' AND NEW.failure_code IS NULL THEN RAISE EXCEPTION ''failed execution requires failure code''; END IF;
  RETURN NEW;
END ';
CREATE TRIGGER approval_request_progression BEFORE UPDATE OR DELETE ON approval_requests FOR EACH ROW EXECUTE FUNCTION approval_request_guard();

CREATE FUNCTION approval_no_clear() RETURNS trigger LANGUAGE plpgsql AS ' BEGIN RAISE EXCEPTION ''approval history cannot be cleared''; END ';
CREATE TRIGGER approval_policies_no_clear BEFORE TRUNCATE ON approval_policies EXECUTE FUNCTION approval_no_clear();
CREATE TRIGGER approval_requests_no_clear BEFORE TRUNCATE ON approval_requests EXECUTE FUNCTION approval_no_clear();
CREATE TRIGGER approval_decisions_no_clear BEFORE TRUNCATE ON approval_decisions EXECUTE FUNCTION approval_no_clear();
CREATE TRIGGER approval_escalations_no_clear BEFORE TRUNCATE ON approval_escalations EXECUTE FUNCTION approval_no_clear();

-- SOURCE 000014_data_lineage.sql
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

-- SOURCE 000015_pim_mdm.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pim_brands (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  name text NOT NULL CHECK (name=btrim(name) AND char_length(name) BETWEEN 1 AND 300 AND name !~ '[[:cntrl:]]'),
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_brands_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE (organization_id,workspace_id,code)
);

CREATE TABLE pim_categories (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  name text NOT NULL CHECK (name=btrim(name) AND char_length(name) BETWEEN 1 AND 300 AND name !~ '[[:cntrl:]]'),
  parent_id text,
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_categories_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT pim_categories_parent_fk FOREIGN KEY (organization_id,workspace_id,parent_id) REFERENCES pim_categories(organization_id,workspace_id,id),
  CONSTRAINT pim_categories_not_self CHECK (parent_id IS NULL OR parent_id <> id),
  UNIQUE (organization_id,workspace_id,code)
);

CREATE TABLE pim_attributes (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  name text NOT NULL CHECK (name=btrim(name) AND char_length(name) BETWEEN 1 AND 300 AND name !~ '[[:cntrl:]]'),
  value_type text NOT NULL CHECK (value_type IN ('text','integer','decimal','boolean','date','datetime','reference')),
  multi_value boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_attributes_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE (organization_id,workspace_id,code)
);

CREATE TABLE pim_product_brands (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  brand_id text NOT NULL,
  source text NOT NULL CHECK (source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,product_id),
  CONSTRAINT pim_product_brands_product_fk FOREIGN KEY (organization_id,workspace_id,product_id) REFERENCES products(organization_id,workspace_id,id),
  CONSTRAINT pim_product_brands_brand_fk FOREIGN KEY (organization_id,workspace_id,brand_id) REFERENCES pim_brands(organization_id,workspace_id,id)
);

CREATE TABLE pim_product_categories (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  category_id text NOT NULL,
  is_primary boolean NOT NULL DEFAULT false,
  source text NOT NULL CHECK (source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,product_id,category_id),
  CONSTRAINT pim_product_categories_product_fk FOREIGN KEY (organization_id,workspace_id,product_id) REFERENCES products(organization_id,workspace_id,id),
  CONSTRAINT pim_product_categories_category_fk FOREIGN KEY (organization_id,workspace_id,category_id) REFERENCES pim_categories(organization_id,workspace_id,id)
);
CREATE UNIQUE INDEX pim_product_categories_primary_idx ON pim_product_categories(organization_id,workspace_id,product_id) WHERE active AND is_primary;

CREATE TABLE pim_product_attribute_values (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  attribute_id text NOT NULL,
  ordinal smallint NOT NULL DEFAULT 0 CHECK (ordinal BETWEEN 0 AND 255),
  value jsonb NOT NULL,
  source text NOT NULL CHECK (source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,product_id,attribute_id,ordinal),
  CONSTRAINT pim_product_attribute_values_product_fk FOREIGN KEY (organization_id,workspace_id,product_id) REFERENCES products(organization_id,workspace_id,id),
  CONSTRAINT pim_product_attribute_values_attribute_fk FOREIGN KEY (organization_id,workspace_id,attribute_id) REFERENCES pim_attributes(organization_id,workspace_id,id),
  CONSTRAINT pim_product_attribute_values_scalar CHECK (jsonb_typeof(value) IN ('string','number','boolean'))
);

CREATE TABLE pim_field_authorities (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  entity_type text NOT NULL CHECK (entity_type IN ('brand','category','attribute')),
  field_path text NOT NULL CHECK (field_path ~ '^[a-z][a-z0-9_.-]{0,127}$'),
  source text NOT NULL CHECK (source ~ '^[a-z][a-z0-9._:/-]{0,127}$'),
  priority integer NOT NULL CHECK (priority BETWEEN 0 AND 10000),
  active boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_field_authorities_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE (organization_id,workspace_id,entity_type,field_path,source)
);

CREATE TABLE pim_duplicate_candidates (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  entity_type text NOT NULL CHECK (entity_type IN ('brand','category','attribute')),
  left_id text NOT NULL,
  right_id text NOT NULL,
  score_bps integer NOT NULL CHECK (score_bps BETWEEN 0 AND 10000),
  signals jsonb NOT NULL,
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open','confirmed','not_duplicate','merged')),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_duplicate_candidates_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT pim_duplicate_candidates_order CHECK (left_id < right_id),
  CONSTRAINT pim_duplicate_candidates_signals CHECK (jsonb_typeof(signals)='array' AND jsonb_array_length(signals) BETWEEN 1 AND 16),
  UNIQUE (organization_id,workspace_id,entity_type,left_id,right_id)
);

CREATE TABLE pim_merge_previews (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  entity_type text NOT NULL CHECK (entity_type IN ('brand','category','attribute')),
  target_id text NOT NULL,
  source_id text NOT NULL,
  target_version text NOT NULL CHECK (char_length(target_version) BETWEEN 1 AND 128),
  source_version text NOT NULL CHECK (char_length(source_version) BETWEEN 1 AND 128),
  fields jsonb NOT NULL,
  has_conflicts boolean NOT NULL,
  fingerprint_sha256 text NOT NULL CHECK (fingerprint_sha256 ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,id),
  CONSTRAINT pim_merge_previews_workspace_fk FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT pim_merge_previews_pair CHECK (target_id <> source_id),
  CONSTRAINT pim_merge_previews_id_fingerprint CHECK (id = 'merge.' || fingerprint_sha256),
  CONSTRAINT pim_merge_previews_fields CHECK (jsonb_typeof(fields)='array' AND jsonb_array_length(fields) BETWEEN 1 AND 128)
);

ALTER TABLE connector_entity_mappings DROP CONSTRAINT connector_entity_mappings_type_chk;
ALTER TABLE connector_entity_mappings ADD CONSTRAINT connector_entity_mappings_type_chk CHECK (entity_type IN ('product','offer','order','brand','category','attribute'));

CREATE OR REPLACE FUNCTION connector_entity_mappings_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE present boolean; BEGIN
  IF TG_OP = ''INSERT'' AND NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new connector mapping must start at version 1''; END IF;
  IF TG_OP = ''UPDATE'' THEN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.entity_type IS DISTINCT FROM OLD.entity_type OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping identity is immutable''; END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping version transition is invalid''; END IF;
  END IF;
  IF NEW.entity_type=''product'' THEN SELECT EXISTS(SELECT 1 FROM products WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSIF NEW.entity_type=''offer'' THEN SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSIF NEW.entity_type=''order'' THEN SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSIF NEW.entity_type=''brand'' THEN SELECT EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSIF NEW.entity_type=''category'' THEN SELECT EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  ELSE SELECT EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
  END IF;
  IF NOT present THEN RAISE EXCEPTION USING ERRCODE=''23503'',MESSAGE=''connector mapping local entity does not exist''; END IF;
  RETURN NEW;
END';

CREATE FUNCTION pim_master_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP=''INSERT'' THEN IF NEW.status<>''draft'' OR NEW.version<>1 THEN RAISE EXCEPTION ''PIM master must start draft at version 1''; END IF; RETURN NEW; END IF;
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.code<>OLD.code OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION ''PIM master identity is immutable''; END IF;
  IF OLD.status=''archived'' THEN RAISE EXCEPTION ''archived PIM master is immutable''; END IF;
  IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''PIM master version transition invalid''; END IF;
  IF NEW.status<>OLD.status AND NOT ((OLD.status=''draft'' AND NEW.status IN (''active'',''archived'')) OR (OLD.status=''active'' AND NEW.status=''archived'')) THEN RAISE EXCEPTION ''PIM lifecycle transition invalid''; END IF;
  RETURN NEW;
END';
CREATE TRIGGER pim_brands_guard BEFORE INSERT OR UPDATE ON pim_brands FOR EACH ROW EXECUTE FUNCTION pim_master_guard();
CREATE TRIGGER pim_categories_guard BEFORE INSERT OR UPDATE ON pim_categories FOR EACH ROW EXECUTE FUNCTION pim_master_guard();
CREATE TRIGGER pim_attributes_guard BEFORE INSERT OR UPDATE ON pim_attributes FOR EACH ROW EXECUTE FUNCTION pim_master_guard();

CREATE FUNCTION pim_category_identity_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''UPDATE'' AND NEW.parent_id IS DISTINCT FROM OLD.parent_id THEN RAISE EXCEPTION ''category parent is immutable in v1''; END IF; RETURN NEW; END';
CREATE TRIGGER pim_categories_parent_immutable BEFORE UPDATE ON pim_categories FOR EACH ROW EXECUTE FUNCTION pim_category_identity_guard();
CREATE FUNCTION pim_attribute_identity_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''UPDATE'' AND (NEW.value_type<>OLD.value_type OR NEW.multi_value<>OLD.multi_value) THEN RAISE EXCEPTION ''attribute type is immutable''; END IF; RETURN NEW; END';
CREATE TRIGGER pim_attributes_type_immutable BEFORE UPDATE ON pim_attributes FOR EACH ROW EXECUTE FUNCTION pim_attribute_identity_guard();

CREATE FUNCTION pim_assignment_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
 IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION ''PIM assignment must start version 1''; END IF; RETURN NEW; END IF;
 IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION ''PIM assignment identity is immutable''; END IF;
 IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''PIM assignment version invalid''; END IF;
 IF OLD.active=false AND NEW.active=true THEN RAISE EXCEPTION ''retired PIM assignment cannot reactivate''; END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_product_brands_guard BEFORE INSERT OR UPDATE ON pim_product_brands FOR EACH ROW EXECUTE FUNCTION pim_assignment_guard();
CREATE TRIGGER pim_product_categories_guard BEFORE INSERT OR UPDATE ON pim_product_categories FOR EACH ROW EXECUTE FUNCTION pim_assignment_guard();

CREATE FUNCTION pim_product_brand_identity_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''UPDATE'' AND NEW.product_id<>OLD.product_id THEN RAISE EXCEPTION ''product brand identity is immutable''; END IF; RETURN NEW; END';
CREATE TRIGGER pim_product_brands_identity BEFORE UPDATE ON pim_product_brands FOR EACH ROW EXECUTE FUNCTION pim_product_brand_identity_guard();
CREATE FUNCTION pim_product_category_identity_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''UPDATE'' AND (NEW.product_id<>OLD.product_id OR NEW.category_id<>OLD.category_id) THEN RAISE EXCEPTION ''product category identity is immutable''; END IF; RETURN NEW; END';
CREATE TRIGGER pim_product_categories_identity BEFORE UPDATE ON pim_product_categories FOR EACH ROW EXECUTE FUNCTION pim_product_category_identity_guard();

CREATE FUNCTION pim_assignment_master_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE st text; BEGIN
 IF TG_TABLE_NAME=''pim_product_brands'' THEN SELECT status INTO st FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.brand_id;
 ELSE SELECT status INTO st FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.category_id; END IF;
 IF st IS NULL OR st=''archived'' THEN RAISE EXCEPTION ''PIM assignment master unavailable''; END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_product_brands_master_guard BEFORE INSERT OR UPDATE ON pim_product_brands FOR EACH ROW EXECUTE FUNCTION pim_assignment_master_guard();
CREATE TRIGGER pim_product_categories_master_guard BEFORE INSERT OR UPDATE ON pim_product_categories FOR EACH ROW EXECUTE FUNCTION pim_assignment_master_guard();

CREATE FUNCTION pim_attribute_value_validate() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE t text; multi boolean; st text; raw text; BEGIN
 SELECT value_type,multi_value,status INTO t,multi,st FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.attribute_id;
 IF t IS NULL OR st=''archived'' THEN RAISE EXCEPTION ''attribute definition unavailable''; END IF;
 IF NOT multi AND NEW.ordinal<>0 THEN RAISE EXCEPTION ''single-value attribute requires ordinal 0''; END IF;
 IF TG_OP=''INSERT'' AND NEW.version<>1 THEN RAISE EXCEPTION ''attribute value must start version 1''; END IF;
 IF TG_OP=''UPDATE'' THEN
   IF NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.product_id<>OLD.product_id OR NEW.attribute_id<>OLD.attribute_id OR NEW.ordinal<>OLD.ordinal OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION ''attribute value identity immutable''; END IF;
   IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''attribute value version invalid''; END IF;
   IF OLD.active=false AND NEW.active=true THEN RAISE EXCEPTION ''retired attribute value cannot reactivate''; END IF;
 END IF;
 raw:=NEW.value #>> ''{}'';
 IF t IN (''text'',''date'',''datetime'',''reference'') AND jsonb_typeof(NEW.value)<>''string'' THEN RAISE EXCEPTION ''attribute value type mismatch'';
 ELSIF t=''integer'' AND (jsonb_typeof(NEW.value)<>''number'' OR raw !~ ''^-?[0-9]+$'') THEN RAISE EXCEPTION ''attribute integer invalid'';
 ELSIF t=''decimal'' AND (jsonb_typeof(NEW.value)<>''string'' OR raw !~ ''^-?(0|[1-9][0-9]*)(\.[0-9]{1,9})?$'') THEN RAISE EXCEPTION ''attribute decimal invalid'';
 ELSIF t=''boolean'' AND jsonb_typeof(NEW.value)<>''boolean'' THEN RAISE EXCEPTION ''attribute boolean invalid'';
 END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_product_attribute_values_guard BEFORE INSERT OR UPDATE ON pim_product_attribute_values FOR EACH ROW EXECUTE FUNCTION pim_attribute_value_validate();

CREATE FUNCTION pim_authority_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
 IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION ''field authority must start version 1''; END IF; RETURN NEW; END IF;
 IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.entity_type<>OLD.entity_type OR NEW.field_path<>OLD.field_path OR NEW.source<>OLD.source OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION ''field authority identity immutable''; END IF;
 IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''field authority version invalid''; END IF;
 IF OLD.active=false AND NEW.active=true THEN RAISE EXCEPTION ''retired field authority cannot reactivate''; END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_field_authorities_guard BEFORE INSERT OR UPDATE ON pim_field_authorities FOR EACH ROW EXECUTE FUNCTION pim_authority_guard();

CREATE FUNCTION pim_duplicate_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
 IF TG_OP=''INSERT'' THEN IF NEW.state<>''open'' OR NEW.version<>1 THEN RAISE EXCEPTION ''duplicate candidate must start open at version 1''; END IF; RETURN NEW; END IF;
 IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.workspace_id<>OLD.workspace_id OR NEW.entity_type<>OLD.entity_type OR NEW.left_id<>OLD.left_id OR NEW.right_id<>OLD.right_id OR NEW.score_bps<>OLD.score_bps OR NEW.signals<>OLD.signals OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION ''duplicate evidence immutable''; END IF;
 IF OLD.state<>''open'' OR NEW.state NOT IN (''confirmed'',''not_duplicate'',''merged'') OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''duplicate review transition invalid''; END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_duplicate_candidates_guard BEFORE INSERT OR UPDATE ON pim_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION pim_duplicate_guard();

CREATE FUNCTION pim_duplicate_signals_validate() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE item jsonb; BEGIN
 FOR item IN SELECT value FROM jsonb_array_elements(NEW.signals) LOOP
   IF jsonb_typeof(item)<>''object'' OR NOT (item ? ''kind'') OR NOT (item ? ''explanation'') OR NOT (item ? ''weight_bps'') OR jsonb_typeof(item->''kind'')<>''string'' OR jsonb_typeof(item->''explanation'')<>''string'' OR jsonb_typeof(item->''weight_bps'')<>''number'' THEN RAISE EXCEPTION ''duplicate signal shape invalid''; END IF;
   IF (item->>''kind'') !~ ''^[a-z][a-z0-9_.-]{0,127}$'' OR char_length(item->>''explanation'') NOT BETWEEN 1 AND 300 OR (item->>''weight_bps'') !~ ''^[0-9]+$'' OR (item->>''weight_bps'')::integer NOT BETWEEN 0 AND 10000 THEN RAISE EXCEPTION ''duplicate signal value invalid''; END IF;
   IF (SELECT count(*) FROM jsonb_object_keys(item)) <> 3 THEN RAISE EXCEPTION ''duplicate signal contains unknown fields''; END IF;
 END LOOP;
 RETURN NEW;
END';
CREATE TRIGGER pim_duplicate_candidates_signals BEFORE INSERT OR UPDATE ON pim_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION pim_duplicate_signals_validate();

CREATE FUNCTION pim_duplicate_refs_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE left_present boolean; right_present boolean; BEGIN
 IF NEW.entity_type=''brand'' THEN SELECT EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.left_id),EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.right_id) INTO left_present,right_present;
 ELSIF NEW.entity_type=''category'' THEN SELECT EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.left_id),EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.right_id) INTO left_present,right_present;
 ELSE SELECT EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.left_id),EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.right_id) INTO left_present,right_present;
 END IF;
 IF NOT left_present OR NOT right_present THEN RAISE EXCEPTION ''duplicate candidate entities must exist in tenant''; END IF; RETURN NEW;
END';
CREATE TRIGGER pim_duplicate_candidates_refs BEFORE INSERT ON pim_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION pim_duplicate_refs_guard();
CREATE FUNCTION pim_merge_refs_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE target_present boolean; source_present boolean; BEGIN
 IF NEW.entity_type=''brand'' THEN SELECT EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.target_id),EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.source_id) INTO target_present,source_present;
 ELSIF NEW.entity_type=''category'' THEN SELECT EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.target_id),EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.source_id) INTO target_present,source_present;
 ELSE SELECT EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.target_id),EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.source_id) INTO target_present,source_present;
 END IF;
 IF NOT target_present OR NOT source_present THEN RAISE EXCEPTION ''merge preview entities must exist in tenant''; END IF; RETURN NEW;
END';
CREATE TRIGGER pim_merge_previews_refs BEFORE INSERT ON pim_merge_previews FOR EACH ROW EXECUTE FUNCTION pim_merge_refs_guard();

CREATE FUNCTION pim_merge_fields_validate() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE item jsonb; decision text; reason text; winner text; conflicts integer:=0; BEGIN
 FOR item IN SELECT value FROM jsonb_array_elements(NEW.fields) LOOP
   IF jsonb_typeof(item)<>''object'' OR (SELECT count(*) FROM jsonb_object_keys(item))<>6 OR NOT (item ?& ARRAY[''field_path'',''target_value'',''source_value'',''winner_source'',''reason'',''decision'']) THEN RAISE EXCEPTION ''merge field shape invalid''; END IF;
   IF jsonb_typeof(item->''field_path'')<>''string'' OR jsonb_typeof(item->''target_value'')<>''string'' OR jsonb_typeof(item->''source_value'')<>''string'' OR jsonb_typeof(item->''winner_source'')<>''string'' OR jsonb_typeof(item->''reason'')<>''string'' OR jsonb_typeof(item->''decision'')<>''string'' THEN RAISE EXCEPTION ''merge field types invalid''; END IF;
   IF (item->>''field_path'') !~ ''^[a-z][a-z0-9_.-]{0,127}$'' OR char_length(item->>''target_value'')>8192 OR char_length(item->>''source_value'')>8192 THEN RAISE EXCEPTION ''merge field bounds invalid''; END IF;
   decision:=item->>''decision''; reason:=item->>''reason''; winner:=item->>''winner_source'';
   IF decision=''keep_target'' AND (winner='''' OR reason NOT IN (''source_missing'',''target_authority'')) THEN RAISE EXCEPTION ''merge keep-target evidence invalid'';
   ELSIF decision=''take_source'' AND (winner='''' OR reason NOT IN (''target_missing'',''source_authority'')) THEN RAISE EXCEPTION ''merge take-source evidence invalid'';
   ELSIF decision=''equal'' AND (winner='''' OR reason<>''equal_values'') THEN RAISE EXCEPTION ''merge equal evidence invalid'';
   ELSIF decision=''conflict'' AND (winner<>'''' OR reason<>''equal_authority'') THEN RAISE EXCEPTION ''merge conflict evidence invalid''; conflicts:=conflicts+1;
   ELSIF decision NOT IN (''keep_target'',''take_source'',''equal'',''conflict'') THEN RAISE EXCEPTION ''merge decision invalid''; END IF;
 END LOOP;
 IF NEW.has_conflicts <> (conflicts>0) THEN RAISE EXCEPTION ''merge conflict flag mismatch''; END IF;
 RETURN NEW;
END';
CREATE TRIGGER pim_merge_previews_fields_guard BEFORE INSERT ON pim_merge_previews FOR EACH ROW EXECUTE FUNCTION pim_merge_fields_validate();

CREATE FUNCTION pim_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''PIM review evidence is append-only''; END';
CREATE TRIGGER pim_merge_previews_immutable BEFORE UPDATE OR DELETE ON pim_merge_previews FOR EACH ROW EXECUTE FUNCTION pim_append_only();

ALTER TABLE pim_brands ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_brands FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_categories ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_categories FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_attributes ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_attributes FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_product_brands ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_product_brands FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_product_categories ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_product_categories FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_product_attribute_values ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_product_attribute_values FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_field_authorities ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_field_authorities FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_duplicate_candidates ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_duplicate_candidates FORCE ROW LEVEL SECURITY;
ALTER TABLE pim_merge_previews ENABLE ROW LEVEL SECURITY; ALTER TABLE pim_merge_previews FORCE ROW LEVEL SECURITY;

CREATE POLICY pim_brands_select ON pim_brands FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_brands_insert ON pim_brands FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_brands_update ON pim_brands FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_categories_select ON pim_categories FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_categories_insert ON pim_categories FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_categories_update ON pim_categories FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_attributes_select ON pim_attributes FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_attributes_insert ON pim_attributes FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_attributes_update ON pim_attributes FOR UPDATE USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_product_brands_all ON pim_product_brands FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_product_categories_all ON pim_product_categories FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_product_attribute_values_all ON pim_product_attribute_values FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_field_authorities_all ON pim_field_authorities FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_duplicate_candidates_all ON pim_duplicate_candidates FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_merge_previews_select ON pim_merge_previews FOR SELECT USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY pim_merge_previews_insert ON pim_merge_previews FOR INSERT WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION pim_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''PIM master/history cannot be hard-deleted''; END';
CREATE TRIGGER pim_brands_no_delete BEFORE DELETE ON pim_brands FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_categories_no_delete BEFORE DELETE ON pim_categories FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_attributes_no_delete BEFORE DELETE ON pim_attributes FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_product_brands_no_delete BEFORE DELETE ON pim_product_brands FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_product_categories_no_delete BEFORE DELETE ON pim_product_categories FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_product_attribute_values_no_delete BEFORE DELETE ON pim_product_attribute_values FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_field_authorities_no_delete BEFORE DELETE ON pim_field_authorities FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE TRIGGER pim_duplicate_candidates_no_delete BEFORE DELETE ON pim_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION pim_no_delete();
CREATE FUNCTION pim_no_clear() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''PIM master/history cannot be cleared''; END';
CREATE TRIGGER pim_brands_no_clear BEFORE TRUNCATE ON pim_brands EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_categories_no_clear BEFORE TRUNCATE ON pim_categories EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_attributes_no_clear BEFORE TRUNCATE ON pim_attributes EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_product_brands_no_clear BEFORE TRUNCATE ON pim_product_brands EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_product_categories_no_clear BEFORE TRUNCATE ON pim_product_categories EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_product_attribute_values_no_clear BEFORE TRUNCATE ON pim_product_attribute_values EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_field_authorities_no_clear BEFORE TRUNCATE ON pim_field_authorities EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_duplicate_candidates_no_clear BEFORE TRUNCATE ON pim_duplicate_candidates EXECUTE FUNCTION pim_no_clear();
CREATE TRIGGER pim_merge_previews_no_clear BEFORE TRUNCATE ON pim_merge_previews EXECUTE FUNCTION pim_no_clear();

CREATE INDEX pim_categories_parent_idx ON pim_categories(organization_id,workspace_id,parent_id,status,id);
CREATE INDEX pim_product_attributes_idx ON pim_product_attribute_values(organization_id,workspace_id,product_id,active,attribute_id,ordinal);
CREATE INDEX pim_duplicate_open_idx ON pim_duplicate_candidates(organization_id,workspace_id,entity_type,state,score_bps DESC,id) WHERE state='open';
CREATE INDEX pim_authorities_lookup_idx ON pim_field_authorities(organization_id,workspace_id,entity_type,field_path,active,priority DESC);

COMMENT ON TABLE pim_brands IS 'Canonical provider-neutral Brand masters.';
COMMENT ON TABLE pim_categories IS 'Canonical provider-neutral Category hierarchy; external taxonomies map through connector_entity_mappings.';
COMMENT ON TABLE pim_attributes IS 'Canonical typed Attribute definitions; provider fields are projections/mappings only.';
COMMENT ON TABLE pim_merge_previews IS 'Immutable review artifact; storing a preview never applies a merge.';

-- SOURCE 000016_legal_party_counterparty.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


CREATE FUNCTION ru_inn_legal_valid(v text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS 'DECLARE x integer; BEGIN IF v !~ ''^[0-9]{10}$'' THEN RETURN false; END IF; x:=((substring(v,1,1)::int*2 + substring(v,2,1)::int*4 + substring(v,3,1)::int*10 + substring(v,4,1)::int*3 + substring(v,5,1)::int*5 + substring(v,6,1)::int*9 + substring(v,7,1)::int*4 + substring(v,8,1)::int*6 + substring(v,9,1)::int*8) % 11) % 10; RETURN x=substring(v,10,1)::int; END';
CREATE FUNCTION ru_inn_individual_valid(v text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS 'DECLARE x integer; y integer; BEGIN IF v !~ ''^[0-9]{12}$'' THEN RETURN false; END IF; x:=((substring(v,1,1)::int*7 + substring(v,2,1)::int*2 + substring(v,3,1)::int*4 + substring(v,4,1)::int*10 + substring(v,5,1)::int*3 + substring(v,6,1)::int*5 + substring(v,7,1)::int*9 + substring(v,8,1)::int*4 + substring(v,9,1)::int*6 + substring(v,10,1)::int*8) % 11) % 10; y:=((substring(v,1,1)::int*3 + substring(v,2,1)::int*7 + substring(v,3,1)::int*2 + substring(v,4,1)::int*4 + substring(v,5,1)::int*10 + substring(v,6,1)::int*3 + substring(v,7,1)::int*5 + substring(v,8,1)::int*9 + substring(v,9,1)::int*4 + substring(v,10,1)::int*6 + substring(v,11,1)::int*8) % 11) % 10; RETURN x=substring(v,11,1)::int AND y=substring(v,12,1)::int; END';
CREATE FUNCTION ru_kpp_valid(v text) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT v ~ ''^[0-9]{4}[0-9A-Z]{2}[0-9]{3}$''';
CREATE FUNCTION ru_ogrn_valid(v text) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT v ~ ''^[0-9]{13}$'' AND (((substring(v,1,12)::bigint % 11) % 10)::int = substring(v,13,1)::int)';
CREATE FUNCTION ru_ogrnip_valid(v text) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT v ~ ''^[0-9]{15}$'' AND (((substring(v,1,14)::bigint % 13) % 10)::int = substring(v,15,1)::int)';

CREATE TABLE legal_entities (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL,
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
  legal_name text NOT NULL CHECK (legal_name=btrim(legal_name) AND char_length(legal_name) BETWEEN 1 AND 500 AND legal_name !~ '[[:cntrl:]]'),
  short_name text NOT NULL DEFAULT '' CHECK (short_name=btrim(short_name) AND char_length(short_name)<=300 AND short_name !~ '[[:cntrl:]]'),
  country_code text NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
  inn text NOT NULL DEFAULT '' CHECK (inn ~ '^[0-9A-Za-z.-]{0,12}$'),
  kpp text NOT NULL DEFAULT '' CHECK (kpp ~ '^[0-9A-Za-z.-]{0,16}$'),
  ogrn text NOT NULL DEFAULT '' CHECK (ogrn ~ '^[0-9A-Za-z.-]{0,32}$'),
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
  version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT legal_entities_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  UNIQUE(organization_id,workspace_id,code),
  CONSTRAINT legal_entities_ru_ids CHECK(country_code<>'RU' OR (ru_inn_legal_valid(inn) AND ru_kpp_valid(kpp) AND ru_ogrn_valid(ogrn)))
);
CREATE UNIQUE INDEX legal_entities_inn_unique ON legal_entities(organization_id,workspace_id,inn) WHERE inn<>'' AND status<>'archived';
CREATE UNIQUE INDEX legal_entities_ogrn_unique ON legal_entities(organization_id,workspace_id,ogrn) WHERE ogrn<>'' AND status<>'archived';

CREATE TABLE individual_entrepreneurs (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL,
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'), full_name text NOT NULL CHECK(full_name=btrim(full_name) AND char_length(full_name) BETWEEN 1 AND 500 AND full_name !~ '[[:cntrl:]]'),
  country_code text NOT NULL CHECK(country_code ~ '^[A-Z]{2}$'), inn text NOT NULL DEFAULT '' CHECK(inn ~ '^[0-9A-Za-z.-]{0,12}$'), ogrnip text NOT NULL DEFAULT '' CHECK(ogrnip ~ '^[0-9A-Za-z.-]{0,32}$'),
  status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','archived')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT individual_entrepreneurs_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id), UNIQUE(organization_id,workspace_id,code), CONSTRAINT individual_entrepreneurs_ru_ids CHECK(country_code<>'RU' OR (ru_inn_individual_valid(inn) AND ru_ogrnip_valid(ogrnip)))
);
CREATE UNIQUE INDEX individual_entrepreneurs_inn_unique ON individual_entrepreneurs(organization_id,workspace_id,inn) WHERE inn<>'' AND status<>'archived';
CREATE UNIQUE INDEX individual_entrepreneurs_ogrnip_unique ON individual_entrepreneurs(organization_id,workspace_id,ogrnip) WHERE ogrnip<>'' AND status<>'archived';

CREATE TABLE legal_branches (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, legal_entity_id text NOT NULL,
  code text NOT NULL CHECK(code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'), name text NOT NULL CHECK(name=btrim(name) AND char_length(name) BETWEEN 1 AND 500 AND name !~ '[[:cntrl:]]'), country_code text NOT NULL CHECK(country_code ~ '^[A-Z]{2}$'), kpp text NOT NULL DEFAULT '' CHECK(kpp ~ '^[0-9A-Za-z.-]{0,16}$'),
  status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','archived')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT legal_branches_parent_fk FOREIGN KEY(organization_id,workspace_id,legal_entity_id) REFERENCES legal_entities(organization_id,workspace_id,id), UNIQUE(organization_id,workspace_id,code), CONSTRAINT legal_branches_ru_kpp CHECK(country_code<>'RU' OR ru_kpp_valid(kpp))
);
CREATE UNIQUE INDEX legal_branches_kpp_unique ON legal_branches(organization_id,workspace_id,legal_entity_id,kpp) WHERE kpp<>'' AND status<>'archived';

CREATE FUNCTION legal_party_ref_exists(p_org text,p_ws text,p_type text,p_id text) RETURNS boolean LANGUAGE plpgsql STABLE AS 'DECLARE ok boolean; BEGIN
 IF p_type=''legal_entity'' THEN SELECT EXISTS(SELECT 1 FROM legal_entities WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok;
 ELSIF p_type=''individual_entrepreneur'' THEN SELECT EXISTS(SELECT 1 FROM individual_entrepreneurs WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok;
 ELSIF p_type=''branch'' THEN SELECT EXISTS(SELECT 1 FROM legal_branches WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok;
 ELSE ok:=false; END IF; RETURN ok; END';

CREATE TABLE counterparties (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, code text NOT NULL CHECK(code ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'),
 party_type text NOT NULL CHECK(party_type IN ('legal_entity','individual_entrepreneur','branch')), party_id text NOT NULL,
 role text NOT NULL CHECK(role IN ('customer','supplier','partner','other')), status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','archived')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT counterparties_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id), UNIQUE(organization_id,workspace_id,code), UNIQUE(organization_id,workspace_id,party_type,party_id)
);
CREATE FUNCTION counterparty_party_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.party_id) THEN RAISE EXCEPTION ''counterparty party reference missing''; END IF; RETURN NEW; END';
CREATE TRIGGER counterparties_party_guard BEFORE INSERT OR UPDATE ON counterparties FOR EACH ROW EXECUTE FUNCTION counterparty_party_guard();

CREATE TABLE legal_addresses (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, party_type text NOT NULL CHECK(party_type IN ('legal_entity','individual_entrepreneur','branch')), party_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('legal','actual','postal')), country_code text NOT NULL CHECK(country_code ~ '^[A-Z]{2}$'), postal_code text NOT NULL DEFAULT '', region text NOT NULL DEFAULT '', city text NOT NULL DEFAULT '', line1 text NOT NULL, line2 text NOT NULL DEFAULT '', is_primary boolean NOT NULL DEFAULT false, active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT legal_addresses_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id)
);
CREATE FUNCTION legal_address_party_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.party_id) THEN RAISE EXCEPTION ''address party reference missing''; END IF; RETURN NEW; END';
CREATE TRIGGER legal_addresses_party_guard BEFORE INSERT OR UPDATE ON legal_addresses FOR EACH ROW EXECUTE FUNCTION legal_address_party_guard();
CREATE UNIQUE INDEX legal_addresses_primary_idx ON legal_addresses(organization_id,workspace_id,party_type,party_id,kind) WHERE active AND is_primary;

CREATE TABLE counterparty_bank_accounts (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, counterparty_id text NOT NULL,
 currency text NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), account_number text NOT NULL CHECK(char_length(account_number) BETWEEN 6 AND 64), bank_name text NOT NULL, bank_country_code text NOT NULL CHECK(bank_country_code ~ '^[A-Z]{2}$'), bic text NOT NULL DEFAULT '', correspondent_account text NOT NULL DEFAULT '', is_primary boolean NOT NULL DEFAULT false,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','archived')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT counterparty_bank_accounts_counterparty_fk FOREIGN KEY(organization_id,workspace_id,counterparty_id) REFERENCES counterparties(organization_id,workspace_id,id), UNIQUE(organization_id,workspace_id,counterparty_id,currency,account_number)
);
CREATE UNIQUE INDEX counterparty_bank_accounts_primary_idx ON counterparty_bank_accounts(organization_id,workspace_id,counterparty_id,currency) WHERE status='active' AND is_primary;

CREATE TABLE counterparty_contracts (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, counterparty_id text NOT NULL, number text NOT NULL, contract_type text NOT NULL CHECK(contract_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$'), signed_on timestamptz, valid_from timestamptz NOT NULL, valid_until timestamptz,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','terminated','expired')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT counterparty_contracts_counterparty_fk FOREIGN KEY(organization_id,workspace_id,counterparty_id) REFERENCES counterparties(organization_id,workspace_id,id), CONSTRAINT counterparty_contract_dates CHECK(valid_until IS NULL OR valid_until>=valid_from), UNIQUE(organization_id,workspace_id,counterparty_id,number)
);

CREATE TABLE counterparty_authorities (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, counterparty_id text NOT NULL, authority_type text NOT NULL CHECK(authority_type IN ('charter','power_of_attorney','mchd','order','other')), reference_number text NOT NULL, issuer text NOT NULL DEFAULT '', issued_at timestamptz NOT NULL, expires_at timestamptz,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','archived')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT counterparty_authorities_counterparty_fk FOREIGN KEY(organization_id,workspace_id,counterparty_id) REFERENCES counterparties(organization_id,workspace_id,id), CONSTRAINT counterparty_authority_dates CHECK(expires_at IS NULL OR expires_at>=issued_at), UNIQUE(organization_id,workspace_id,counterparty_id,authority_type,reference_number)
);

CREATE TABLE legal_party_duplicate_candidates (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, party_type text NOT NULL CHECK(party_type IN ('legal_entity','individual_entrepreneur','branch')), left_id text NOT NULL, right_id text NOT NULL, score_bps integer NOT NULL CHECK(score_bps BETWEEN 0 AND 10000), signals jsonb NOT NULL CHECK(jsonb_typeof(signals)='array' AND jsonb_array_length(signals) BETWEEN 1 AND 16), state text NOT NULL DEFAULT 'open' CHECK(state IN ('open','confirmed','not_duplicate','merged')), version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT legal_party_duplicate_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id), CONSTRAINT legal_party_duplicate_order CHECK(left_id<right_id), UNIQUE(organization_id,workspace_id,party_type,left_id,right_id)
);
CREATE FUNCTION legal_party_duplicate_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.left_id) OR NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.right_id) THEN RAISE EXCEPTION ''duplicate candidate references missing party''; END IF; RETURN NEW; END';
CREATE TRIGGER legal_party_duplicate_guard BEFORE INSERT OR UPDATE ON legal_party_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION legal_party_duplicate_guard();

CREATE TABLE legal_party_merge_previews (
 id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, party_type text NOT NULL CHECK(party_type IN ('legal_entity','individual_entrepreneur','branch')), target_id text NOT NULL, source_id text NOT NULL, target_version bigint NOT NULL CHECK(target_version>=1), source_version bigint NOT NULL CHECK(source_version>=1), fields jsonb NOT NULL CHECK(jsonb_typeof(fields)='array' AND jsonb_array_length(fields) BETWEEN 1 AND 64), has_conflicts boolean NOT NULL, fingerprint_sha256 text NOT NULL CHECK(fingerprint_sha256 ~ '^[0-9a-f]{64}$'), created_at timestamptz NOT NULL,
 PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT legal_party_merge_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id), CONSTRAINT legal_party_merge_pair CHECK(target_id<>source_id), CONSTRAINT legal_party_merge_id CHECK(id='party-merge.'||fingerprint_sha256)
);
CREATE FUNCTION legal_party_merge_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.target_id) OR NOT legal_party_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.party_type,NEW.source_id) THEN RAISE EXCEPTION ''merge preview references missing party''; END IF; RETURN NEW; END';
CREATE TRIGGER legal_party_merge_guard BEFORE INSERT ON legal_party_merge_previews FOR EACH ROW EXECUTE FUNCTION legal_party_merge_guard();

-- Extend generic external identity mapping additively for enterprise party masters.
ALTER TABLE connector_entity_mappings DROP CONSTRAINT connector_entity_mappings_type_chk;
ALTER TABLE connector_entity_mappings ADD CONSTRAINT connector_entity_mappings_type_chk CHECK(entity_type IN ('product','offer','order','brand','category','attribute','legal_entity','individual_entrepreneur','branch','counterparty','bank_account','contract','authority_reference'));
CREATE OR REPLACE FUNCTION connector_entity_mappings_guard() RETURNS trigger LANGUAGE plpgsql AS 'DECLARE present boolean; BEGIN
 IF TG_OP=''INSERT'' AND NEW.version<>1 THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''new connector mapping must start at version 1''; END IF;
 IF TG_OP=''UPDATE'' THEN IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.connector_account_id IS DISTINCT FROM OLD.connector_account_id OR NEW.entity_type IS DISTINCT FROM OLD.entity_type OR NEW.local_entity_id IS DISTINCT FROM OLD.local_entity_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping identity is immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION USING ERRCODE=''55000'',MESSAGE=''connector mapping version must increase by one''; END IF; END IF;
 present:=false;
 IF NEW.entity_type=''product'' THEN SELECT EXISTS(SELECT 1 FROM products WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''offer'' THEN SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''order'' THEN SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''brand'' THEN SELECT EXISTS(SELECT 1 FROM pim_brands WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''category'' THEN SELECT EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''attribute'' THEN SELECT EXISTS(SELECT 1 FROM pim_attributes WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''legal_entity'' THEN SELECT EXISTS(SELECT 1 FROM legal_entities WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''individual_entrepreneur'' THEN SELECT EXISTS(SELECT 1 FROM individual_entrepreneurs WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''branch'' THEN SELECT EXISTS(SELECT 1 FROM legal_branches WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''counterparty'' THEN SELECT EXISTS(SELECT 1 FROM counterparties WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''bank_account'' THEN SELECT EXISTS(SELECT 1 FROM counterparty_bank_accounts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''contract'' THEN SELECT EXISTS(SELECT 1 FROM counterparty_contracts WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present;
 ELSIF NEW.entity_type=''authority_reference'' THEN SELECT EXISTS(SELECT 1 FROM counterparty_authorities WHERE organization_id=NEW.organization_id AND workspace_id=NEW.workspace_id AND id=NEW.local_entity_id) INTO present; END IF;
 IF NOT present THEN RAISE EXCEPTION USING ERRCODE=''23503'',MESSAGE=''connector mapping local entity does not exist in tenant''; END IF; RETURN NEW; END';

-- Direct SQL lifecycle/version/identity guards.
CREATE FUNCTION legal_party_master_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''INSERT'' THEN IF NEW.version<>1 OR NEW.status<>''draft'' THEN RAISE EXCEPTION ''legal-party master must start draft/version 1''; END IF; RETURN NEW; END IF; IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id OR NEW.code IS DISTINCT FROM OLD.code OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''legal-party identity immutable''; END IF; IF OLD.status=''archived'' THEN RAISE EXCEPTION ''archived legal-party master immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''legal-party version invalid''; END IF; IF NOT ((OLD.status=''draft'' AND NEW.status IN (''draft'',''active'',''archived'')) OR (OLD.status=''active'' AND NEW.status IN (''active'',''archived''))) THEN RAISE EXCEPTION ''legal-party lifecycle invalid''; END IF; RETURN NEW; END';
CREATE TRIGGER legal_entities_guard BEFORE INSERT OR UPDATE ON legal_entities FOR EACH ROW EXECUTE FUNCTION legal_party_master_guard();
CREATE TRIGGER individual_entrepreneurs_guard BEFORE INSERT OR UPDATE ON individual_entrepreneurs FOR EACH ROW EXECUTE FUNCTION legal_party_master_guard();
CREATE TRIGGER legal_branches_guard BEFORE INSERT OR UPDATE ON legal_branches FOR EACH ROW EXECUTE FUNCTION legal_party_master_guard();
CREATE TRIGGER counterparties_guard BEFORE INSERT OR UPDATE ON counterparties FOR EACH ROW EXECUTE FUNCTION legal_party_master_guard();

CREATE FUNCTION legal_party_subrecord_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION ''legal-party subrecord must start at version 1''; END IF; RETURN NEW; END IF; IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''legal-party subrecord identity immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''legal-party subrecord version invalid''; END IF; RETURN NEW; END';
CREATE TRIGGER legal_addresses_guard BEFORE INSERT OR UPDATE ON legal_addresses FOR EACH ROW EXECUTE FUNCTION legal_party_subrecord_guard();

CREATE FUNCTION legal_party_status_subrecord_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''INSERT'' THEN IF NEW.version<>1 OR NEW.status<>''draft'' THEN RAISE EXCEPTION ''legal-party status record must start draft/version 1''; END IF; RETURN NEW; END IF; IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''legal-party status record identity immutable''; END IF; IF OLD.status=''archived'' THEN RAISE EXCEPTION ''archived legal-party status record immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''legal-party status record version invalid''; END IF; IF NOT ((OLD.status=''draft'' AND NEW.status IN (''draft'',''active'',''archived'')) OR (OLD.status=''active'' AND NEW.status IN (''active'',''archived''))) THEN RAISE EXCEPTION ''legal-party status lifecycle invalid''; END IF; RETURN NEW; END';
CREATE TRIGGER counterparty_bank_accounts_guard BEFORE INSERT OR UPDATE ON counterparty_bank_accounts FOR EACH ROW EXECUTE FUNCTION legal_party_status_subrecord_guard();
CREATE TRIGGER counterparty_authorities_guard BEFORE INSERT OR UPDATE ON counterparty_authorities FOR EACH ROW EXECUTE FUNCTION legal_party_status_subrecord_guard();

CREATE FUNCTION legal_party_contract_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF TG_OP=''INSERT'' THEN IF NEW.version<>1 OR NEW.status<>''draft'' THEN RAISE EXCEPTION ''contract must start draft/version 1''; END IF; RETURN NEW; END IF; IF NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.id IS DISTINCT FROM OLD.id OR NEW.counterparty_id IS DISTINCT FROM OLD.counterparty_id OR NEW.number IS DISTINCT FROM OLD.number OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''contract identity immutable''; END IF; IF OLD.status IN (''terminated'',''expired'') THEN RAISE EXCEPTION ''closed contract immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''contract version invalid''; END IF; RETURN NEW; END';
CREATE TRIGGER counterparty_contracts_guard BEFORE INSERT OR UPDATE ON counterparty_contracts FOR EACH ROW EXECUTE FUNCTION legal_party_contract_guard();

-- RLS policies.
ALTER TABLE legal_entities ENABLE ROW LEVEL SECURITY; ALTER TABLE legal_entities FORCE ROW LEVEL SECURITY;
ALTER TABLE individual_entrepreneurs ENABLE ROW LEVEL SECURITY; ALTER TABLE individual_entrepreneurs FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_branches ENABLE ROW LEVEL SECURITY; ALTER TABLE legal_branches FORCE ROW LEVEL SECURITY;
ALTER TABLE counterparties ENABLE ROW LEVEL SECURITY; ALTER TABLE counterparties FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_addresses ENABLE ROW LEVEL SECURITY; ALTER TABLE legal_addresses FORCE ROW LEVEL SECURITY;
ALTER TABLE counterparty_bank_accounts ENABLE ROW LEVEL SECURITY; ALTER TABLE counterparty_bank_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE counterparty_contracts ENABLE ROW LEVEL SECURITY; ALTER TABLE counterparty_contracts FORCE ROW LEVEL SECURITY;
ALTER TABLE counterparty_authorities ENABLE ROW LEVEL SECURITY; ALTER TABLE counterparty_authorities FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_party_duplicate_candidates ENABLE ROW LEVEL SECURITY; ALTER TABLE legal_party_duplicate_candidates FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_party_merge_previews ENABLE ROW LEVEL SECURITY; ALTER TABLE legal_party_merge_previews FORCE ROW LEVEL SECURITY;
CREATE POLICY legal_entities_all ON legal_entities FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY individual_entrepreneurs_all ON individual_entrepreneurs FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY legal_branches_all ON legal_branches FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY counterparties_all ON counterparties FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY legal_addresses_all ON legal_addresses FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY counterparty_bank_accounts_all ON counterparty_bank_accounts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY counterparty_contracts_all ON counterparty_contracts FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY counterparty_authorities_all ON counterparty_authorities FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY legal_party_duplicate_all ON legal_party_duplicate_candidates FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY legal_party_merge_select ON legal_party_merge_previews FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY legal_party_merge_insert ON legal_party_merge_previews FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION legal_party_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''legal-party master/history cannot be hard-deleted''; END';
CREATE FUNCTION legal_party_no_clear() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''legal-party master/history cannot be cleared''; END';
CREATE TRIGGER legal_entities_no_delete BEFORE DELETE ON legal_entities FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER legal_entities_no_clear BEFORE TRUNCATE ON legal_entities EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER individual_entrepreneurs_no_delete BEFORE DELETE ON individual_entrepreneurs FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER individual_entrepreneurs_no_clear BEFORE TRUNCATE ON individual_entrepreneurs EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER legal_branches_no_delete BEFORE DELETE ON legal_branches FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER legal_branches_no_clear BEFORE TRUNCATE ON legal_branches EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER counterparties_no_delete BEFORE DELETE ON counterparties FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER counterparties_no_clear BEFORE TRUNCATE ON counterparties EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER legal_addresses_no_delete BEFORE DELETE ON legal_addresses FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER legal_addresses_no_clear BEFORE TRUNCATE ON legal_addresses EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER counterparty_bank_accounts_no_delete BEFORE DELETE ON counterparty_bank_accounts FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER counterparty_bank_accounts_no_clear BEFORE TRUNCATE ON counterparty_bank_accounts EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER counterparty_contracts_no_delete BEFORE DELETE ON counterparty_contracts FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER counterparty_contracts_no_clear BEFORE TRUNCATE ON counterparty_contracts EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER counterparty_authorities_no_delete BEFORE DELETE ON counterparty_authorities FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER counterparty_authorities_no_clear BEFORE TRUNCATE ON counterparty_authorities EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER legal_party_duplicate_no_delete BEFORE DELETE ON legal_party_duplicate_candidates FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER legal_party_duplicate_no_clear BEFORE TRUNCATE ON legal_party_duplicate_candidates EXECUTE FUNCTION legal_party_no_clear();
CREATE TRIGGER legal_party_merge_immutable BEFORE UPDATE OR DELETE ON legal_party_merge_previews FOR EACH ROW EXECUTE FUNCTION legal_party_no_delete(); CREATE TRIGGER legal_party_merge_no_clear BEFORE TRUNCATE ON legal_party_merge_previews EXECUTE FUNCTION legal_party_no_clear();

CREATE INDEX legal_entities_search_idx ON legal_entities(organization_id,workspace_id,lower(legal_name),status,id);
CREATE INDEX individual_entrepreneurs_search_idx ON individual_entrepreneurs(organization_id,workspace_id,lower(full_name),status,id);
CREATE INDEX counterparties_party_idx ON counterparties(organization_id,workspace_id,party_type,party_id,status,id);
CREATE INDEX counterparty_contracts_lookup_idx ON counterparty_contracts(organization_id,workspace_id,counterparty_id,status,valid_from,id);
CREATE INDEX counterparty_authorities_lookup_idx ON counterparty_authorities(organization_id,workspace_id,counterparty_id,status,expires_at,id);
CREATE INDEX legal_party_duplicate_open_idx ON legal_party_duplicate_candidates(organization_id,workspace_id,party_type,state,score_bps DESC,id) WHERE state='open';

COMMENT ON TABLE legal_entities IS 'Canonical provider-neutral legal entity master; Russian identifiers validated in typed Core adapters.';
COMMENT ON TABLE counterparties IS 'Tenant counterparty role referencing a canonical legal-party master.';
COMMENT ON TABLE legal_party_merge_previews IS 'Immutable non-executing merge review evidence; destructive merge requires a later approved workflow.';

-- SOURCE 000017_product_compliance.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE FUNCTION compliance_gtin_valid(v text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS 'DECLARE s integer:=0; i integer; w integer; BEGIN IF length(v) NOT IN (8,12,13,14) OR v !~ ''^[0-9]+$'' THEN RETURN false; END IF; FOR i IN REVERSE length(v)-1..1 LOOP IF ((length(v)-i) % 2)=1 THEN w:=3; ELSE w:=1; END IF; s:=s+substring(v,i,1)::int*w; END LOOP; RETURN ((10-(s%10))%10)=substring(v,length(v),1)::int; END';
CREATE FUNCTION compliance_holder_ref_exists(p_org text,p_ws text,p_type text,p_id text) RETURNS boolean LANGUAGE plpgsql STABLE AS 'DECLARE ok boolean:=false; BEGIN IF p_type='''' OR p_id='''' THEN RETURN p_type='''' AND p_id=''''; ELSIF p_type=''legal_entity'' OR p_type=''individual_entrepreneur'' OR p_type=''branch'' THEN RETURN legal_party_ref_exists(p_org,p_ws,p_type,p_id); ELSIF p_type=''counterparty'' THEN SELECT EXISTS(SELECT 1 FROM counterparties WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok; END IF; RETURN ok; END';
CREATE FUNCTION compliance_subject_ref_exists(p_org text,p_ws text,p_type text,p_id text) RETURNS boolean LANGUAGE plpgsql STABLE AS 'DECLARE ok boolean:=false; BEGIN IF p_type=''product'' THEN SELECT EXISTS(SELECT 1 FROM products WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok; ELSIF p_type=''offer'' THEN SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok; ELSIF p_type=''category'' THEN SELECT EXISTS(SELECT 1 FROM pim_categories WHERE organization_id=p_org AND workspace_id=p_ws AND id=p_id) INTO ok; ELSIF p_type=''gtin'' THEN ok:=compliance_gtin_valid(p_id); ELSIF p_type=''sku'' THEN SELECT EXISTS(SELECT 1 FROM offers WHERE organization_id=p_org AND workspace_id=p_ws AND sku=p_id) INTO ok; END IF; RETURN ok; END';
CREATE FUNCTION compliance_requirements_valid(v jsonb) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS 'DECLARE x jsonb; BEGIN IF jsonb_typeof(v)<>''array'' OR jsonb_array_length(v)<1 OR jsonb_array_length(v)>32 THEN RETURN false; END IF; FOR x IN SELECT value FROM jsonb_array_elements(v) LOOP IF jsonb_typeof(x)<>''object'' OR NOT (x ? ''document_type'') OR NOT (x ? ''failure_outcome'') OR NOT (x ? ''verification_required'') OR NOT (x ? ''min_validity_hours'') THEN RETURN false; END IF; IF x->>''document_type'' NOT IN (''declaration'',''certificate'',''eac_evidence'',''state_registration'',''veterinary'',''sanitary'',''refusal_letter'',''information_letter'',''other'') OR x->>''failure_outcome'' NOT IN (''warn'',''approval_required'',''block'') OR jsonb_typeof(x->''verification_required'')<>''boolean'' OR jsonb_typeof(x->''min_validity_hours'')<>''number'' OR (x->>''min_validity_hours'')::integer<0 OR (x->>''min_validity_hours'')::integer>87600 THEN RETURN false; END IF; END LOOP; RETURN true; EXCEPTION WHEN others THEN RETURN false; END';

CREATE TABLE compliance_documents (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL,
  document_type text NOT NULL CHECK(document_type IN ('declaration','certificate','eac_evidence','state_registration','veterinary','sanitary','refusal_letter','information_letter','other')),
  number text NOT NULL CHECK(char_length(number) BETWEEN 1 AND 256), jurisdiction text NOT NULL CHECK(jurisdiction ~ '^[A-Z]{2}$'),
  issuer text NOT NULL CHECK(issuer=btrim(issuer) AND char_length(issuer) BETWEEN 1 AND 300), registry_source text NOT NULL CHECK(registry_source ~ '^[a-z][a-z0-9._:/-]{0,127}$'), registry_reference text NOT NULL DEFAULT '' CHECK(char_length(registry_reference)<=256),
  status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','valid','suspended','revoked','expired','verification_failed')),
  issued_at timestamptz NOT NULL, expires_at timestamptz,
  holder_party_type text NOT NULL DEFAULT '' CHECK(holder_party_type IN ('','legal_entity','individual_entrepreneur','branch','counterparty')), holder_party_id text NOT NULL DEFAULT '',
  evidence_object_id text NOT NULL DEFAULT '' CHECK(char_length(evidence_object_id)<=128), verification_source text NOT NULL DEFAULT '' CHECK(verification_source='' OR verification_source ~ '^[a-z][a-z0-9._:/-]{0,127}$'), verified_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT compliance_documents_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id),
  CONSTRAINT compliance_documents_dates CHECK(expires_at IS NULL OR expires_at>=issued_at), CONSTRAINT compliance_documents_verification CHECK((verification_source='' AND verified_at IS NULL) OR (verification_source<>'' AND verified_at IS NOT NULL))
);
CREATE UNIQUE INDEX compliance_document_number_unique ON compliance_documents(organization_id,workspace_id,jurisdiction,document_type,number) WHERE status<>'revoked';
CREATE INDEX compliance_document_expiry_idx ON compliance_documents(organization_id,workspace_id,status,expires_at,id) WHERE expires_at IS NOT NULL;

CREATE FUNCTION compliance_document_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT compliance_holder_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.holder_party_type,NEW.holder_party_id) THEN RAISE EXCEPTION ''compliance holder missing''; END IF; IF TG_OP=''INSERT'' THEN IF NEW.version<>1 OR NEW.status<>''draft'' THEN RAISE EXCEPTION ''compliance document must start draft/v1''; END IF; RETURN NEW; END IF; IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.document_type IS DISTINCT FROM OLD.document_type OR NEW.number IS DISTINCT FROM OLD.number OR NEW.jurisdiction IS DISTINCT FROM OLD.jurisdiction OR NEW.registry_source IS DISTINCT FROM OLD.registry_source OR NEW.issued_at IS DISTINCT FROM OLD.issued_at OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''compliance document identity immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''compliance document version invalid''; END IF; IF OLD.status IN (''revoked'',''expired'') THEN RAISE EXCEPTION ''terminal compliance document immutable''; END IF; IF OLD.status=''draft'' AND NEW.status NOT IN (''draft'',''valid'',''verification_failed'',''revoked'') THEN RAISE EXCEPTION ''invalid compliance status transition''; ELSIF OLD.status=''valid'' AND NEW.status NOT IN (''valid'',''suspended'',''revoked'',''expired'',''verification_failed'') THEN RAISE EXCEPTION ''invalid compliance status transition''; ELSIF OLD.status=''suspended'' AND NEW.status NOT IN (''suspended'',''valid'',''revoked'',''expired'',''verification_failed'') THEN RAISE EXCEPTION ''invalid compliance status transition''; ELSIF OLD.status=''verification_failed'' AND NEW.status NOT IN (''verification_failed'',''valid'',''revoked'',''expired'') THEN RAISE EXCEPTION ''invalid compliance status transition''; END IF; RETURN NEW; END';
CREATE TRIGGER compliance_documents_guard BEFORE INSERT OR UPDATE ON compliance_documents FOR EACH ROW EXECUTE FUNCTION compliance_document_guard();

CREATE TABLE compliance_bindings (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL,
  subject_type text NOT NULL CHECK(subject_type IN ('product','offer','category','gtin','sku')), subject_id text NOT NULL,
  active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>=1), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT compliance_bindings_document_fk FOREIGN KEY(organization_id,workspace_id,document_id) REFERENCES compliance_documents(organization_id,workspace_id,id), UNIQUE(organization_id,workspace_id,document_id,subject_type,subject_id)
);
CREATE INDEX compliance_bindings_subject_idx ON compliance_bindings(organization_id,workspace_id,subject_type,subject_id,active,document_id);
CREATE FUNCTION compliance_binding_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NOT compliance_subject_ref_exists(NEW.organization_id,NEW.workspace_id,NEW.subject_type,NEW.subject_id) THEN RAISE EXCEPTION ''compliance subject missing''; END IF; IF TG_OP=''INSERT'' THEN IF NEW.version<>1 THEN RAISE EXCEPTION ''compliance binding must start v1''; END IF; RETURN NEW; END IF; IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.document_id IS DISTINCT FROM OLD.document_id OR NEW.subject_type IS DISTINCT FROM OLD.subject_type OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN RAISE EXCEPTION ''compliance binding identity immutable''; END IF; IF NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION ''compliance binding version invalid''; END IF; RETURN NEW; END';
CREATE TRIGGER compliance_bindings_guard BEFORE INSERT OR UPDATE ON compliance_bindings FOR EACH ROW EXECUTE FUNCTION compliance_binding_guard();

CREATE TABLE compliance_policies (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, code text NOT NULL CHECK(code ~ '^[a-z][a-z0-9._:/-]{0,127}$'), jurisdiction text NOT NULL CHECK(jurisdiction ~ '^[A-Z]{2}$'), operation text NOT NULL CHECK(operation IN ('publication','sale','advertising','shipping')),
  connector_family text NOT NULL DEFAULT '' CHECK(connector_family='' OR connector_family ~ '^[a-z][a-z0-9._:/-]{0,127}$'), seller_role text NOT NULL DEFAULT '' CHECK(seller_role='' OR seller_role ~ '^[a-z][a-z0-9._:/-]{0,127}$'), category_id text,
  requirements jsonb NOT NULL CHECK(compliance_requirements_valid(requirements)), effective_from timestamptz NOT NULL, effective_until timestamptz, active boolean NOT NULL DEFAULT true, version bigint NOT NULL CHECK(version>=1), created_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id,version), CONSTRAINT compliance_policies_workspace_fk FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id), CONSTRAINT compliance_policies_category_fk FOREIGN KEY(organization_id,workspace_id,category_id) REFERENCES pim_categories(organization_id,workspace_id,id), UNIQUE(organization_id,workspace_id,code,version), CONSTRAINT compliance_policy_dates CHECK(effective_until IS NULL OR effective_until>effective_from)
);
CREATE INDEX compliance_policy_eval_idx ON compliance_policies(organization_id,workspace_id,jurisdiction,operation,active,effective_from,version);
CREATE FUNCTION compliance_policy_immutable() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''compliance policy versions are append-only''; END';
CREATE TRIGGER compliance_policies_immutable BEFORE UPDATE OR DELETE ON compliance_policies FOR EACH ROW EXECUTE FUNCTION compliance_policy_immutable();

CREATE TABLE compliance_verifications (
  id text NOT NULL, organization_id text NOT NULL, workspace_id text NOT NULL, document_id text NOT NULL, source text NOT NULL CHECK(source ~ '^[a-z][a-z0-9._:/-]{0,127}$'), status text NOT NULL CHECK(status IN ('valid','suspended','revoked','expired','verification_failed')), registry_reference text NOT NULL DEFAULT '' CHECK(char_length(registry_reference)<=256), checked_at timestamptz NOT NULL,
  PRIMARY KEY(organization_id,workspace_id,id), CONSTRAINT compliance_verification_document_fk FOREIGN KEY(organization_id,workspace_id,document_id) REFERENCES compliance_documents(organization_id,workspace_id,id)
);
CREATE INDEX compliance_verification_timeline_idx ON compliance_verifications(organization_id,workspace_id,document_id,checked_at DESC,id);
CREATE TRIGGER compliance_verifications_immutable BEFORE UPDATE OR DELETE ON compliance_verifications FOR EACH ROW EXECUTE FUNCTION compliance_policy_immutable();

ALTER TABLE compliance_documents ENABLE ROW LEVEL SECURITY; ALTER TABLE compliance_documents FORCE ROW LEVEL SECURITY;
ALTER TABLE compliance_bindings ENABLE ROW LEVEL SECURITY; ALTER TABLE compliance_bindings FORCE ROW LEVEL SECURITY;
ALTER TABLE compliance_policies ENABLE ROW LEVEL SECURITY; ALTER TABLE compliance_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE compliance_verifications ENABLE ROW LEVEL SECURITY; ALTER TABLE compliance_verifications FORCE ROW LEVEL SECURITY;
CREATE POLICY compliance_documents_all ON compliance_documents FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY compliance_bindings_all ON compliance_bindings FOR ALL USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY compliance_policies_select ON compliance_policies FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY compliance_policies_insert ON compliance_policies FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY compliance_verifications_select ON compliance_verifications FOR SELECT USING(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY compliance_verifications_insert ON compliance_verifications FOR INSERT WITH CHECK(organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

CREATE FUNCTION compliance_no_delete() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''compliance evidence cannot be hard-deleted''; END';
CREATE FUNCTION compliance_no_clear() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''compliance evidence cannot be cleared''; END';
CREATE TRIGGER compliance_documents_no_delete BEFORE DELETE ON compliance_documents FOR EACH ROW EXECUTE FUNCTION compliance_no_delete(); CREATE TRIGGER compliance_documents_no_clear BEFORE TRUNCATE ON compliance_documents EXECUTE FUNCTION compliance_no_clear();
CREATE TRIGGER compliance_bindings_no_delete BEFORE DELETE ON compliance_bindings FOR EACH ROW EXECUTE FUNCTION compliance_no_delete(); CREATE TRIGGER compliance_bindings_no_clear BEFORE TRUNCATE ON compliance_bindings EXECUTE FUNCTION compliance_no_clear();
CREATE TRIGGER compliance_policies_no_clear BEFORE TRUNCATE ON compliance_policies EXECUTE FUNCTION compliance_no_clear();
CREATE TRIGGER compliance_verifications_no_clear BEFORE TRUNCATE ON compliance_verifications EXECUTE FUNCTION compliance_no_clear();

COMMENT ON TABLE compliance_documents IS 'Canonical product-compliance evidence registry; status/verification history is auditable and tenant scoped.';
COMMENT ON TABLE compliance_policies IS 'Append-only versioned product compliance policy; publication evaluation uses exact historical policy versions.';
-- BASELINE_SOURCE_END

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
