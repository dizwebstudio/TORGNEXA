# ADR 0099: Shared trust control plane for identity, evidence and operator labs

Status: Accepted

## Context

The post-Task-128 audit found that Community application processes share the
PostgreSQL owner/superuser role, REST authorization trusts realm roles without
consulting `workspace_members`, sensitive settings writes can outlive failed
audit capture, and two create APIs accept but do not enforce idempotency keys.
MCP credentials have no expiry/rotation/usage lifecycle and MCP does not share
the REST security-edge composition. Upload and dependency remediation remain
direct fixes, but the remaining gaps need one durable tenant trust boundary.

The same foundation can safely support three requested operator capabilities.
AI egress needs explicit data/budget decisions, connector replay needs bounded
dry-run evidence, and profitability scenarios need immutable assumptions. Each
would otherwise invent its own receipt/history model.

## Decision

Task 129 adds a provider-neutral trust control plane with three append-oriented
records: idempotency receipts, security evidence and governed execution usage.
All are tenant-scoped, forced-RLS and content-minimized. Stable operation names
and SHA-256 request digests bind retries without storing credentials or raw AI
content. Sensitive repositories commit their business row and evidence in one
database transaction where the effect is local; external calls record a durable
authorization decision before egress and an outcome afterward.

Human authorization becomes database authoritative. The verified OIDC token
continues to supply issuer/subject and a routing scope. A membership resolver
then requires an active `workspace_members` row bound to the issuer/subject
reference and replaces realm roles with the stored workspace role. An invited
email may bind only through the reviewed invitation/JIT transition; disabling
the member fails authorization on the next request.

Community compose retains an owner/migrator role only for schema operations and
creates a separate login application role with `NOSUPERUSER NOBYPASSRLS
NOCREATEDB NOCREATEROLE`. API, worker, scheduler and MCP use that role. A runtime
posture inspector fails production startup if these invariants regress and
exposes only boolean/minimized evidence to tenant administrators.

This decision also supersedes two narrow operational baselines without editing
their accepted ADRs: ADR 0093's Go 1.26.5 binding moves to the patched 1.26.7
toolchain, and ADR 0095's heartbeat fallback becomes liveness-only. Realtime
query invalidation now requires an explicit `invalidate` frame; `ready` and
`heartbeat` cannot cause periodic whole-screen refetch amplification.

## Consequences

MCP credentials gain bounded expiry, rotation version, last-used timestamp and
revocation evidence. MCP transport reuses trusted proxy, origin and rate-limit
policy without changing JSON-RPC semantics.

AI governance stores policy and usage metadata only. Prompt previews contain
bounded redacted text returned to the caller but are not persisted. Connector
Replay Lab v1 performs deterministic host-side admission of bounded synthetic
fixtures and proves that no production secret, remote call or write authority
was used; connector execution can only be added later through an existing
conformance/dry-run port. Profitability Scenario Lab performs deterministic fixed-decimal/
minor-unit calculations over an immutable versioned input snapshot; it is
decision support, not transactional settlement truth.

Deployments now have an explicit migrator/application-role split and must
provision active workspace administrators before normal human access. The
additional receipts and evidence consume bounded PostgreSQL storage, while
their append-only design makes rollback a roll-forward or restore operation.

## Compatibility impact

Public API changes are additive under `/api/v1`. Existing OIDC tokens remain
cryptographically valid but require an active membership binding. Existing MCP
tokens remain valid until their migrated expiry and may be rotated. No existing
event schema or Connector SDK root changes. Realtime event names and wire shape
remain compatible; only the browser reaction to liveness frames is narrowed.

## Migration and data impact

One expand migration adds trust-control tables and MCP lifecycle columns. No
historical ledger/audit row is rewritten. New personal identity fields contain
only normalized email already present in `workspace_members` and a one-way
issuer/subject reference; retention follows the existing member workflow.

## Security and privacy impact

Default deny applies at database role, tenant membership, operation permission,
egress policy and execution layers. Raw tokens, prompts, provider responses,
production PII and connector credentials are excluded from receipts/evidence.
AI data classes and redaction are explicit, and replay fixtures are synthetic.

## Operational impact

Deployments must provision the application role before starting runtimes and
must establish at least one active admin membership. Posture failure is a
release/readiness blocker. Credential expiry and budget thresholds require
operator monitoring through the settings surface.

## Alternatives considered

Keeping realm roles authoritative was rejected because disabling a workspace
member would not revoke access. Separate receipt/history tables per feature were
rejected because retry and evidence semantics would drift. Allowing replay to
use live connector credentials was rejected because a test surface must never
become a privileged remote-write bypass. Floating-point profitability math was
rejected because it would violate TORGNEXA money invariants.
