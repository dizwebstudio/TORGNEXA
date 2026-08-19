BEGIN;

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
