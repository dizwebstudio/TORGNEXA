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

Client-credentials manifests do not open a browser. Their client registration
is exchanged transiently during the remote check.

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
`remote_check_not_configured`. Account activation separately requires current
healthy evidence and at least one explicitly enabled capability. Task 109 owns
bounded history, detailed rate-limit visibility and operational remediation;
it does not replace this validation gate or authoritative audit evidence.
