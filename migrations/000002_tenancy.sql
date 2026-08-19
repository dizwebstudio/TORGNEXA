BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE organizations
  ADD COLUMN status text NOT NULL DEFAULT 'active',
  ADD COLUMN version bigint NOT NULL DEFAULT 1,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE organizations
  ADD CONSTRAINT organizations_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ) NOT VALID,
  ADD CONSTRAINT organizations_name_chk CHECK (
    name = btrim(name) AND char_length(name) BETWEEN 1 AND 200
  ) NOT VALID,
  ADD CONSTRAINT organizations_status_chk CHECK (
    status IN ('active', 'suspended', 'archived')
  ) NOT VALID,
  ADD CONSTRAINT organizations_version_chk CHECK (version >= 1) NOT VALID,
  ADD CONSTRAINT organizations_timestamps_chk CHECK (updated_at >= created_at) NOT VALID;

ALTER TABLE workspaces
  ADD COLUMN status text NOT NULL DEFAULT 'active',
  ADD COLUMN version bigint NOT NULL DEFAULT 1,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE workspaces
  ADD CONSTRAINT workspaces_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ) NOT VALID,
  ADD CONSTRAINT workspaces_name_chk CHECK (
    name = btrim(name) AND char_length(name) BETWEEN 1 AND 200
  ) NOT VALID,
  ADD CONSTRAINT workspaces_status_chk CHECK (
    status IN ('active', 'suspended', 'archived')
  ) NOT VALID,
  ADD CONSTRAINT workspaces_version_chk CHECK (version >= 1) NOT VALID,
  ADD CONSTRAINT workspaces_timestamps_chk CHECK (updated_at >= created_at) NOT VALID,
  ADD CONSTRAINT workspaces_organization_id_id_key UNIQUE (organization_id, id);

CREATE TABLE stores (
  id text PRIMARY KEY,
  organization_id text NOT NULL,
  workspace_id text NOT NULL,
  code text NOT NULL,
  name text NOT NULL,
  kind text NOT NULL DEFAULT 'store',
  status text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT stores_id_sortable_chk CHECK (
    id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    OR id ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'
  ),
  CONSTRAINT stores_code_chk CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
  CONSTRAINT stores_name_chk CHECK (
    name = btrim(name) AND char_length(name) BETWEEN 1 AND 200
  ),
  CONSTRAINT stores_kind_chk CHECK (kind IN ('store', 'business_unit')),
  CONSTRAINT stores_status_chk CHECK (status IN ('active', 'suspended', 'archived')),
  CONSTRAINT stores_version_chk CHECK (version >= 1),
  CONSTRAINT stores_timestamps_chk CHECK (updated_at >= created_at),
  CONSTRAINT stores_workspace_fk FOREIGN KEY (organization_id, workspace_id)
    REFERENCES workspaces (organization_id, id) ON DELETE RESTRICT,
  CONSTRAINT stores_organization_workspace_id_key UNIQUE (organization_id, workspace_id, id),
  CONSTRAINT stores_organization_workspace_code_key UNIQUE (organization_id, workspace_id, code)
);

CREATE INDEX stores_workspace_status_idx
  ON stores (organization_id, workspace_id, status, id);

ALTER TABLE connector_accounts
  ADD CONSTRAINT connector_accounts_workspace_scope_fk
  FOREIGN KEY (organization_id, workspace_id)
  REFERENCES workspaces (organization_id, id)
  ON DELETE RESTRICT
  NOT VALID;

ALTER TABLE outbox_events
  ADD CONSTRAINT outbox_events_workspace_scope_fk
  FOREIGN KEY (organization_id, workspace_id)
  REFERENCES workspaces (organization_id, id)
  ON DELETE RESTRICT
  NOT VALID;

ALTER TABLE audit_records
  ADD CONSTRAINT audit_records_workspace_scope_fk
  FOREIGN KEY (organization_id, workspace_id)
  REFERENCES workspaces (organization_id, id)
  ON DELETE RESTRICT
  NOT VALID;

ALTER TABLE organizations
  VALIDATE CONSTRAINT organizations_id_sortable_chk,
  VALIDATE CONSTRAINT organizations_name_chk,
  VALIDATE CONSTRAINT organizations_status_chk,
  VALIDATE CONSTRAINT organizations_version_chk,
  VALIDATE CONSTRAINT organizations_timestamps_chk;

ALTER TABLE workspaces
  VALIDATE CONSTRAINT workspaces_id_sortable_chk,
  VALIDATE CONSTRAINT workspaces_name_chk,
  VALIDATE CONSTRAINT workspaces_status_chk,
  VALIDATE CONSTRAINT workspaces_version_chk,
  VALIDATE CONSTRAINT workspaces_timestamps_chk;

ALTER TABLE connector_accounts
  VALIDATE CONSTRAINT connector_accounts_workspace_scope_fk;

ALTER TABLE outbox_events
  VALIDATE CONSTRAINT outbox_events_workspace_scope_fk;

ALTER TABLE audit_records
  VALIDATE CONSTRAINT audit_records_workspace_scope_fk;

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY organizations_tenant_isolation ON organizations
  FOR ALL
  USING (id = current_setting('app.organization_id', true))
  WITH CHECK (id = current_setting('app.organization_id', true));

ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
CREATE POLICY workspaces_tenant_isolation ON workspaces
  FOR ALL
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND id = current_setting('app.workspace_id', true)
  );

ALTER TABLE stores ENABLE ROW LEVEL SECURITY;
ALTER TABLE stores FORCE ROW LEVEL SECURITY;
CREATE POLICY stores_tenant_isolation ON stores
  FOR ALL
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

ALTER TABLE connector_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY connector_accounts_tenant_isolation ON connector_accounts
  FOR ALL
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;
CREATE POLICY outbox_events_tenant_isolation ON outbox_events
  FOR ALL
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

ALTER TABLE audit_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_records FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_records_tenant_isolation ON audit_records
  FOR ALL
  USING (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  )
  WITH CHECK (
    organization_id = current_setting('app.organization_id', true)
    AND workspace_id = current_setting('app.workspace_id', true)
  );

-- The placeholder inbox table has no tenant columns yet. Keep it inaccessible
-- to application/table-owner roles until Task 009 replaces this deny-all state
-- with tenant-scoped inbox/idempotency policies.
ALTER TABLE inbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbox_events FORCE ROW LEVEL SECURITY;

COMMENT ON TABLE stores IS 'Tenant-scoped stores and business units; application access requires transaction-local app.organization_id and app.workspace_id.';
COMMENT ON TABLE inbox_events IS 'Reserved and deny-all until Task 009 adds tenant-scoped inbox/idempotency semantics.';

COMMIT;
