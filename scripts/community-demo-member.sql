INSERT INTO workspace_members (
  id,
  organization_id,
  workspace_id,
  email,
  display_name,
  oidc_subject,
  role_code,
  status,
  invitation_key
) VALUES (
  :'demo_member_id',
  '0198b8d0-0000-7000-8000-000000000001',
  '0198b8d0-0000-7000-8000-000000000002',
  'demo@local.torgnexa',
  'Демо оператор',
  :'demo_subject',
  'admin',
  'active',
  :'demo_invitation_key'
)
ON CONFLICT (organization_id, workspace_id, email) DO UPDATE SET
  display_name = EXCLUDED.display_name,
  oidc_subject = EXCLUDED.oidc_subject,
  role_code = 'admin',
  status = 'active',
  version = workspace_members.version + 1,
  updated_at = clock_timestamp();
