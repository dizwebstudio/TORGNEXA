# ADR 0026: Enterprise IAM Through Keycloak Federation/Provisioning

## Decision
Keep OIDC/Keycloak as application boundary while adding LDAP/AD, SAML, SCIM and JIT adapters/mapping policies. External identity claims never directly grant TORGNEXA permissions.

## Consequences
Authorization remains deterministic and auditable across self-hosted enterprise identity systems.
