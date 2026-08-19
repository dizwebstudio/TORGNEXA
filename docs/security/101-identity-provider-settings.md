# Identity provider settings

TORGNEXA continues to authenticate only through Keycloak/OIDC. The Settings surface manages reviewed upstream OIDC configuration; it does not accept LDAP passwords, trust external groups as roles or bypass the Task-084 mapping boundary.

## Provider-neutral configuration

`provider_id` is a tenant label, not a Core dispatch key. VK ID, a corporate OIDC server and another compatible provider use the same protocol model. Provider-specific behavior belongs in the identity boundary, never in commerce Core.

## URL and secret policy

- `TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS` is the exact deployment allowlist for issuer and discovery metadata hosts. Empty means deny all managed providers.
- callback URLs are limited to the exact `/oidc/callback` URL derived from `TORGNEXA_SECURITY_ALLOWED_ORIGINS`; HTTPS is required except for explicit loopback development origins;
- managed issuers and discovery endpoints require HTTPS, redirect following is disabled, DNS answers must be public and the discovery connection is pinned to the validated addresses;
- discovery responses are limited to 64 KiB and raw provider errors/bodies are not persisted or returned;
- client secrets are created as `oauth_client` material through the secrets abstraction. Responses expose only a boolean presence marker.

## Lifecycle

Saving creates an inactive immutable draft revision. Discovery validation appends evidence for that exact revision. Activation fails closed unless the latest evidence for the current revision is successful. Editing an active provider leaves the previous active revision in service until the new draft is validated and activated. Rollback creates and activates a new revision copied from previously validated evidence.

Every write requires `settings.identity_providers.write`, an idempotency key, optimistic version matching and append-only audit evidence.
