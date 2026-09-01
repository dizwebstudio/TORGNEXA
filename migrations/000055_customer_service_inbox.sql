BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Task 228 / Epic 183. Customer service links to canonical commerce
-- aggregates; it is not a second CRM, customer master or claims ledger.
-- Only minimized references and sanitized text are retained.
CREATE TABLE customer_service_customer_refs (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  customer_ref_id text NOT NULL,
  source_system text NOT NULL,
  remote_customer_ref text NOT NULL,
  display_name_mask text NOT NULL DEFAULT '',
  contact_mask text NOT NULL DEFAULT '',
  identity_state text NOT NULL,
  confidence_bps integer NOT NULL DEFAULT 0,
  source text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (organization_id,workspace_id,customer_ref_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,source_system,remote_customer_ref),
  CONSTRAINT customer_service_customer_ref_chk CHECK (
    customer_ref_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    source_system ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    remote_customer_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    char_length(display_name_mask) <= 160 AND char_length(contact_mask) <= 160 AND
    display_name_mask !~ '[\r\n\x00]' AND contact_mask !~ '[\r\n\x00]' AND
    identity_state IN ('verified','ambiguous','unmatched') AND
    confidence_bps BETWEEN 0 AND 10000 AND source ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    created_at <= updated_at AND version >= 1
  )
);
CREATE INDEX customer_service_customer_refs_updated_idx ON customer_service_customer_refs(organization_id,workspace_id,updated_at DESC,customer_ref_id DESC);

CREATE TABLE customer_service_conversations (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  conversation_id text NOT NULL,
  source_system text NOT NULL,
  account_id text NOT NULL,
  remote_thread_id text NOT NULL,
  conversation_type text NOT NULL,
  state text NOT NULL,
  priority text NOT NULL,
  customer_ref_id text,
  identity_state text NOT NULL,
  subject text NOT NULL DEFAULT '',
  order_id text NOT NULL DEFAULT '',
  order_item_id text NOT NULL DEFAULT '',
  product_id text NOT NULL DEFAULT '',
  offer_id text NOT NULL DEFAULT '',
  return_id text NOT NULL DEFAULT '',
  claim_id text NOT NULL DEFAULT '',
  assignee_id text NOT NULL DEFAULT '',
  team_id text NOT NULL DEFAULT '',
  sla_state text NOT NULL DEFAULT 'new',
  first_response_due_at timestamptz,
  resolution_due_at timestamptz,
  last_message_at timestamptz NOT NULL,
  source_quality text NOT NULL,
  moderation_state text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,conversation_id),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  FOREIGN KEY (organization_id,workspace_id,customer_ref_id) REFERENCES customer_service_customer_refs(organization_id,workspace_id,customer_ref_id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,source_system,account_id,remote_thread_id),
  CONSTRAINT customer_service_conversation_chk CHECK (
    conversation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    source_system ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND account_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    remote_thread_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    conversation_type IN ('message','review','question','claim','return_request','delivery_failure') AND
    state IN ('unread','open','pending_customer','pending_internal','resolved','closed','spam') AND
    priority IN ('low','normal','high','urgent') AND identity_state IN ('verified','ambiguous','unmatched') AND
    char_length(subject) <= 500 AND subject !~ '[\x00]' AND
    char_length(order_id) <= 192 AND char_length(order_item_id) <= 192 AND char_length(product_id) <= 192 AND
    char_length(offer_id) <= 192 AND char_length(return_id) <= 192 AND char_length(claim_id) <= 192 AND
    char_length(assignee_id) <= 192 AND char_length(team_id) <= 192 AND
    sla_state IN ('new','in_progress','waiting','escalated','breached','met') AND
    source_quality IN ('observed','confirmed','partial','stale','unknown') AND
    moderation_state IN ('pending','approved','blocked','spam') AND
    version >= 1 AND created_at <= updated_at
  )
);
CREATE INDEX customer_service_conversations_queue_idx ON customer_service_conversations(organization_id,workspace_id,state,priority,sla_state,updated_at DESC,conversation_id DESC);
CREATE INDEX customer_service_conversations_customer_idx ON customer_service_conversations(organization_id,workspace_id,customer_ref_id,last_message_at DESC,conversation_id DESC);
CREATE INDEX customer_service_conversations_order_idx ON customer_service_conversations(organization_id,workspace_id,order_id,conversation_id) WHERE order_id <> '';

