# Trust control plane and decision labs

Task 129 makes the database, rather than OIDC realm roles, authoritative for
workspace access and supplies one durable foundation for retry receipts,
security evidence, credential lifecycle and bounded operator experiments.

## Runtime posture

Community deployments use two PostgreSQL identities:

- `torgnexa` owns the database and is used only by `migrate` and
  `app-db-role`;
- `torgnexa_app` is the runtime login for API, worker, scheduler and MCP. It is
  neither owner nor superuser, has no `BYPASSRLS`, role/database creation or
  `public` schema creation privilege.

Run `scripts/init-community-env.sh` before the first start. Existing `.env`
files can be upgraded by running the same command again: it atomically appends
a newly generated `TORGNEXA_APP_DB_PASSWORD` while preserving every existing
volume credential. It refuses malformed/empty existing values; do not reuse the
owner password. `app-db-role` rotates/provisions the login after migrations and
before any runtime starts.

Every runtime inspects its actual Go version and PostgreSQL identity at startup.
An unsafe or unmeasurable posture stops startup. Administrators can inspect the
minimized result at `GET /api/v1/settings/security/posture`; DSNs and secrets are
never returned.

## Workspace authorization

OIDC validation still establishes issuer, subject and the requested tenant
route. Access then requires an active `workspace_members` row bound to the
one-way issuer/subject reference. Permission checks read the current database
member role on every request, so disabling a member takes effect immediately.
An invited email can bind to its first verified OIDC subject. Development can
bootstrap exactly one initial realm administrator only while the workspace has
no members; production never performs this bootstrap.

## Receipts and evidence

Retryable sensitive operations require `Idempotency-Key`. A receipt binds the
tenant, stable operation name, key and SHA-256 request digest. Reusing a key
with a different request conflicts; a valid retry cannot repeat the local side
effect. Local sensitive writes commit their resource, receipt and minimized
evidence in one transaction.

`security_evidence` is append-only at the database layer. It records actor
reference, correlation, decision/outcome, resource and optional request digest,
but never raw bearer/provider credentials, prompts or provider responses. The
administrator view is `GET /api/v1/settings/security/evidence`.

For an external AI call the idempotency key is reserved and an `allowed`
decision is durable before egress. `denied`, `succeeded` and `failed` outcomes
are evidence records. A consumed external key is never automatically replayed.

## Credential Lifecycle Center

MCP credentials expire after 90 days by default (1–365 days are accepted), are
returned only on initial creation/rotation, and can be immediately revoked.
Rotation creates a replacement credential and revokes the predecessor in the
same transaction. Settings show status, expiry, predecessor, last use and use
count; only token hashes are persisted.

MCP traffic also passes the configured trusted-proxy, origin, request-size and
rate-limit security edge before JSON-RPC authentication and governance.

## AI governance

Without a current enabled policy, AI egress is denied. Each immutable policy
revision declares exact allowlists for data classes, providers and models,
maximum prompt bytes and a monthly authorization budget. Callers must classify
the request. Email, phone and credential-shaped text is redacted before the
provider call. The preview endpoint returns redacted text but neither persists
it nor performs network IO.

## Decision labs

Connector Replay Lab v1 accepts at most 64 KiB of explicitly synthetic JSON.
It recursively rejects credential-shaped keys and excessive nesting/item
counts, then records a deterministic digest/result proving `remote_calls: 0`
and `writes: 0`. It does not load production connector credentials.

Profitability Scenario Lab records immutable input/result snapshots and
`profitability-v1`. Money uses integer minor units, quantity uses milli-units,
fees use basis points and FX uses micros. The result is decision support; it
does not mutate settlement, price or inventory truth.

All controls and labs are available under **Настройки → Контроль и сценарии**.
