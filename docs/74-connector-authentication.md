# Connector authentication and validation

Task 106 closes the production authentication flow without importing provider
implementations into the API process. Canonical connector manifests declare
OAuth grant/endpoints/scopes and an optional exact HTTPS `connection_test`.
Connectors without a reviewed probe fail closed as
`remote_check_not_configured` and cannot be enabled.

## OAuth lifecycle

The browser first stores a JSON client registration containing `client_id` and
`client_secret`; the API encrypts it through `SecretProvider`. For an
authorization-code manifest, `POST /connector-accounts:oauth-start` creates
cryptographic state plus a PKCE S256 verifier/challenge. Only the state digest,
tenant, actor, account version, exact callback, encrypted temporary-secret
reference and ten-minute expiry are stored in PostgreSQL.

The provider redirects to `/oauth/connectors/callback`. The authenticated shell
sends code, state and the same callback to
`POST /connector-accounts:oauth-callback`. PostgreSQL atomically consumes the
state once while rechecking actor, callback, expiry and disabled account
version. The API exchanges the code through redirect-free, DNS-pinned HTTPS,
stores the token bundle encrypted, and revokes both the client-registration and
temporary state secrets. No client secret, verifier, access token, refresh
token or provider response body is returned or logged.

Task 134 makes that encrypted bundle executable without exposing its storage
shape to a connector. An account-aware host runtime supplies only the current
access token through `SecretAccessor.UseSecret`. One minute before expiry, API
or worker acquires a tenant/reference-scoped PostgreSQL transaction advisory
lock, re-reads the bundle and performs at most one refresh against the exact
manifest token endpoint. The stable secret reference is rotated to one new
immutable ciphertext version. A returned replacement refresh token is stored;
if the provider omits it, the prior refresh token is preserved.

If refresh material is absent or revoked, health becomes
`oauth_reauthorization_required`. Token endpoint or encrypted rotation failure
becomes `oauth_refresh_failed`. Neither outcome exposes provider error bodies.
Repeated browser OAuth is therefore a remediation path, not the normal access
token renewal mechanism.

Client-credentials manifests do not open a browser. Their client registration
is exchanged transiently by the host when the connector needs an access token;
client credentials are never passed into the provider adapter or cached as
long-lived plaintext.

## Callback and egress policy

Allowed browser origins come from `TORGNEXA_SECURITY_ALLOWED_ORIGINS` and expand
only to the exact `/oauth/connectors/callback` path. HTTPS is mandatory except
for loopback development origins. Query strings, fragments, userinfo and other
paths are rejected.

OAuth and connection-test destinations are immutable manifest HTTPS URLs.
Current DNS answers must all be public global-unicast addresses; private,
loopback, link-local, multicast and special-use ranges are denied. Connections
are pinned to the validated answers, redirects are rejected, TLS 1.2+ is
required, timeouts are capped at 15 seconds and response bodies at 64 KiB.

## Activation invariant

Credential enrollment and OAuth completion always leave the account disabled
and reset health. `POST /connector-accounts:check` performs the real remote
request and persists only `healthy`, `auth_rejected`, `rate_limited`,
`remote_unavailable`, `credentials_*`, `oauth_exchange_failed` or
`remote_check_not_configured`, plus Task-134 `oauth_refresh_failed` and
`oauth_reauthorization_required`. Account activation separately requires current
healthy evidence and at least one explicitly enabled capability. Task 109 owns
bounded history, detailed rate-limit visibility and operational remediation;
it does not replace this validation gate or authoritative audit evidence.
