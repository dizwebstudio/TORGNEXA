# Settings security

Task 103 adds an administrator-only security section to Settings. PostgreSQL is the authoritative store for minimized application session state and immutable login/audit evidence. Keycloak remains the identity provider and owns passwords, authentication methods and provider-wide SSO sessions.

## Session semantics

After UserInfo validates a bearer token, the API derives SHA-256 references from the configured issuer, OIDC `sub` and `sid` (with issuance time as a bounded fallback). Only those references, timestamps and a bounded client class (`browser`, `mobile`, `api`, `unknown`) are stored. Tokens, raw provider identifiers, IP addresses and raw User-Agent values are prohibited.

Revocation is application-enforced: the session row becomes `revoked` and subsequent requests using that OIDC session receive `401`. The same transaction appends `settings.security.session_revoked` audit evidence with actor and correlation ID. Provider-wide logout is intentionally delegated to the Keycloak account console.

Already-open SSE streams repeat OIDC/session, membership and permission checks
every 15 seconds, with a five-second total timeout. Revocation, member disable
or inability to verify access cancels the stream; a write already underway
retains its bounded deadline. Original token expiry independently cancels the
context and caps frame writes. Rechecks cannot reactivate revoked sessions or
duplicate first-login evidence. This is bounded periodic enforcement, not an
instantaneous logout of every network connection. See
[ADR-0190](../adr/0190-continuous-sse-authorization.md).

The login timeline means “first observed by TORGNEXA after successful OIDC validation”; it is not presented as a complete Keycloak authentication history. Runtime configuration is shown as `configured`, while provider health is explicitly `not_verified` until a separate external probe has run.

ADR-0188 makes first observation safe when several requests register the same
session concurrently. A unique-key conflict is followed by a fresh locked read;
only the creator writes `session_observed`, atomically with the session. An
established-session request keeps the existing lookup/update path. Revocation
and subject binding are checked under the row lock before updating timestamps;
late or parallel observations cannot reactivate a revoked session. Cancellation
and login-event failures roll back a new session, while real DB failures still
fail authentication closed. Invalid/revoked sessions receive 401; session-store
outages receive a generic 503 with `Retry-After: 5` and `Cache-Control: no-store`,
without `WWW-Authenticate` or database details. Authorization and business
handlers do not run on either failure. This distinction covers session storage;
provider errors and membership resolution keep their existing handling.
No migration or session-history rewrite is needed. See
[ADR-0188](../adr/0188-concurrent-oidc-session-observation.md).

## Privileged mutation audit inventory

Task 234.3 completed the Settings inventory in ADR-0193. Member invitation and
role/status, workspace metadata, current/member profile, avatar removal,
identity-provider revisions/actions and session revocation commit with their
authoritative audit through the tenant/pool-bound transaction introduced by
ADR-0184. Connector accounts, runtime configuration, OAuth local state and
bootstrap controls use the same guarantee through ADR-0191.

AI egress policy and connector replay already commit immutable
`security_evidence` with their operation receipts. MCP and AI provider accounts
now commit governed evidence and the Settings audit in one transaction. MCP
agent policy and tenant kill-switch revisions now require an idempotency key and
commit the immutable revision, receipt and audit together. Manual sync and
reconciliation dispatch commit deterministic run rows with audit; exact retries
append neither. Audit failures return 500 and roll back every local business
row. Summaries contain bounded field names, versions, state and internal IDs;
email, raw OIDC subject and credentials are excluded.

OAuth worker refresh is the one non-rollbackable remote boundary. It commits a
minimized deterministic `connector.oauth_refresh.requested` intent before the
provider call and fails closed when that evidence cannot be stored. See
[ADR-0193](../adr/0193-atomic-privileged-dispatch-and-refresh-intent.md).

## Invitation ownership

An unbound workspace invitation is accepted only when authenticated UserInfo
returns its matching email and the boolean `email_verified: true` for the same
OIDC subject. False, null or missing verification cannot bind an invitation.
The API never combines this flag with a token/profile fallback email, nor treats
a token-only verification claim as ownership evidence. Wrong JSON types fail
authentication. Profile email remains available independently for presentation.

An authenticated unbound caller without ownership proof receives the ordinary
403 response; the invitation remains unchanged and can later be accepted after
verification. Existing active members resolve by issuer-bound subject, so they
do not need to verify email again on each request. A different email or OIDC
realm role cannot elevate an already bound viewer to another invitation's role.
Disabled members and cross-workspace requests remain denied.

Configure the trusted IdP's UserInfo response to include the real email and its
boolean verification result. Deploy the A02 fix to all API instances. Historic
bindings are not automatically revoked: review prior suspicious acceptances and
use governed member disable/session revocation when warranted. Details and
compatibility: [ADR-0187](../adr/0187-verified-email-invitation-binding.md).

`./scripts/check-audit-postgres.sh` runs Task 234.3 settings/dispatch/refresh
failure and replay cases, A02 ownership/replay and A09 concurrent
session registration/revocation scenarios with local synthetic TLS UserInfo and
disposable PostgreSQL alongside A03–A08. A09 uses two independent application
pools and deterministic database barriers to exercise the conflicting requests.
It does not use live provider accounts or modify the running Community database.

## Data governance and SIEM

Session history is tenant-scoped under forced RLS and append-only. It has security-evidence retention: at least 180 days, then the deployment retention policy applies. SIEM export consumes authoritative audit asynchronously; sink failure never participates in the session revocation commit.
