BEGIN;
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

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
