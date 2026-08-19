# Task 101: Identity Provider Settings

## Status
`implemented`

## Objective
Add reviewed OIDC and VK identity-provider configuration through the existing Keycloak/enterprise-IAM boundary.

## Dependencies
100, 084

## Acceptance
- provider-neutral configuration model; Core contains no VK-specific branch;
- client secrets use the secrets abstraction and never appear in API responses, logs or plaintext DB columns;
- callback/issuer URLs are allowlisted and SSRF-safe;
- enabling or changing a provider requires an admin capability and audit record;
- configuration supports validation before activation and a safe rollback path.

## Implementation

- `/api/v1/settings/identity-providers` exposes bounded tenant-scoped OIDC configuration without provider-name dispatch; `provider_id=vk` and corporate providers use the same model.
- Migration `000065` stores mutable current/active pointers plus immutable configuration revisions and append-only discovery validation evidence under forced RLS.
- Client secrets enter the existing `SecretProvider` as `oauth_client` material. API responses expose only `secret_configured`; plaintext and secret references are absent.
- Issuers must match `TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS`, resolve only to public addresses and use HTTPS. Callbacks must exactly match `/oidc/callback` on a configured `TORGNEXA_SECURITY_ALLOWED_ORIGINS` origin.
- Validation performs redirect-free, response-bounded, DNS-pinned OIDC discovery. Activation accepts only the current successfully validated revision.
- Rollback copies a previously validated revision into a new immutable revision and activates it atomically, preserving monotonic history and audit evidence.
- The dedicated Settings tab supports draft creation, secret replacement, discovery validation, activation, disable and rollback. Every mutation requires the admin-only settings capability and append-only audit.
