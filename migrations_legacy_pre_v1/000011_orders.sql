BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
