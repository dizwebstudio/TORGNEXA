# ADR-0188 — Concurrent OIDC session observation

Status: Accepted

## Context

Audit A09 reproduced seven false 401 responses among eight concurrent first
requests for one valid OIDC session. `SELECT ... FOR UPDATE` cannot lock a row
that does not exist, so all requests reached a plain INSERT and seven lost with
a unique-key violation. Session storage must handle concurrent creators, while
the API must distinguish real persistence failures from invalid credentials
without granting access when the authoritative session check fails.

## Decision

Retain the existing row-locked lookup for established sessions. When the row is
absent, insert with `ON CONFLICT (organization_id,workspace_id,session_ref) DO
NOTHING`. Only the transaction that actually inserts the row writes its
`session_observed` login event. These two records still commit atomically.

A conflicting creator reads the session again with `FOR UPDATE` in a separate
READ COMMITTED statement. This fresh snapshot sees the winner's committed row;
reusing the earlier missing-row result or a single-statement snapshot would not
establish the current state. Check the stored subject and active/revoked status
under the same row lock used by Revoke. A revoked session remains denied, and a
reused reference with a different subject is invalid. Neither path changes the
session's binding or status to active.

For active sessions preserve immutable first-observation metadata and retain
`GREATEST` updates for `last_seen_at`/`expires_at`. The common established-session
path gains no extra database statement. Additional INSERT/re-read work occurs
only during first registration conflicts. No process-local mutex, new lock
service, increased production pool size, retry loop or ignored DB error is used.

Invalid observations, mismatched subjects and revoked sessions still yield 401.
Other session-store failures (including cancellation/deadline) map to a generic
503 with `Retry-After: 5` and `Cache-Control: no-store`, without a credential
challenge or underlying database details. Authorization and business handlers
do not run. Preserve context errors when cancellation precedes Observe, too.
This classification is limited to the session store; UserInfo/provider failures
and membership resolution are outside this change.

## Compatibility and migration

No SQL migration, public API/SDK signature, event schema, dependency or new
persisted field. The shared authentication failure response changes from 401
to 503 for session-store outages; OpenAPI documents it and generated SDK source
hashes are refreshed. Clients must handle this as temporary unavailability,
without clearing credentials in response to an authentication challenge.
The existing tenant/session unique constraint arbitrates
creation. Repository-owned and shared transaction boundaries remain READ
COMMITTED and tenant/pool-bound. Update API instances to use the fix; old
instances in a mixed rollout can still produce the original false 401.

Existing sessions and login history require no rewrite. Revoke is serialized
with observation by the existing row lock: an observation ordered before
revocation may finish, while an observation ordered after committed revocation
must be denied. This is not cancellation of business requests authenticated
before revocation, nor provider-wide SSO logout.

## Security and privacy

Session storage stays fail-closed for real persistence errors, cancellation,
revocation and subject mismatch. Session creation rolls back when its login
event fails. Forced RLS, append-only login evidence and revocation audit remain
unchanged. New tests use synthetic identity hashes and credentials; no new
PII/logging or retention path is introduced.

## Validation

A PostgreSQL BEFORE INSERT fixture blocks all eight concurrent creators at an
advisory barrier, then releases them together. Two independent application
pools and actual OIDC/auth/tenant/authz composition exercise the same session.
Before the fix seven requests return 401; after it all eight succeed and only
one session/login event persists. Repeated requests and post-revocation 401s
are checked separately.

Additional PostgreSQL tests establish both Observe/Revoke lock orders by
waiting for an actual blocked backend, cancel a blocked initial INSERT, inject
login-event failure (HTTP 503, no partial records) and successful retry, verify non-regressing timestamps and immutable
metadata, reject a subject mismatch, and isolate identical references across
workspaces. All run with forced RLS and `-count=1 -race` alongside A02–A08.
This is concurrency regression evidence, not a production latency/capacity claim;
JWKS, membership-call reduction and last_seen throttling remain Task 234.6.
