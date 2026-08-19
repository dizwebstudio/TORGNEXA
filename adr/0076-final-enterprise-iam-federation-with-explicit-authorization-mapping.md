# ADR 0076: Enterprise IAM federation with explicit authorization mapping

## Status
Accepted

## Context
Task 084 extends Keycloak/OIDC with enterprise federation and provisioning while preserving TORGNEXA authorization semantics across LDAP/AD, SAML, SCIM and JIT.

## Decision
Keep Keycloak/OIDC as authentication boundary and evaluate external groups/claims only through versioned tenant-scoped mapping rules. Unmapped identities are denied; offboarding revokes sessions, API keys and delegations and emits security audit.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Trusting external groups as roles was rejected. Application-level LDAP password handling was rejected in favor of federation through the identity boundary.

## Compatibility impact
Existing OIDC login remains compatible; enterprise mappings are additive and do not alter Community authentication behavior.

## Migration and data impact
Expand-only migration 000051 adds mappings, identity links and service accounts under forced RLS.

## Security and privacy impact
Privileged mappings are explicit/audited, service-account secrets remain references, and disabled/unmapped subjects receive no access.

## Operational impact
Operators must reconcile identity/group drift and test deprovisioning/session revocation for each enterprise IdP.
