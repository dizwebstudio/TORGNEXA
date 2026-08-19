BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE TABLE support_conversations (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, provider text NOT NULL, account_id text NOT NULL, remote_thread_id text NOT NULL, case_id text NOT NULL DEFAULT '', assignee_id text NOT NULL DEFAULT '', sla_deadline timestamptz, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, UNIQUE(organization_id,workspace_id,provider,account_id,remote_thread_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE support_messages (organization_id text NOT NULL, workspace_id text NOT NULL, message_id text NOT NULL, conversation_id text NOT NULL REFERENCES support_conversations(id), remote_message_id text NOT NULL, direction text NOT NULL CHECK(direction IN ('in','out')), redacted_body text NOT NULL, occurred_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,message_id), UNIQUE(organization_id,workspace_id,conversation_id,remote_message_id));
CREATE TABLE support_cases (id text PRIMARY KEY, organization_id text NOT NULL, workspace_id text NOT NULL, conversation_id text NOT NULL REFERENCES support_conversations(id), state text NOT NULL CHECK(state IN ('open','pending','resolved')), assignee_id text NOT NULL DEFAULT '', sla_deadline timestamptz NOT NULL, version bigint NOT NULL CHECK(version>=1), updated_at timestamptz NOT NULL, UNIQUE(organization_id,workspace_id,conversation_id), FOREIGN KEY(organization_id,workspace_id) REFERENCES workspaces(organization_id,id));
CREATE TABLE support_case_assignments (organization_id text NOT NULL, workspace_id text NOT NULL, assignment_id text NOT NULL, case_id text NOT NULL REFERENCES support_cases(id), assignee_id text NOT NULL, assigned_at timestamptz NOT NULL, PRIMARY KEY(organization_id,workspace_id,assignment_id));
ALTER TABLE support_conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_conversations FORCE ROW LEVEL SECURITY;
CREATE POLICY support_conversations_tenant_policy ON support_conversations FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY support_messages_tenant_policy ON support_messages FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY support_cases_tenant_policy ON support_cases FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));
ALTER TABLE support_case_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_case_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY support_case_assignments_tenant_policy ON support_case_assignments FOR ALL USING (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true)) WITH CHECK (organization_id=current_setting('app.organization_id',true) AND workspace_id=current_setting('app.workspace_id',true));

INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (
 current_setting('torgnexa.migration_version')::integer,current_setting('torgnexa.migration_name'),current_setting('torgnexa.migration_file'),current_setting('torgnexa.migration_phase'),current_setting('torgnexa.migration_risk'),current_setting('torgnexa.migration_checksum'),current_setting('torgnexa.application_version'),current_setting('torgnexa.migration_execution_id'),current_setting('torgnexa.migration_duration_ms')::bigint
);
COMMIT;
