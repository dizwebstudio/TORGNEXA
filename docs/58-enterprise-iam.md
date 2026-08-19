# Enterprise IAM

Keycloak/OIDC remains the primary identity boundary. Enterprise deployments add federation/provisioning through a provider-neutral IAM integration layer.

## Required capabilities

- LDAP/Active Directory federation and group mapping;
- SAML 2.0 identity-provider/service-provider patterns where required;
- SCIM 2.0 provisioning/deprovisioning adapter surface;
- JIT provisioning with explicit tenant/workspace mapping rules;
- service accounts with scoped credentials and rotation;
- organization/workspace role mapping from groups/claims;
- MFA/session/token policies;
- break-glass administrative accounts with enhanced audit;
- offboarding that revokes sessions, API keys and delegated access.

## Authorization

Authentication federation never grants implicit authorization. External groups/claims are translated through reviewed mapping rules into TORGNEXA RBAC/ABAC roles/scopes. Default deny applies to unmapped identities.

## Audit and reconciliation

Provisioning/federation changes emit security audit events. Scheduled reconciliation detects stale identities, group drift, orphaned service accounts and disabled users with active sessions/keys.
