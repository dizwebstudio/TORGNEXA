BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
