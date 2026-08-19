BEGIN;

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
