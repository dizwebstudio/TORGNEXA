BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
