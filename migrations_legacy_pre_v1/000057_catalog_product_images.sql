BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE catalog_product_images (
  id text NOT NULL,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  product_id text NOT NULL,
  url text NOT NULL CHECK (char_length(url) <= 2039 AND url ~ '^https://[^[:space:]]+$'),
  alt_text text NOT NULL DEFAULT '' CHECK (alt_text = btrim(alt_text) AND char_length(alt_text) <= 300),
  position smallint NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 255),
  version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (organization_id, workspace_id, id),
  CONSTRAINT catalog_product_images_product_fk FOREIGN KEY (organization_id, workspace_id, product_id)
    REFERENCES products (organization_id, workspace_id, id) ON DELETE RESTRICT,
  UNIQUE (organization_id, workspace_id, product_id, url)
);

CREATE INDEX catalog_product_images_product_idx
  ON catalog_product_images (organization_id, workspace_id, product_id, position, id);

ALTER TABLE catalog_product_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_product_images FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_product_images_tenant_all ON catalog_product_images
  USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true))
  WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

REVOKE DELETE, TRUNCATE ON catalog_product_images FROM PUBLIC;

CREATE FUNCTION catalog_product_images_guard() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  IF TG_OP = ''INSERT'' THEN
    IF NEW.version <> 1 THEN RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''new product image must start at version 1''; END IF;
    RETURN NEW;
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
     OR NEW.product_id IS DISTINCT FROM OLD.product_id OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''product image identity is immutable'';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''product image version transition is invalid'';
  END IF;
  RETURN NEW;
END';
CREATE TRIGGER catalog_product_images_guard_insert BEFORE INSERT ON catalog_product_images FOR EACH ROW EXECUTE FUNCTION catalog_product_images_guard();
CREATE TRIGGER catalog_product_images_guard_update BEFORE UPDATE ON catalog_product_images FOR EACH ROW EXECUTE FUNCTION catalog_product_images_guard();

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);

COMMIT;
