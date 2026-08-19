# Task 084 contract v1

Keycloak/OIDC remains the application identity boundary. LDAP/AD, SAML, SCIM and JIT inputs are translated only through explicit tenant-scoped reviewed mappings; unmapped identities are denied.

All tenant data is organization/workspace scoped, retry/idempotency semantics are explicit, and credentials/PII are minimized behind existing security boundaries.