CREATE TABLE customer_service_messages (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  message_id text NOT NULL,
  conversation_id text NOT NULL,
  remote_message_id text NOT NULL DEFAULT '',
  direction text NOT NULL,
  visibility text NOT NULL,
  delivery_state text NOT NULL,
  safe_text text NOT NULL,
  content_digest char(64) NOT NULL,
  language text NOT NULL DEFAULT '',
  moderation_state text NOT NULL,
  identity_state text NOT NULL,
  order_id text NOT NULL DEFAULT '',
  product_id text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,message_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  CONSTRAINT customer_service_message_chk CHECK (
    message_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND conversation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    char_length(remote_message_id) <= 192 AND direction IN ('inbound','outbound') AND visibility IN ('public','internal') AND
    delivery_state IN ('observed','draft','queued','sent','accepted','failed','unknown') AND char_length(safe_text) BETWEEN 1 AND 16000 AND
    content_digest ~ '^[0-9a-f]{64}$' AND language ~ '^[A-Za-z-]{0,16}$' AND
    moderation_state IN ('pending','approved','blocked','spam') AND identity_state IN ('verified','ambiguous','unmatched') AND
    char_length(order_id) <= 192 AND char_length(product_id) <= 192
  )
);
CREATE UNIQUE INDEX customer_service_messages_remote_uq ON customer_service_messages(organization_id,workspace_id,conversation_id,remote_message_id) WHERE remote_message_id <> '';
CREATE INDEX customer_service_messages_conversation_idx ON customer_service_messages(organization_id,workspace_id,conversation_id,occurred_at,message_id);

CREATE TABLE customer_service_replies (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  reply_id text NOT NULL,
  conversation_id text NOT NULL,
  visibility text NOT NULL,
  origin text NOT NULL,
  safe_text text NOT NULL,
  content_digest char(64) NOT NULL,
  template_id text NOT NULL DEFAULT '',
  approval_ref text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL,
  delivery_state text NOT NULL,
  remote_receipt text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (organization_id,workspace_id,reply_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,idempotency_key),
  CONSTRAINT customer_service_reply_chk CHECK (
    reply_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND conversation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    visibility IN ('public','internal') AND origin IN ('human','template','ai_draft') AND char_length(safe_text) BETWEEN 1 AND 16000 AND
    content_digest ~ '^[0-9a-f]{64}$' AND char_length(template_id) <= 192 AND char_length(approval_ref) <= 192 AND
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$' AND delivery_state IN ('observed','draft','queued','sent','accepted','failed','unknown') AND
    char_length(remote_receipt) <= 192 AND char_length(error_code) <= 128 AND created_at <= updated_at AND version >= 1 AND
    (visibility = 'internal' AND delivery_state IN ('draft','observed') OR visibility = 'public') AND
    (origin <> 'ai_draft' OR delivery_state = 'draft')
  )
);
CREATE INDEX customer_service_replies_conversation_idx ON customer_service_replies(organization_id,workspace_id,conversation_id,created_at,reply_id);

CREATE TABLE customer_service_assignments (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  assignment_id text NOT NULL,
  conversation_id text NOT NULL,
  assignee_id text NOT NULL DEFAULT '',
  team_id text NOT NULL DEFAULT '',
  reason text NOT NULL DEFAULT '',
  expected_version bigint NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,assignment_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  CONSTRAINT customer_service_assignment_chk CHECK (
    assignment_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND conversation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    (assignee_id <> '' OR team_id <> '') AND char_length(assignee_id) <= 192 AND char_length(team_id) <= 192 AND
    char_length(reason) <= 500 AND expected_version >= 1
  )
);
CREATE INDEX customer_service_assignments_conversation_idx ON customer_service_assignments(organization_id,workspace_id,conversation_id,created_at DESC,assignment_id DESC);

CREATE TABLE customer_service_sla_policies (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  policy_id text NOT NULL,
  version bigint NOT NULL,
  conversation_type text NOT NULL,
  priority text NOT NULL,
  timezone text NOT NULL,
  first_response_minutes integer NOT NULL,
  resolution_minutes integer NOT NULL,
  holidays jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,policy_id,version),
  FOREIGN KEY (organization_id,workspace_id) REFERENCES workspaces(organization_id,id) ON DELETE RESTRICT,
  CONSTRAINT customer_service_sla_policy_chk CHECK (
    policy_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND version >= 1 AND
    conversation_type IN ('message','review','question','claim','return_request','delivery_failure') AND
    priority IN ('low','normal','high','urgent') AND char_length(timezone) BETWEEN 1 AND 128 AND
    first_response_minutes BETWEEN 1 AND 1000000 AND resolution_minutes BETWEEN first_response_minutes AND 2000000 AND
    jsonb_typeof(holidays) = 'array'
  )
);
CREATE TABLE customer_service_sla_events (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  event_id text NOT NULL,
  conversation_id text NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL,
  kind text NOT NULL,
  occurred_at timestamptz NOT NULL,
  notification_key text NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,event_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  UNIQUE (organization_id,workspace_id,notification_key),
  CONSTRAINT customer_service_sla_event_chk CHECK (
    event_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND conversation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    policy_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND policy_version >= 1 AND kind IN ('warning','breached','escalated') AND
    notification_key ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$'
  )
);

