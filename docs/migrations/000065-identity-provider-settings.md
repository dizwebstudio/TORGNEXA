# Migration 000065 — identity provider settings

Migration `000065_identity_provider_settings.sql` is an additive high-risk security migration for Task 101. It does not replace the runtime Keycloak/OIDC boundary.

The migration adds:

- `settings_identity_providers`, containing only the current/active revision pointers and enabled state;
- immutable `settings_identity_provider_revisions`, containing bounded provider-neutral OIDC configuration and an optional opaque `SecretProvider` reference;
- append-only `settings_identity_provider_validations`, containing a metadata digest, normalized endpoint URLs and safe reason codes rather than discovery response bodies.

All three tables are tenant scoped with forced RLS. Revision and validation rows reject update, delete and truncate. Client secret plaintext, tokens and private identity payloads are forbidden.

Rollback is forward-only at the schema level. Application rollback copies an already validated historical revision into a new monotonically increasing revision, so configuration evidence is never rewritten. Old binaries ignore the additive tables.