CREATE TABLE customer_service_attachments (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  attachment_id text NOT NULL,
  conversation_id text NOT NULL,
  upload_id text NOT NULL,
  object_ref text NOT NULL,
  visibility text NOT NULL,
  release_state text NOT NULL,
  content_digest char(64) NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id,workspace_id,attachment_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  CONSTRAINT customer_service_attachment_chk CHECK (
    attachment_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND upload_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND
    object_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND visibility IN ('public','internal') AND
    release_state IN ('quarantined','released','rejected') AND content_digest ~ '^[0-9a-f]{64}$'
  )
);

CREATE TABLE customer_service_findings (
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  finding_id text NOT NULL,
  conversation_id text,
  kind text NOT NULL,
  severity text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  explanation text NOT NULL,
  expected_digest char(64),
  observed_digest char(64),
  detected_at timestamptz NOT NULL,
  resolved_at timestamptz,
  PRIMARY KEY (organization_id,workspace_id,finding_id),
  FOREIGN KEY (organization_id,workspace_id,conversation_id) REFERENCES customer_service_conversations(organization_id,workspace_id,conversation_id) ON DELETE RESTRICT,
  CONSTRAINT customer_service_finding_chk CHECK (
    finding_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$' AND char_length(conversation_id) <= 192 AND
    kind ~ '^[a-z][a-z0-9._-]{0,63}$' AND severity IN ('info','warn','block') AND status IN ('open','acknowledged','resolved') AND
    char_length(explanation) BETWEEN 1 AND 500 AND (expected_digest IS NULL OR expected_digest ~ '^[0-9a-f]{64}$') AND
    (observed_digest IS NULL OR observed_digest ~ '^[0-9a-f]{64}$') AND (resolved_at IS NULL OR resolved_at >= detected_at)
  )
);
CREATE INDEX customer_service_findings_queue_idx ON customer_service_findings(organization_id,workspace_id,status,severity,detected_at DESC,finding_id DESC);

CREATE FUNCTION customer_service_append_only() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN
  RAISE EXCEPTION USING ERRCODE=''55000'', MESSAGE=''customer service history is append-only'';
  RETURN NULL;
END';
CREATE TRIGGER customer_service_messages_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON customer_service_messages FOR EACH STATEMENT EXECUTE FUNCTION customer_service_append_only();
CREATE TRIGGER customer_service_assignments_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON customer_service_assignments FOR EACH STATEMENT EXECUTE FUNCTION customer_service_append_only();
CREATE TRIGGER customer_service_sla_events_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON customer_service_sla_events FOR EACH STATEMENT EXECUTE FUNCTION customer_service_append_only();
CREATE TRIGGER customer_service_attachments_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON customer_service_attachments FOR EACH STATEMENT EXECUTE FUNCTION customer_service_append_only();
CREATE TRIGGER customer_service_findings_no_mutation BEFORE UPDATE OR DELETE OR TRUNCATE ON customer_service_findings FOR EACH STATEMENT EXECUTE FUNCTION customer_service_append_only();
REVOKE UPDATE,DELETE,TRUNCATE ON customer_service_messages,customer_service_assignments,customer_service_sla_events,customer_service_attachments,customer_service_findings FROM PUBLIC;

ALTER TABLE customer_service_customer_refs ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_customer_refs FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_conversations FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_messages FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_replies FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_assignments FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_sla_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_sla_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_sla_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_sla_events FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_attachments FORCE ROW LEVEL SECURITY;
ALTER TABLE customer_service_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_service_findings FORCE ROW LEVEL SECURITY;

CREATE POLICY customer_service_customer_refs_tenant_all ON customer_service_customer_refs FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_conversations_tenant_all ON customer_service_conversations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_messages_tenant_all ON customer_service_messages FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_replies_tenant_all ON customer_service_replies FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_assignments_tenant_all ON customer_service_assignments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_sla_policies_tenant_all ON customer_service_sla_policies FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_sla_events_tenant_all ON customer_service_sla_events FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_attachments_tenant_all ON customer_service_attachments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
CREATE POLICY customer_service_findings_tenant_all ON customer_service_findings FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

COMMENT ON TABLE customer_service_customer_refs IS 'Minimized tenant-scoped customer identity links; not a customer master or profile store.';
COMMENT ON TABLE customer_service_conversations IS 'Unified support inbox threads linked to canonical order/product/return/claim references.';
COMMENT ON TABLE customer_service_messages IS 'Sanitized immutable inbound/outbound history; raw provider payloads are forbidden.';
COMMENT ON TABLE customer_service_replies IS 'Durable human/template reply intents and redacted remote receipts; AI remains draft-only.';
COMMENT ON TABLE customer_service_attachments IS 'Upload-pipeline references only; content remains quarantine/release controlled.';
COMMENT ON TABLE customer_service_findings IS 'Append-only local/remote inbox reconciliation findings.';

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES(current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint);

COMMIT;
